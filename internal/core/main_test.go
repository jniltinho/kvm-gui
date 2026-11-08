package core

import (
	"os"
	"testing"
)

// TestMain keeps every test away from the developer's real settings.yaml and .env.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "macos-kvm-gui-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// testDefaults is Defaults with an explicit SSH port, for tests that validate a config or build
// QEMU's arguments without creating a VM.
func testDefaults() Config {
	c := Defaults()
	c.SSHPort = FirstSSHPort
	return c
}

// needTool skips a test whose tool is missing, or fails it under MKG_REQUIRE_TOOLS=1 (CI and make test),
// so a check never passes silently because the image lacks a tool.
func needTool(t *testing.T, why string) {
	t.Helper()
	if os.Getenv("MKG_REQUIRE_TOOLS") == "1" {
		t.Fatal("required tool missing: ", why)
	}
	t.Skip(why)
}
