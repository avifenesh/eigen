// Package transcript imports conversation transcripts from other coding agents
// (Claude Code, Codex, Pi, Hermes) and eigen's own sessions, normalizing each
// into eigen's []llm.Message so a conversation can be resumed and continued.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// Source identifies a transcript format.
type Source string

const (
	SourceEigen    Source = "eigen"
	SourceClaude   Source = "claude"
	SourceCodex    Source = "codex"
	SourcePi       Source = "pi"
	SourceHermes   Source = "hermes"
	SourceOpenCode Source = "opencode"
)

// Import reads a transcript file, auto-detecting the source from its path.
func Import(path string) ([]llm.Message, error) {
	return ImportFrom(Detect(path), path)
}

// ImportFrom parses path using an explicit source.
func ImportFrom(src Source, path string) ([]llm.Message, error) {
	switch src {
	case SourceClaude:
		return parseClaude(path)
	case SourceCodex:
		return parseCodex(path)
	case SourcePi:
		return parsePi(path)
	case SourceHermes:
		return parseHermes(path)
	case SourceOpenCode:
		return ImportOpenCode(path, "")
	case SourceEigen, "":
		return Load(path)
	default:
		return nil, fmt.Errorf("unknown transcript source %q", src)
	}
}

// Detect guesses the source from the file path.
func Detect(path string) Source {
	switch {
	case strings.Contains(path, "/.claude/projects/"):
		return SourceClaude
	case strings.Contains(path, "/.codex/sessions/"):
		return SourceCodex
	case strings.Contains(path, "/.pi/agent/sessions/"):
		return SourcePi
	case strings.Contains(path, "/.hermes/sessions/"):
		return SourceHermes
	case path == "opencode" || strings.Contains(path, "opencode.db") || strings.Contains(path, "/opencode/"):
		return SourceOpenCode
	default:
		return SourceEigen
	}
}

// Save writes messages as eigen-native JSONL (one llm.Message per line).
// The write is atomic (temp file + rename) so a crash, force-exit, or
// concurrent reader never sees a truncated transcript — this file is the
// durable record of the conversation.
func Save(path string, msgs []llm.Message) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	f := tmp
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	rotateBackups(path)
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// Best-effort directory fsync makes the rename durable across sudden power
	// loss on filesystems that require fsync(dir) for metadata persistence.
	_ = syncDir(filepath.Dir(path))
	return nil
}

const transcriptBackupGenerations = 5 // .bak + .bak.1 .. .bak.4

func backupPath(path string, gen int) string {
	if gen == 0 {
		return path + ".bak"
	}
	return fmt.Sprintf("%s.bak.%d", path, gen)
}

func rotateBackups(path string) {
	old, err := os.ReadFile(path)
	if err != nil || len(old) == 0 {
		return
	}
	_ = os.Remove(backupPath(path, transcriptBackupGenerations-1))
	for i := transcriptBackupGenerations - 2; i >= 0; i-- {
		_ = os.Rename(backupPath(path, i), backupPath(path, i+1))
	}
	_ = os.WriteFile(backupPath(path, 0), old, 0o644)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Load reads an eigen-native JSONL session file.
func Load(path string) ([]llm.Message, error) {
	return scanJSONL(path, func(line []byte, out *[]llm.Message) error {
		var m llm.Message
		if err := json.Unmarshal(line, &m); err != nil {
			return err
		}
		*out = append(*out, m)
		return nil
	})
}

// scanJSONL reads a JSONL file, invoking fn per non-empty line to append to the
// result. Per-line decode errors are skipped (transcripts contain mixed types).
func scanJSONL(path string, fn func(line []byte, out *[]llm.Message) error) ([]llm.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []llm.Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	for sc.Scan() {
		b := sc.Bytes()
		if len(strings.TrimSpace(string(b))) == 0 {
			continue
		}
		// Per-line errors are non-fatal: skip malformed/unknown lines.
		_ = fn(b, &out)
	}
	return out, sc.Err()
}

// rawArgs normalizes tool arguments to a valid JSON object. Some sources encode
// arguments as a JSON object, others as a JSON-encoded string.
func rawArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	// If it's a JSON string containing JSON (e.g. "{\"a\":1}"), unwrap it.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if strings.TrimSpace(s) == "" {
			return json.RawMessage("{}")
		}
		return json.RawMessage(s)
	}
	return raw
}
