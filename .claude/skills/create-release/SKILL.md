---
name: create-release
description: Create a version tag and a GitHub Release for kvm-gui (or a reims-<commit> Reims vGPU release) with polished notes. Use when the user asks to release, cut a version, tag a release or publish a new version.
---

# Releasing kvm-gui

Two series live on this repository's releases:

| Series | Tag | Title | Workflow |
|---|---|---|---|
| kvm-gui | `vX.Y.Z` (one series: the old `macos-kvm-gui` releases continue in it) | `kvm-gui vX.Y.Z` | `go-release.yml`, manual: `gh workflow run go-release.yml --ref vX.Y.Z`; runs `ci.yml` (with the four clean-distribution checks) on the exact commit first, then publishes and marks the release **latest** |
| Reims vGPU's QEMU, `reims-ctl`, `reims-util` | `reims-<7-digit commit>` (must match `ARG REIMS_COMMIT` in `reims/Dockerfile`) | `Reims vGPU QEMU <commit>` | `reims-release.yml`, on the tag push |

Keep the last two releases of each series (`gh release delete <tag> --cleanup-tag -y` for older ones). To try the release build without publishing, run it on any ref with `-f publish=false`: the five files come back as the run's `kvm-gui-release` artifact.

## Before you start

```bash
gh auth status            # needs repo scope
git status                # must be clean
git fetch origin --tags
git checkout main
git pull --ff-only
make test openspec dist   # everything in the build image (Docker); must pass
```

## Versioning (kvm-gui)

Semantic versioning with a `v` prefix.

- `feat:` commits since the last tag → bump MINOR.
- Only `fix:`, `perf:`, `refactor:`, `ci:`, `docs:` or `chore:` → bump PATCH.

```bash
LAST=$(git describe --tags --match 'v[0-9]*' --abbrev=0)
git log "$LAST"..HEAD --oneline
```

## Process

### 1. Review and categorize the changes

Sections, in this order; omit empty ones:

| Section | Prefixes |
|---|---|
| **✨ New Features** | `feat:` |
| **🔧 Improvements** | `fix:`, `perf:`, `refactor:`, `ci:` |
| **🧹 Cleanup** | `chore:`, `cleanup:` |
| **📚 Documentation** | `docs:`, `doc:` |

### 2. Tag, push and run the workflow

```bash
NEXT=vX.Y.Z
git tag -a "$NEXT" -m "Release $NEXT"
git push origin "$NEXT"
gh workflow run go-release.yml --ref "$NEXT"
gh run watch "$(gh run list --workflow go-release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
```

The workflow attaches:

| Artifact | Notes |
|---|---|
| `kvm-gui-vX.Y.Z-x86_64.AppImage` | any of the four distributions with GTK 4 and WebKitGTK 6 installed (the AppImage does not bundle them) |
| `kvm-gui-vX.Y.Z-linux-amd64` | the plain binary, same libraries |
| `kvm-gui_X.Y.Z_amd64.deb` | Ubuntu 24.04, 26.04, Debian 13; `/usr/bin/kvm-gui` and the `macos-kvm-gui` link; replaces `macos-kvm-gui` |
| `kvm-gui-X.Y.Z-1.x86_64.rpm` | Rocky Linux 10 (enable EPEL first: WebKitGTK 6 comes from it); same files; obsoletes `macos-kvm-gui` |
| `SHA256SUMS` | all four files |

### 3. Replace the generated notes

Always keep the home lab notice and say only what was really validated.

~~~bash
cat > /tmp/release-notes-$NEXT.md <<'EOF'
> **For home lab use.** macOS is Apple's software, and its license only allows it on Apple-branded hardware.

## ✨ New Features

- ...

## 🔧 Improvements

- ...

## Install

```sh
sha256sum -c --ignore-missing SHA256SUMS
sudo apt install ./kvm-gui_X.Y.Z_amd64.deb      # Ubuntu 24.04, 26.04, Debian 13
sudo dnf install epel-release && sudo dnf install ./kvm-gui-X.Y.Z-1.x86_64.rpm   # Rocky Linux 10
chmod +x kvm-gui-vX.Y.Z-x86_64.AppImage && ./kvm-gui-vX.Y.Z-x86_64.AppImage     # or the plain binary
```

**Validated:** <what was really run, e.g. the Nilton VM (Sequoia 15.8.1, Reims vGPU) booted from the GUI on Ubuntu 24.04>

**Full Changelog**: https://github.com/jniltinho/kvm-gui/compare/vPREV...vNEXT
EOF
gh release edit "$NEXT" --title "kvm-gui $NEXT" --notes-file /tmp/release-notes-$NEXT.md
~~~

### 4. Verify

```bash
gh release view "$NEXT" --json name,isLatest,assets --jq '.name, .isLatest, (.assets[].name)'
gh release download "$NEXT" -D /tmp/check-$NEXT && (cd /tmp/check-$NEXT && sha256sum -c SHA256SUMS)
```

Check the title, that it is latest, the five assets, the checksums, and that `scripts/check-dist.sh <dist> all` passes for `ubuntu24.04`, `ubuntu26.04`, `debian13` and `rocky10` on the downloaded files (CI already ran it on the commit).

## Reims vGPU releases

1. Pin the newest upstream commit in `reims/Dockerfile` and `reims/Dockerfile.rocky`, build and check locally in Docker (AGENTS.md), commit, then tag `reims-<7-digit commit>` on that commit and push the tag.
2. `reims-release.yml` builds QEMU, `reims-ctl` and `reims-util` for `ubuntu24.04`, `ubuntu26.04`, `debian13` and `rocky10`, checks each in a clean container, and publishes them with `reims-vgpu-src-<commit>.tar.gz`.
3. Notes: the home lab notice; that it is Reims vGPU's QEMU (with `reims-ctl` and `reims-util`), not an app; that `kvm-gui reims install` downloads the host's package and checks the SHA-256 the app pins; the assets table; **Validated** (what really ran, e.g. the Nilton VM on the 24.04 build). Credit [steelbrain/reims-vgpu](https://github.com/steelbrain/reims-vgpu) as the base. Do not mention the archived reims-gui.
4. Commit `reims/manifest.json` with the published SHA-256 files.

## Building a release locally

`make dist VERSION=vX.Y.Z` produces the same files in `dist/` without publishing anything.
