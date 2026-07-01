package gui

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// ManifestHash returns a hash of the committed golden manifest file. This is
// the shared implementation used by both guiserver's hello handshake and the
// gate test. The hash is computed from the canonical JSON bytes of
// bridge.manifest.json, so any change to the manifest (method signature,
// DTO field, JSON tag) changes the hash.
//
// Exported so guiserver.go and manifest_test.go can both use it.
func ManifestHash() string {
	// Simple FNV-1a hash over the committed manifest bytes.
	hash := uint64(14695981039346656037)
	for _, b := range manifestBytes {
		hash ^= uint64(b)
		hash *= 1099511628211
	}
	return fmt.Sprintf("%016x", hash)
}

// ManifestData returns the parsed manifest for testing/introspection.
func ManifestData() (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, err
	}
	return m, nil
}

//go:embed bridge.manifest.json
var manifestBytes []byte
