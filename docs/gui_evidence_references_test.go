package docs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGUIEvidenceReferencesResolveToRealTests(t *testing.T) {
	docs, err := filepath.Glob("gui*.md")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile("`(internal/(?:app|tui|gui|agent|daemon|transcript|feed)|\\.):(Test[A-Za-z0-9_]+)`")
	for _, doc := range docs {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			pkg, testName := m[1], m[2]
			if !testExists(t, pkg, testName) {
				t.Fatalf("%s cites missing test %s:%s", doc, pkg, testName)
			}
		}
	}
}

func testExists(t *testing.T, pkg, testName string) bool {
	t.Helper()
	var files []string
	var err error
	if pkg == "." {
		files, err = filepath.Glob("../*_test.go")
	} else {
		files, err = filepath.Glob("../" + pkg + "/*_test.go")
	}
	if err != nil {
		t.Fatal(err)
	}
	needle := fmt.Sprintf("func %s(", testName)
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), needle) {
			return true
		}
	}
	return false
}
