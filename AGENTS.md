# AGENTS.md

Guidance for AI coding agents (Claude Code, Codex, Cursor) working in this repository.

## One app

**kvm-gui** (Go 1.27, at the repository root) is one binary: the desktop GUI (Go + Wails v3, Vue 3 + Vite Plus + Tailwind, being built per the OpenSpec change `migrate-gui-to-go-wails`), the command line (Cobra, `cmd/`) and the MCP server (`internal/mcp`), over the GUI-free core `internal/core`.

| Part | Where | Releases (tag → title) |
|---|---|---|
| kvm-gui | `main.go`, `cmd/`, `internal/` | `vX.Y.Z` → `kvm-gui vX.Y.Z` (none until the Wails GUI lands) |
| Reims vGPU's QEMU, `reims-ctl`, `reims-util` | upstream [steelbrain/reims-vgpu](https://github.com/steelbrain/reims-vgpu), packaged from `reims/` | `reims-<commit>` → `Reims vGPU QEMU <commit>` |
| reims-gui (Rust, archived) | `archive/rust/reims-gui/` | none: read-only, never built |

The only Rust that is built is upstream Reims vGPU, inside `reims/`'s Docker build. `steelbrain/reims-vgpu` is the base reference for anything Reims.

## Rules

- **English only** in code, comments, docs, UI text and commit messages. Talk to the user in their language.
- **Docker-only builds.** Every build, test and package runs in the build image (`build/linux.Dockerfile`) through `make`; never use or install Go, Node, Rust or GTK headers on the host.
- **After each milestone, `make dist`** writes the milestone's artifacts to `dist/` with `SHA256SUMS`.
- **Validate locally before GitHub.** Build, test and check packages here first, then push or tag.
- **Keep the last 2 releases and tags of each series** (`v*`, `reims-*`). Delete older ones with `gh release delete <tag> --cleanup-tag -y`. Exception: `v0.13.0` (the last `macos-kvm-gui`) stays while `scripts/check-dist.sh` uses it as the upgrade fixture (checksums pinned there).
- **Release notes** follow `.claude/skills/create-release/SKILL.md`: the home lab notice, ✨/🔧/🧹/📚 sections, Install, **Validated** (only what really ran) and Full Changelog (links to tags that still exist, or commit ids).
- **OpenSpec** (`openspec/`) for features: propose, have Codex review it (`codex exec -s read-only -C . "<review prompt>"`), fix P1/P2 findings, implement, mark each task with its evidence, archive. `make openspec` (`openspec validate --all --strict`) must pass.
- **References.** VirtualBox, virt-manager and webvirtmgr are behavior references only: never copy their code, strings, layouts or icons (this project is BSD-3-Clause). Reference captures live in `docs/prints/` (git-ignored).
- **Secrets.** The test VM's password is never written to files, commits or docs.
- **Destructive or outward actions** (force pushes, deleting releases outside the 2-per-series rule, posting to third-party repos) need the user's explicit OK.
- **The user's desktop.** Do not drive it with xdotool or agent-desktop while the user is using it. Ask first, or use the GNOME containers or a separate Xvfb display.

## Build and test

```sh
make image       # the build image
make test        # gofmt, go vet, go test -race (MKG_REQUIRE_TOOLS=1: a missing tool fails)
make openspec    # openspec validate --all --strict
make self-test   # the GUI opens under Xvfb in the image
make dist        # dist/: AppImage, binary, kvm-gui_<v>_amd64.deb, kvm-gui-<v>-1.x86_64.rpm, SHA256SUMS
scripts/check-dist.sh <ubuntu24.04|ubuntu26.04|debian13|rocky10> all  # artifacts in clean containers
scripts/check-gnome.sh <24.04|26.04>  # Reims embedding under GNOME (X11; Wayland: none), the .deb in clean containers
make test-embed  # internal/embed under Xvfb
make test-integration  # real QEMU/KVM boot of the embedded OpenCore (needs /dev/kvm)
```

- **Packages.** The `.deb` installs on Ubuntu 24.04, 26.04 and Debian 13, the `.rpm` on Rocky Linux 10; both replace `macos-kvm-gui` and keep that name as a command link.

### Reims vGPU's QEMU (`reims/`)

- `reims/Dockerfile` (Ubuntu 24.04/26.04, `--build-arg BASE=debian:13` for Debian 13) and `reims/Dockerfile.rocky` pin upstream's commit (`ARG REIMS_COMMIT`) and run all of upstream's `scripts/install-ctl.sh`: QEMU, `reims-ctl` and `reims-util` end up in one build folder.
- `docker build --build-arg UBUNTU=24.04 -t reims-build:24.04 - < reims/Dockerfile`, then `docker run --rm -e HOST_UID=$(id -u) -e HOST_GID=$(id -g) -v <dir>:/build reims-build:24.04`.
- `reims/package.sh pkg|src` packs a build and the corresponding source; `.github/workflows/reims-release.yml` does it for all four platforms on a `reims-<7-digit commit>` tag, and `reims/manifest.json` then takes the published SHA-256 files.
- An Ubuntu build does not run on Debian (`libjpeg.so.8`); each platform needs its own.

## Workflows (`.github/workflows/`)

| File | Runs | Does |
|---|---|---|
| `ci.yml` | push to `main`, PRs | `make test layers openspec frontend-check self-test dist` in the build image, then `scripts/check-dist.sh` on the four distributions |
| `go-release.yml` | manual | runs `ci.yml` on the exact commit, then on a `v*` tag publishes the AppImage, binary, `.deb`, `.rpm` and `SHA256SUMS` as latest; `-f publish=false` keeps them as run artifacts |
| `reims-release.yml` | tag `reims-<commit>`; called by `reims-upstream.yml` | Reims QEMU with `reims-ctl` and `reims-util` for 24.04, 26.04, debian13, rocky10, and the source archive |
| `reims-upstream.yml` | manual | the same packages at the newest commit of upstream's `main`, as `reims-<commit>`, unless that release exists (`-f force=true` rebuilds); the app keeps installing the commit `reims/manifest.json` pins |

## Testing on real hardware

- **Reference host:** Ubuntu 24.04, KDE Plasma on X11, Ryzen 9 9900X, RTX 4070 (Reims vGPU validated) and RX 6700 XT.
- **Test VM:** "Nilton" (macOS Sequoia 15.8.1, Reims vGPU) in `~/macOS-VMs/nilton`, SSH at `127.0.0.1:2223`, user `nilton`; the password comes from the user, never stored. Shut it down over SSH (`sudo shutdown -h now`), not with Force stop.
- **Do not run two QEMUs on the same disk.** The live GNOME containers (`rg-live-img:24.04` / `26.04`) use a qcow2 overlay over the host disk mounted read-only; do not start the VM on the host while they run.

## Docs to keep in step

`README.md`, `docs/architecture.md`, `docs/development.md`, `docs/reims-vgpu.md`, `docs/cli.md`, `docs/mcp.md`, `docs/getting-started.md`, `docs/screenshots/` (captured from the real app), `THIRD_PARTY_NOTICES.md`, `reims/README.md`.
