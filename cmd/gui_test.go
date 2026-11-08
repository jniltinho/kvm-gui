package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// `licenses` prints the embedded license text; the GUI flags exist on both the root and `gui`.
func TestLicensesAndGUIFlags(t *testing.T) {
	licenses = "BSD-3-Clause test text\n"
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"licenses"})
	if err := rootCmd.Execute(); err != nil || out.String() != licenses {
		t.Fatalf("licenses: %v %q", err, out.String())
	}
	for _, c := range []string{"", "gui"} {
		cmd := rootCmd
		if c != "" {
			cmd = guiCmd
		}
		for _, f := range []string{"self-test", "safe-gpu"} {
			if cmd.Flags().Lookup(f) == nil {
				t.Errorf("%q has no --%s", strings.TrimSpace("kvm-gui "+c), f)
			}
		}
	}
}
