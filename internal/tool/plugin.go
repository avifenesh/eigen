package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// pluginSpec is one entry in a plugins.json file: a tool backed by an external
// command. The command receives the tool-call arguments as JSON on stdin and
// returns the result on stdout (a non-zero exit is an error, with stderr shown).
type pluginSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Command     []string        `json:"command"`
	ReadOnly    bool            `json:"readonly"`
	TimeoutSec  int             `json:"timeout_seconds"`
	Disabled    bool            `json:"disabled,omitempty"` // kept in config, not loaded

}

const defaultPluginTimeout = 60 * time.Second

// LoadPlugins reads plugin definitions from the given JSON files (missing files
// are skipped) and returns them as tool Definitions. A malformed file is an
// error; individual specs missing a name or command are an error too.
func LoadPlugins(paths ...string) ([]Definition, error) {
	var defs []Definition
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var specs []pluginSpec
		if err := json.Unmarshal(data, &specs); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, sp := range specs {
			if sp.Disabled {
				continue
			}
			d, err := pluginDefinition(sp)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			defs = append(defs, d)
		}
	}
	return defs, nil
}

// pluginDefinition turns a spec into a Definition with a command-exec Run.
func pluginDefinition(sp pluginSpec) (Definition, error) {
	if sp.Name == "" {
		return Definition{}, fmt.Errorf("plugin with empty name")
	}
	if len(sp.Command) == 0 {
		return Definition{}, fmt.Errorf("plugin %q has no command", sp.Name)
	}
	params := sp.Parameters
	if len(params) == 0 {
		params = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	timeout := defaultPluginTimeout
	if sp.TimeoutSec > 0 {
		timeout = time.Duration(sp.TimeoutSec) * time.Second
	}
	argv := sp.Command
	return Definition{
		Name:        sp.Name,
		Description: sp.Description,
		Parameters:  params,
		ReadOnly:    sp.ReadOnly,
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.Stdin = bytes.NewReader(args)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if ctx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("plugin %q timed out after %s", sp.Name, timeout)
			}
			if err != nil {
				msg := bytes.TrimSpace(stderr.Bytes())
				if len(msg) == 0 {
					msg = []byte(err.Error())
				}
				return "", fmt.Errorf("plugin %q failed: %s", sp.Name, msg)
			}
			return stdout.String(), nil
		},
	}, nil
}
