package archtest

import (
	"encoding/json"
	"os"
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
