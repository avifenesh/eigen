// Command manifest generates bridge.manifest.json: a deterministic golden
// manifest of the Bridge's dispatcher-exposed RPC methods. Includes method
// names, parameter types (with full struct-field JSON tags for every
// recursively reachable DTO), and result types — everything needed to catch
// agent-driven renames that would silently break the Qt client under the
// reflect dispatcher.
//
// Usage: go:generate directive in internal/gui/bridge.go or manual:
//
//	go run ./internal/gui/gen/manifest
//
// Output: internal/gui/bridge.manifest.json (committed to git).
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/gui"
)

func main() {
	manifest := generateManifest()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fail(fmt.Errorf("marshal manifest: %w", err))
	}

	// Write to internal/gui/bridge.manifest.json
	repoRoot, err := findRepoRoot()
	if err != nil {
		fail(err)
	}
	outPath := filepath.Join(repoRoot, "internal", "gui", "bridge.manifest.json")
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		fail(fmt.Errorf("write manifest: %w", err))
	}

	fmt.Fprintf(os.Stderr, "manifest: wrote %s (%d methods, %d types)\n",
		outPath, len(manifest.Methods), len(manifest.Types))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "manifest: %v\n", err)
	os.Exit(1)
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

type Manifest struct {
	Methods []Method           `json:"methods"`
	Types   map[string]TypeDef `json:"types"`
}

type Method struct {
	Name    string   `json:"name"`
	Params  []Param  `json:"params"`
	Results []string `json:"results"`
}

type Param struct {
	Type string `json:"type"`
}

type TypeDef struct {
	Kind   string            `json:"kind"` // struct, slice, map, basic, interface
	Fields []Field           `json:"fields,omitempty"`
	Elem   string            `json:"elem,omitempty"`  // for slice/array
	Key    string            `json:"key,omitempty"`   // for map
	Value  string            `json:"value,omitempty"` // for map
	Hash   string            `json:"hash,omitempty"`  // SHA256 of canonical form
	Tags   map[string]string `json:"tags,omitempty"`  // json tags for verification
}

type Field struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	JSONTag string            `json:"jsonTag,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"` // all struct tags
}

func generateManifest() *Manifest {
	bridgeType := reflect.TypeOf(&gui.Bridge{})

	// Collect dispatcher-exposed methods (skip Start/Stop/SetEmitter/Shutdown/SetApp)
	skip := map[string]bool{
		"Start": true, "Stop": true, "SetEmitter": true, "Shutdown": true, "SetApp": true,
	}

	var methods []Method
	seenTypes := make(map[string]bool)
	typeQueue := []reflect.Type{}

	for i := 0; i < bridgeType.NumMethod(); i++ {
		m := bridgeType.Method(i)
		if !m.IsExported() || skip[m.Name] {
			continue
		}

		method := Method{Name: m.Name}
		mt := m.Type

		// Collect params (skip receiver, optionally skip context.Context first param)
		paramOffset := 1 // skip receiver
		if mt.NumIn() > 1 {
			firstParam := mt.In(1)
			// Check if first param is context.Context
			if firstParam.String() == "context.Context" {
				paramOffset = 2
			}
		}

		for j := paramOffset; j < mt.NumIn(); j++ {
			pt := mt.In(j)
			typeName := typeString(pt)
			method.Params = append(method.Params, Param{Type: typeName})
			queueType(pt, seenTypes, &typeQueue)
		}

		// Collect results (skip trailing error)
		for j := 0; j < mt.NumOut(); j++ {
			rt := mt.Out(j)
			// Skip error results
			if rt.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
				continue
			}
			typeName := typeString(rt)
			method.Results = append(method.Results, typeName)
			queueType(rt, seenTypes, &typeQueue)
		}

		methods = append(methods, method)
	}

	// Sort methods by name for determinism
	sort.Slice(methods, func(i, j int) bool { return methods[i].Name < methods[j].Name })

	// Build type definitions recursively
	types := make(map[string]TypeDef)
	visitedTypes := make(map[reflect.Type]bool)

	for len(typeQueue) > 0 {
		t := typeQueue[0]
		typeQueue = typeQueue[1:]

		if visitedTypes[t] {
			continue
		}
		visitedTypes[t] = true

		ts := typeString(t)
		if _, exists := types[ts]; exists {
			continue
		}

		def := buildTypeDef(t, seenTypes, &typeQueue)
		types[ts] = def
	}

	return &Manifest{Methods: methods, Types: types}
}

func queueType(t reflect.Type, seen map[string]bool, queue *[]reflect.Type) {
	ts := typeString(t)
	if !seen[ts] && !isBasicType(t) {
		seen[ts] = true
		*queue = append(*queue, t)
	}
}

func buildTypeDef(t reflect.Type, seen map[string]bool, queue *[]reflect.Type) TypeDef {
	// Unwrap pointers
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	def := TypeDef{}

	switch t.Kind() {
	case reflect.Struct:
		def.Kind = "struct"
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}

			field := Field{
				Name: f.Name,
				Type: typeString(f.Type),
			}

			// Extract all struct tags
			if f.Tag != "" {
				field.Tags = make(map[string]string)
				// Parse json tag
				if jsonTag := f.Tag.Get("json"); jsonTag != "" {
					field.JSONTag = jsonTag
					field.Tags["json"] = jsonTag
				}
				// Also capture other tags that might affect wire format
				for _, tagKey := range []string{"yaml", "xml", "form", "binding"} {
					if tagVal := f.Tag.Get(tagKey); tagVal != "" {
						field.Tags[tagKey] = tagVal
					}
				}
			}

			def.Fields = append(def.Fields, field)
			queueType(f.Type, seen, queue)
		}

		// Sort fields by name for determinism
		sort.Slice(def.Fields, func(i, j int) bool {
			return def.Fields[i].Name < def.Fields[j].Name
		})

	case reflect.Slice, reflect.Array:
		def.Kind = "slice"
		def.Elem = typeString(t.Elem())
		queueType(t.Elem(), seen, queue)

	case reflect.Map:
		def.Kind = "map"
		def.Key = typeString(t.Key())
		def.Value = typeString(t.Elem())
		queueType(t.Key(), seen, queue)
		queueType(t.Elem(), seen, queue)

	case reflect.Interface:
		def.Kind = "interface"

	default:
		def.Kind = "basic"
	}

	// Compute hash of canonical representation
	def.Hash = hashTypeDef(def)

	return def
}

func hashTypeDef(def TypeDef) string {
	// Canonical representation for hashing
	var parts []string
	parts = append(parts, "kind:"+def.Kind)
	if def.Elem != "" {
		parts = append(parts, "elem:"+def.Elem)
	}
	if def.Key != "" {
		parts = append(parts, "key:"+def.Key)
	}
	if def.Value != "" {
		parts = append(parts, "value:"+def.Value)
	}
	for _, f := range def.Fields {
		parts = append(parts, fmt.Sprintf("field:%s:%s:%s", f.Name, f.Type, f.JSONTag))
	}
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("%x", h[:8]) // first 8 bytes
}

func typeString(t reflect.Type) string {
	// Handle pointers
	ptr := ""
	for t.Kind() == reflect.Ptr {
		ptr += "*"
		t = t.Elem()
	}

	// For named types from packages, use full path
	if t.PkgPath() != "" {
		// Simplify internal package paths
		pkg := t.PkgPath()
		if strings.HasPrefix(pkg, "github.com/avifenesh/eigen/internal/") {
			pkg = strings.TrimPrefix(pkg, "github.com/avifenesh/eigen/internal/")
		}
		return ptr + pkg + "." + t.Name()
	}

	// For unnamed composite types (slices, maps, etc.)
	switch t.Kind() {
	case reflect.Slice:
		return ptr + "[]" + typeString(t.Elem())
	case reflect.Array:
		return ptr + fmt.Sprintf("[%d]", t.Len()) + typeString(t.Elem())
	case reflect.Map:
		return ptr + "map[" + typeString(t.Key()) + "]" + typeString(t.Elem())
	case reflect.Chan:
		return ptr + "chan " + typeString(t.Elem())
	case reflect.Func:
		return ptr + "func"
	default:
		return ptr + t.String()
	}
}

func isBasicType(t reflect.Type) bool {
	// Unwrap pointers
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.String:
		return true
	}

	// Also treat well-known types as basic
	if t.PkgPath() == "" {
		switch t.String() {
		case "error", "interface{}":
			return true
		}
	}

	return false
}
