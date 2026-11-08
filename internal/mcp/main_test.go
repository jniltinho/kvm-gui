package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps every test away from the developer's real settings.yaml and .env. Run as
// ssh-askpass (vm_ssh's helper symlink points at this test binary), it is the helper.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == AskpassName {
		if err := Askpass(os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "macos-kvm-gui-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// needTool skips a test whose tool is missing, or fails it under MKG_REQUIRE_TOOLS=1.
func needTool(t *testing.T, why string) {
	t.Helper()
	if os.Getenv("MKG_REQUIRE_TOOLS") == "1" {
		t.Fatal("required tool missing: ", why)
	}
	t.Skip(why)
}
