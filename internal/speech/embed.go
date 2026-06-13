package speech

import _ "embed"

// kokoroScript is eigen's own copy of the Kokoro stdin TTS backend (vendored
// from the author's codex-desktop-linux read-aloud work, renamed to
// EIGEN_KOKORO_* env vars). Embedding makes eigen self-contained: the binary
// materializes the script under ~/.eigen/kokoro/ at detection time.
//
//go:embed embedded/kokoro_stdin.py
var kokoroScript string
