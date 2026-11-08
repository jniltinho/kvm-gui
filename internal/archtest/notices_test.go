package archtest

import (
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// Every module in go.mod and every package in frontend/package.json has a row in
// THIRD_PARTY_NOTICES.md (task 8.5).
func TestNoticesCoverDependencies(t *testing.T) {
	notices, err := os.ReadFile("../../THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	gomod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	var deps []string
	for _, m := range regexp.MustCompile(`(?m)^\s+([a-z0-9.\-]+\.[a-z]+/[^\s]+) v`).FindAllStringSubmatch(string(gomod), -1) {
		deps = append(deps, m[1])
	}
	var pkg struct{ Dependencies, DevDependencies map[string]string }
	b, err := os.ReadFile("../../frontend/package.json")
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(b, &pkg)
	for d := range pkg.Dependencies {
		deps = append(deps, d)
	}
	for d := range pkg.DevDependencies {
		deps = append(deps, d)
	}
	if len(deps) < 10 {
		t.Fatalf("found only %v", deps)
	}
	for _, d := range deps {
		if !strings.Contains(string(notices), "`"+d+"`") {
			t.Errorf("THIRD_PARTY_NOTICES.md has no row for %s", d)
		}
	}
}

// Every Go module compiled into the binary has its license text, at its version, in
// THIRD_PARTY_LICENSES.txt, which every package and the binary ship (Codex group 8 review).
func TestLicenseTextsCoverModules(t *testing.T) {
	texts, err := os.ReadFile("../../THIRD_PARTY_LICENSES.txt")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("go", "list", "-tags", "production", "-deps", "-f", "{{with .Module}}{{.Path}} {{.Version}}{{end}}", "../..").Output()
	if err != nil {
		t.Skip("go list:", err) // the GUI package needs GTK's headers: run in the build image
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" || strings.Fields(line)[0] == "github.com/jniltinho/kvm-gui" {
			continue
		}
		n++
		if !strings.Contains(string(texts), "\n======== "+line+"\n") {
			t.Errorf("THIRD_PARTY_LICENSES.txt lacks %s: run make licenses", line)
		}
	}
	if n < 10 {
		t.Fatalf("only %d modules listed", n)
	}
}
