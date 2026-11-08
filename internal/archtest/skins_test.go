package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every skin (frontend/src/themes/*.css, one or more `:root[data-theme="…"] { … }` blocks) must define
// the same tokens, so a new skin cannot leave a role unset and fall back to another skin's colour.
func TestSkinsDefineTheSameTokens(t *testing.T) {
	files, _ := filepath.Glob("../../frontend/src/themes/*.css")
	block := regexp.MustCompile(`(?s)data-theme="([a-z-]+)"\]\s*\{(.*?)\n\}`)
	token := regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+):`)
	var ref []string
	refName := ""
	n := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range block.FindAllStringSubmatch(string(b), -1) {
			var toks []string
			for _, tm := range token.FindAllStringSubmatch(m[2], -1) {
				toks = append(toks, tm[1])
			}
			sort.Strings(toks)
			n++
			if ref == nil {
				ref, refName = toks, m[1]
				continue
			}
			if strings.Join(toks, " ") != strings.Join(ref, " ") {
				t.Errorf("skin %q tokens differ from %q:\n%v\n%v", m[1], refName, toks, ref)
			}
		}
	}
	if n < 6 {
		t.Fatalf("found %d skins, want the 6 shipped ones", n)
	}
}
