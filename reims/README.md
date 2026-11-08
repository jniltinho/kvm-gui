# Reims vGPU packaging

Reims vGPU is [steelbrain/reims-vgpu](https://github.com/steelbrain/reims-vgpu): a virtual GPU for
macOS VMs, its QEMU build, and `reims-ctl`. Upstream's README, `docs/qemu.md` and
`docs/architecture.md` are the source of truth for the GPU, its QEMU, `reims-ctl` and the guest's
setup; this folder only packages it for kvm-gui.

| File | What it does |
| --- | --- |
| `Dockerfile` | Builds upstream at the pinned commit (`ARG REIMS_COMMIT`) on Ubuntu 24.04/26.04 or, with `BASE=debian:13`, Debian 13 |
| `Dockerfile.rocky` | The same for Rocky Linux 10 |
| `manifest.json` | The `reims-<commit>` release and each platform's package with its SHA-256; `kvm-gui reims install` downloads and checks them |
| `package.sh` | Packs a build (`pkg`) and the corresponding source (`src`) for the release |
| `reims.go` | Embeds the Dockerfile and manifest in the app (`kvm-gui reims build`, `reims install`) |

Newest upstream: `.github/workflows/reims-upstream.yml` (run by hand: `gh workflow run reims-upstream.yml`) builds the
same packages at the newest commit of steelbrain/reims-vgpu's `main` and publishes them as `reims-<7-digit commit>`,
unless that release exists. Nothing is pinned by it: to make the app install that build, pin the Dockerfiles and
`manifest.json` to it as below.

Releases: `.github/workflows/reims-release.yml` runs on a `reims-<7-digit commit>` tag that must
match `ARG REIMS_COMMIT`. After a release, `manifest.json` takes the published SHA-256 files.
