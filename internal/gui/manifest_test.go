package gui_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/gui"
)

// TestManifestUpToDate regenerates the Bridge manifest in-memory and compares
// it to the committed bridge.manifest.json. If they differ, the bridge contract
// changed (a method was added/removed/renamed, a DTO field changed, a JSON tag
// was renamed) and the developer MUST:
//
//  1. Run: go generate ./internal/gui
//  2. Review the diff: git diff internal/gui/bridge.manifest.json
//  3. If the change is intentional (new method, DTO evolution), commit it.
//     If accidental (agent renamed a field), revert.
//
// This test is the load-bearing piece that prevents silent breakage: under
// the guiserver reflect dispatcher, a renamed JSON tag becomes a silent null
// in Qt. The test runs tagless so it's part of `make gate`.
func TestManifestUpToDate(t *testing.T) {
	// Regenerate manifest in a temp dir
	tempDir := t.TempDir()

	// Find repo root
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	// Run the generator
	cmd := exec.Command("go", "run", "./internal/gui/gen/manifest")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "TMPDIR="+tempDir)

	// Capture stderr to see the generator output
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("generate manifest: %v\nstderr:\n%s", err, stderr.String())
	}

	// Load the freshly generated manifest
	generatedPath := filepath.Join(repoRoot, "internal", "gui", "bridge.manifest.json")
	generatedBytes, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read generated manifest: %v", err)
	}

	// Parse both manifests for comparison
	var generated, committed map[string]any

	if err := json.Unmarshal(generatedBytes, &generated); err != nil {
		t.Fatalf("parse generated manifest: %v", err)
	}

	committedData, err := gui.ManifestData()
	if err != nil {
		t.Fatalf("parse committed manifest: %v", err)
	}
	committed = committedData

	// Compare canonical JSON (re-marshal both with sorted keys)
	generatedCanonical, _ := json.MarshalIndent(generated, "", "  ")
	committedCanonical, _ := json.MarshalIndent(committed, "", "  ")

	if !bytes.Equal(generatedCanonical, committedCanonical) {
		// Compute a compact diff of changed entries
		diff := computeManifestDiff(committed, generated)

		t.Fatalf("Bridge contract changed — run `go generate ./internal/gui` and review the diff\n\n"+
			"Changed entries:\n%s\n\n"+
			"Committed manifest hash: %s\n"+
			"Generated manifest hash: %s\n",
			diff, hashBytes(committedCanonical), hashBytes(generatedCanonical))
	}
}

func findRepoRoot() (string, error) {
	// Walk up from PWD until we find go.mod
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found")
		}
		dir = parent
	}
}

func computeManifestDiff(committed, generated map[string]any) string {
	var diff []string

	// Compare methods
	committedMethods := extractMethods(committed)
	generatedMethods := extractMethods(generated)

	// Find added/removed/changed methods
	for name, genMethod := range generatedMethods {
		if comMethod, exists := committedMethods[name]; !exists {
			diff = append(diff, fmt.Sprintf("  + Method %q (added)", name))
		} else {
			if !equalJSON(comMethod, genMethod) {
				diff = append(diff, fmt.Sprintf("  ~ Method %q (signature changed)", name))
			}
		}
	}

	for name := range committedMethods {
		if _, exists := generatedMethods[name]; !exists {
			diff = append(diff, fmt.Sprintf("  - Method %q (removed)", name))
		}
	}

	// Compare types
	committedTypes := extractTypes(committed)
	generatedTypes := extractTypes(generated)

	for name, genType := range generatedTypes {
		if comType, exists := committedTypes[name]; !exists {
			diff = append(diff, fmt.Sprintf("  + Type %q (added)", name))
		} else {
			if !equalJSON(comType, genType) {
				// Try to identify what changed
				changes := describeTypeChange(comType, genType)
				diff = append(diff, fmt.Sprintf("  ~ Type %q: %s", name, changes))
			}
		}
	}

	for name := range committedTypes {
		if _, exists := generatedTypes[name]; !exists {
			diff = append(diff, fmt.Sprintf("  - Type %q (removed)", name))
		}
	}

	if len(diff) == 0 {
		return "  (no differences detected)"
	}

	return strings.Join(diff, "\n")
}

func extractMethods(manifest map[string]any) map[string]any {
	methods := make(map[string]any)
	if m, ok := manifest["methods"].([]any); ok {
		for _, method := range m {
			if mmap, ok := method.(map[string]any); ok {
				if name, ok := mmap["name"].(string); ok {
					methods[name] = method
				}
			}
		}
	}
	return methods
}

func extractTypes(manifest map[string]any) map[string]any {
	if types, ok := manifest["types"].(map[string]any); ok {
		return types
	}
	return make(map[string]any)
}

func describeTypeChange(committed, generated any) string {
	comMap, cok := committed.(map[string]any)
	genMap, gok := generated.(map[string]any)

	if !cok || !gok {
		return "structure changed"
	}

	// Check fields
	comFields := extractFields(comMap)
	genFields := extractFields(genMap)

	var changes []string

	for name, genField := range genFields {
		if comField, exists := comFields[name]; !exists {
			changes = append(changes, fmt.Sprintf("field %q added", name))
		} else {
			// Check JSON tag
			comTag := getJSONTag(comField)
			genTag := getJSONTag(genField)
			if comTag != genTag {
				changes = append(changes, fmt.Sprintf("field %q JSON tag: %q → %q", name, comTag, genTag))
			}
		}
	}

	for name := range comFields {
		if _, exists := genFields[name]; !exists {
			changes = append(changes, fmt.Sprintf("field %q removed", name))
		}
	}

	if len(changes) == 0 {
		return "changed"
	}

	return strings.Join(changes, ", ")
}

func extractFields(typeMap map[string]any) map[string]any {
	fields := make(map[string]any)
	if f, ok := typeMap["fields"].([]any); ok {
		for _, field := range f {
			if fmap, ok := field.(map[string]any); ok {
				if name, ok := fmap["name"].(string); ok {
					fields[name] = field
				}
			}
		}
	}
	return fields
}

func getJSONTag(field any) string {
	if fmap, ok := field.(map[string]any); ok {
		if tag, ok := fmap["jsonTag"].(string); ok {
			return tag
		}
	}
	return ""
}

func equalJSON(a, b any) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return bytes.Equal(aJSON, bJSON)
}

func hashBytes(b []byte) string {
	hash := uint64(14695981039346656037)
	for _, c := range b {
		hash ^= uint64(c)
		hash *= 1099511628211
	}
	return fmt.Sprintf("%016x", hash)
}
