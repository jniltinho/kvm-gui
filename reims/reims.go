// Package reims holds the inputs that build and package Reims vGPU's QEMU (steelbrain/reims-vgpu):
// the Dockerfiles that pin upstream's commit, the manifest of the packages this repository
// publishes on its reims-<commit> releases, and package.sh. It is the one place they live: the
// app embeds them from here, and .github/workflows/reims-release.yml reads them from here.
package reims

import _ "embed"

var (
	// Dockerfile builds upstream at the pinned commit for an Ubuntu or Debian platform.
	//go:embed Dockerfile
	Dockerfile []byte
	// Manifest names the release tag and each platform's package with its SHA-256.
	//go:embed manifest.json
	Manifest []byte
)
