// kvm-gui creates and runs macOS and other VMs on Linux KVM: one binary that is the desktop GUI
// (the default), the command line and the MCP server.
package main

import (
	_ "embed"

	"github.com/jniltinho/kvm-gui/cmd"
)

// version is set at build time: go build -ldflags "-X main.version=v1.0.0".
var version = "dev"

// The project's license and the third-party notices travel inside the binary (kvm-gui licenses).
var (
	//go:embed LICENSE
	license string
	//go:embed THIRD_PARTY_NOTICES.md
	notices string
	//go:embed THIRD_PARTY_LICENSES.txt
	licenseTexts string
)

func main() { cmd.Execute(version, license+"\n"+notices+"\n"+licenseTexts) }
