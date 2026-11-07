package analysis

import (
	"go/build"
	"strings"
	"testing"
)

// TestImportsOnlyStandardLibrary keeps the dependency direction documented in
// doc.go: tsparser imports analysis, never the reverse. Allowing only the
// standard library also rules out cmd, GUI-specific code and any later package
// that could import tsparser and close a cycle.
func TestImportsOnlyStandardLibrary(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	imports := append(append(append([]string{}, pkg.Imports...), pkg.TestImports...), pkg.XTestImports...)
	if len(imports) == 0 {
		t.Fatal("no imports found; is the package directory right?")
	}
	for _, path := range imports {
		if !isStandardLibrary(path) {
			t.Errorf("package analysis imports %q; it may import only the standard library", path)
		}
	}
}

// isStandardLibrary reports whether path names a standard-library package: its
// first element has no dot, unlike module paths such as github.com/....
func isStandardLibrary(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func TestIsStandardLibrary(t *testing.T) {
	for path, want := range map[string]bool{
		"fmt":      true,
		"go/build": true,
		"github.com/small-teton/mpeg-ts-analyzer/v2/tsparser": false,
		"example.com/x": false,
	} {
		if got := isStandardLibrary(path); got != want {
			t.Errorf("isStandardLibrary(%q) = %v, want %v", path, got, want)
		}
	}
}
