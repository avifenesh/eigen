package harness

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const chromeBridgeSourceDir = "embedded/chrome-bridge"
const ChromeBridgeHostName = "dev.agent_chrome_bridge"

func ChromeBridgeHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "chrome-bridge")
}

func ChromeBridgeMCPScript() string {
	home := ChromeBridgeHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "bin", "mcp-server.js")
}

func ChromeBridgeExtensionDir() string {
	home := ChromeBridgeHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "extension")
}

func ChromeBridgeInstalled() bool {
	for _, p := range []string{ChromeBridgeMCPScript(), filepath.Join(ChromeBridgeHome(), "bin", "native-host.js"), filepath.Join(ChromeBridgeExtensionDir(), "manifest.json")} {
		if p == "" {
			return false
		}
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

func InstallChromeBridge() (extensionDir string, manifests []string, extensionID string, err error) {
	home := ChromeBridgeHome()
	if home == "" {
		return "", nil, "", os.ErrNotExist
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", nil, "", err
	}
	_ = os.Chmod(home, 0o700)
	if err := copyEmbeddedTree(chromeBridgeSourceDir, home); err != nil {
		return "", nil, "", err
	}
	for _, rel := range []string{"bin/mcp-server.js", "bin/broker.js", "bin/native-host.js", "scripts/doctor.js", "scripts/extension-id.js"} {
		_ = os.Chmod(filepath.Join(home, rel), 0o755)
	}
	id, err := chromeExtensionID(filepath.Join(home, "extension", "manifest.json"))
	if err != nil {
		return "", nil, "", err
	}
	manifestPaths, err := writeChromeNativeHostManifests(home, id)
	if err != nil {
		return "", nil, "", err
	}
	return filepath.Join(home, "extension"), manifestPaths, id, nil
}

func copyEmbeddedFile(src, dst string) error {
	in, err := SourceFS.ReadFile(filepath.ToSlash(src))
	if err != nil {
		return err
	}
	info, err := SourceFS.Open(filepath.ToSlash(src))
	if err != nil {
		return err
	}
	defer info.Close()
	st, err := info.Stat()
	if err != nil {
		return err
	}
	mode := st.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	return writeFileAtomic(dst, in, mode)
}

func copyEmbeddedTree(srcDir, dstDir string) error {
	prefix := strings.TrimSuffix(srcDir, "/") + "/"
	return walkEmbedded(srcDir, func(path string, isDir bool) error {
		rel := strings.TrimPrefix(path, prefix)
		if rel == srcDir || rel == "" {
			return os.MkdirAll(dstDir, 0o755)
		}
		target := filepath.Join(dstDir, filepath.FromSlash(rel))
		if isDir {
			return os.MkdirAll(target, 0o755)
		}
		return copyEmbeddedFile(path, target)
	})
}

func walkEmbedded(root string, fn func(path string, isDir bool) error) error {
	entries, err := SourceFS.ReadDir(root)
	if err != nil {
		return err
	}
	if err := fn(root, true); err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.ToSlash(filepath.Join(root, e.Name()))
		if e.IsDir() {
			if err := walkEmbedded(p, fn); err != nil {
				return err
			}
			continue
		}
		if err := fn(p, false); err != nil {
			return err
		}
	}
	return nil
}

func chromeExtensionID(manifestPath string) (string, error) {
	var manifest struct {
		Key string `json:"key"`
	}
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return "", err
	}
	der, err := base64.StdEncoding.DecodeString(manifest.Key)
	if err != nil {
		return "", fmt.Errorf("decode extension key: %w", err)
	}
	h := sha256.Sum256(der)
	alphabet := "abcdefghijklmnop"
	var out strings.Builder
	for _, b := range h[:16] {
		out.WriteByte(alphabet[b>>4])
		out.WriteByte(alphabet[b&0x0f])
	}
	return out.String(), nil
}

func writeChromeNativeHostManifests(root, extensionID string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	manifest := map[string]any{
		"name":            ChromeBridgeHostName,
		"description":     "Eigen Chrome Connector native messaging host",
		"path":            filepath.Join(root, "bin", "native-host.js"),
		"type":            "stdio",
		"allowed_origins": []string{"chrome-extension://" + extensionID + "/"},
	}
	paths := []string{
		filepath.Join(home, ".config", "google-chrome", "NativeMessagingHosts", ChromeBridgeHostName+".json"),
		filepath.Join(home, ".config", "chromium", "NativeMessagingHosts", ChromeBridgeHostName+".json"),
	}
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := writeJSONAtomicHarness(p, manifest, 0o644); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func writeJSONAtomicHarness(path string, v any, perm os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), perm)
}

func writeFileAtomic(dst string, body []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		return err
	}
	ok = true
	return nil
}
