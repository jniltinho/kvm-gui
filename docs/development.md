# Development

[Documentation index](README.md)

## Build

Everything builds and runs in one Docker image, `build/linux.Dockerfile` (Ubuntu 24.04, Go from `go.mod`, Node and the OpenSpec CLI, `qemu-utils`, QEMU, Xvfb, `dpkg-dev`, `rpm`). The host needs only Docker and make; do not install Go or build dependencies on it.

```sh
make image          # build the image (kvm-gui-build:24.04)
make build          # dist/kvm-gui (CGO_ENABLED=0)
make build-prod     # stripped, -trimpath, main.version from VERSION
make self-test      # the GUI opens and loads the VM list, under Xvfb in the image
make dist           # dist/: the AppImage, the binary, kvm-gui_<v>_amd64.deb, kvm-gui-<v>-1.x86_64.rpm and SHA256SUMS
scripts/check-dist.sh <ubuntu24.04|ubuntu26.04|debian13|rocky10> all   # the artifacts in clean containers
scripts/check-gnome.sh <24.04|26.04>   # Reims vGPU embedding under GNOME (X11 on 24.04, Wayland on 26.04)
make test-embed     # the embedding package under Xvfb, with a fake reparenting window manager
```

Go's build and module caches persist in `~/.cache/kvm-gui-build`. Boot assets are committed; a normal build does not fetch them.

`make package` runs `scripts/package.sh` in the image. The `.deb` (Ubuntu 24.04, 26.04, Debian 13) depends on `qemu-system-x86`, `qemu-system-gui`, `qemu-utils`, `ovmf` and `pkexec` and recommends `swtpm`; the `.rpm` (Rocky Linux 10) requires `qemu-kvm`, `qemu-img`, `edk2-ovmf` and `polkit`. Both replace `macos-kvm-gui` and install `/usr/bin/macos-kvm-gui` as a link to `kvm-gui`. After each milestone, `make dist` leaves the milestone's artifacts in `dist/`.

The desktop GUI is being rebuilt in Go + Wails (OpenSpec change `migrate-gui-to-go-wails`); until it lands, the binary is the command line and the MCP server.

## Tests and validation

```sh
make test
```

In the build image, this first fetches `internal/core/testdata/ocvalidate` if absent, using `scripts/fetch-ocvalidate.sh`, then checks gofmt, runs `go vet ./...` and `go test -race ./...` with `MKG_REQUIRE_TOOLS=1`, so a test that needs `qemu-img` or `ocvalidate` fails instead of skipping. Fetching verifies the OpenCore release checksum from `assets.env` with the same environment/repository `.env` overrides. The validator is git-ignored and not embedded. With it already present and Go dependencies cached, the unit-test path needs no downloads.

Tests cover configuration/profiles, fake host/sysfs checks, reservation scripts, QEMU arguments, QMP with a fake server, recovery with a local HTTP server, real Apple chunklist signature data and synthetic signed-image failures. Disk/media tests use real `qemu-img` from the build image; with `MKG_REQUIRE_TOOLS=1` (as `make test` sets) a missing tool fails the test instead of skipping it. They do not download a full macOS installation or exercise physical GPU passthrough.

`TestOpenCoreConfig` generates install, installed and GPU config variants, invoking `ocvalidate` when the executable exists and requiring `No issues found`. Running bare `go test` without it does **not** validate the schema. To explicitly fetch/refresh it and run that test:

```sh
make internal/core/testdata/ocvalidate
make test
```

### Integration with real QEMU/KVM

Install `qemu-system-x86`, ensure `/dev/kvm` access and a working graphical environment for QEMU's default display, and stop conflicting hypervisor VMs:

```sh
make test-integration
# go test -race -tags integration -run Real ./internal/core
```

`TestRealQEMULifecycle` creates a temporary workspace (including commas in its path), creates a 40 GiB thin disk, boots embedded OpenCore via vvfat, checks persistent VARS creation and lock ownership, sends QMP powerdown, then force-stops QEMU and checks cleanup. It uses host SSH port **22299**. It does **not** install macOS or prove a guest reaches its desktop.

The integration-tagged `TestPrintBootArgs` is a separate manual helper: with `MKG_BOOT_WS` set, it prepares that workspace and writes NUL-separated QEMU arguments to `<workspace>/args`. It is excluded by `-run Real`. Use a disposable workspace/overlay, not an unprotected production guest disk, for manual boot validation.

Only Sonoma has recorded end-to-end validation in the repository. GPU passthrough is **untested on real hardware**. Report unit, QEMU-lifecycle, guest-desktop and real-GPU validation separately.

## Updating boot assets

[![Assets pipeline](diagrams/assets-pipeline.png)](diagrams/assets-pipeline.svg)

1. Review upstream release licenses and record the version, canonical download URL and independently checked SHA-256 in [`assets.env`](../assets.env). Maintain [third-party notices](../THIRD_PARTY_NOTICES.md) if needed.
2. Install `curl`, `sha256sum`, `unzip` and `dpkg-deb`, then run `scripts/fetch-assets.sh`. It downloads all components and verifies every checksum before deleting/rebuilding `internal/assets/files`. Hash failure preserves the old tree; a later extraction failure is not an atomic update.
3. Review the rebuilt EFI tree, OVMF pair and `files/VERSIONS`. The script also refreshes test-only `ocvalidate`. Commit the intended boot files, not the validator or generated build output.
4. If OpenCore's schema changed, update `internal/assets/config.plist.tmpl` from the corresponding upstream sample, retaining attribution and KVM settings. A version bump alone does not update the template.
5. Run `make test`, `make test-integration` where KVM is available, and a real macOS boot with a disposable overlay. Check picker/media behavior, resolution, kexts, stable identity and unchanged NVRAM.

The scripts parse plain `KEY=value` data, never source shell files. Only component `VERSION`, `URL`, `SHA256` keys are accepted (the validator-only script reads OpenCore URL/SHA256). Precedence is **process environment > repository `.env` > `assets.env`**. Keep repository `.env` values unquoted and without shell expressions.

For a runtime experiment without rebuilding:

```sh
make build
MKG_ASSETS_DIR="$PWD/internal/assets" ./dist/kvm-gui vm start
```

This is a complete-tree override, not a per-file overlay. `kvm-gui version` reports its versions/source. It is separate from the optional runtime `.env` under the user config directory; see [architecture](architecture.md#settings-and-overrides). Boot files extracted into a workspace are regenerated on every Start, so do not use workspace edits as the development source.

## OpenSpec workflow

[`openspec/config.yaml`](../openspec/config.yaml) selects the `spec-driven` schema and English artifacts/UI/comments. Changes live under [`openspec/changes/`](../openspec/changes/), with `proposal.md`, `design.md`, `specs/<capability>/spec.md`, `tasks.md` and `.openspec.yaml`.

- Explain the motivation/scope in the proposal, decisions and risks in the design, and behavior as requirements/scenarios in spec deltas.
- Implement and verify task-list items; keep validation evidence and untested work visible.
- Validate all changes using the CI-pinned tool:

  ```sh
  make openspec   # openspec validate --all --strict, in the build image
  ```

- Use OpenSpec's archive workflow after acceptance; `openspec/changes/archive/` is the archive destination. Validation checks spec structure, not runtime correctness.

Read changes in context: `add-desktop-gui` describes the original clone/Python design, now superseded by `embed-boot-assets`. Parts of the original GPU proposal also predate keeping `vmware-svga` and generating `agdpmod=pikera`. Current code and the later boot-assets design define those implemented behaviors; do not restore old assumptions from historical proposals.

## CI and releases

| Workflow | Trigger and work |
| --- | --- |
| [CI](../.github/workflows/ci.yml) | Pushes to `main` and pull requests: `make test`, `layers`, `openspec`, `frontend-check`, `self-test` and `dist` in the build image, then `scripts/check-dist.sh` on Ubuntu 24.04, Ubuntu 26.04, Debian 13 and Rocky Linux 10 (Rocky's self-test runs on Wayland under a headless Weston: RHEL 10 has no Xorg server). New runs cancel earlier runs for the same ref. |
| [Release](../.github/workflows/go-release.yml) | By hand. It runs CI on the exact commit first; on a `v*` tag it then publishes the AppImage, the binary, the `.deb`, the `.rpm` and `SHA256SUMS` as the latest release. With `-f publish=false` (any ref) it keeps the five files as the run's artifact and publishes nothing. |
| [Reims vGPU](../.github/workflows/reims-release.yml) | A `reims-<7-digit commit>` tag matching `reims/Dockerfile`: builds QEMU, `reims-ctl` and `reims-util` for Ubuntu 24.04, 26.04, Debian 13 and Rocky Linux 10, checks each in a clean container, and publishes them with the source archive. |

Neither workflow runs the real-QEMU integration target, a macOS installation or a real-GPU test. Release does not run the separate OpenSpec job. Builds use committed boot assets rather than refetching them; tests may fetch `ocvalidate` on a fresh runner.

### The create-release skill

The repository's [`.claude/skills/create-release/SKILL.md`](../.claude/skills/create-release/SKILL.md) documents the maintainer release procedure; it is not a Make target or automatic version bump. It requires authenticated `gh`, a clean/up-to-date `main`, fetched tags and passing local tests. It uses `v0.MINOR.PATCH`: feature commits bump MINOR; fixes/improvements/docs/chores alone bump PATCH.

The procedure reviews commits, creates an annotated `v*` tag, pushes it, runs and watches `go-release.yml` (manual, on the tag), then replaces the generated notes with categorized New Features, Improvements, Cleanup and Documentation sections (omitting empty sections). Keep the home-lab/license notice, install instructions, actual hardware-validation scope and full changelog link. Finally check the release title, four attached artifacts, SHA-256 files and that the binary opens with the tagged title. Never describe unit coverage as real-GPU validation.
