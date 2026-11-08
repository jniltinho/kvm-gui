// Package archtest checks the repository's layering and the frontend skins.
package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/jniltinho/kvm-gui"

// imports lists every import of the non-test Go files under dir, by the file that has it.
func imports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == "archive" || strings.HasPrefix(d.Name(), ".")) && p != dir {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, i := range f.Imports {
			path, _ := strconv.Unquote(i.Path.Value)
			out[p] = append(out[p], path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Only internal/gui imports Wails, and internal/core and reims import no GUI package, so the
// command line and the MCP server never depend on the desktop toolkit.
func TestLayers(t *testing.T) {
	for file, paths := range imports(t, "../..") {
		inGUI := strings.HasPrefix(filepath.ToSlash(file), "../../internal/gui/")
		inCore := strings.HasPrefix(filepath.ToSlash(file), "../../internal/core/") || strings.HasPrefix(filepath.ToSlash(file), "../../reims/")
		for _, p := range paths {
			if strings.HasPrefix(p, "github.com/wailsapp/") && !inGUI {
				t.Errorf("%s imports %s: only internal/gui may import Wails", file, p)
			}
			if inCore && (p == module+"/internal/gui" || p == module+"/cmd" || p == module+"/internal/mcp") {
				t.Errorf("%s imports %s: the core stays independent of its front ends", file, p)
			}
		}
	}
}
