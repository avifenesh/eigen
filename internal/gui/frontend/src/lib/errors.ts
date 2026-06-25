// errText — normalize any caught value into clean, human-readable text.
//
// Wails surfaces a Go bound-method error as a RuntimeError whose `.message` is
// sometimes a JSON envelope, e.g.
//   {"message":"GlideEc2: sh: 1: /home/ubuntu/.local/bin/eigen: not found","cause":{},"kind":"RuntimeError"}
// Showing that blob raw (a `{e.message}` in a view) leaks JSON + a "RuntimeError"
// kind at the user. This unwraps to the inner `message` so views render the
// actual reason ("GlideEc2: … eigen: not found"), falling back gracefully for
// plain Errors, strings, and nested envelopes.
export function errText(e: unknown): string {
  const raw =
    e instanceof Error ? e.message : typeof e === "string" ? e : e == null ? "" : String(e);
  return unwrapEnvelope(raw) || "Something went wrong.";
}

// Peel a JSON error envelope ({message|error|cause}) down to its innermost text.
// Bounded recursion so a self-referential blob can't loop. Non-JSON passes
// through untouched (the common case).
function unwrapEnvelope(s: string, depth = 0): string {
  const trimmed = s.trim();
  if (depth > 4 || !trimmed.startsWith("{")) return trimmed;
  try {
    const obj = JSON.parse(trimmed);
    const inner = obj?.message ?? obj?.error ?? obj?.cause;
    if (typeof inner === "string" && inner.trim() !== "" && inner.trim() !== trimmed) {
      return unwrapEnvelope(inner, depth + 1);
    }
    // An object cause (further nesting) — recurse into its message.
    if (inner && typeof inner === "object" && typeof inner.message === "string") {
      return unwrapEnvelope(inner.message, depth + 1);
    }
    return trimmed;
  } catch {
    return trimmed; // not valid JSON after all
  }
}
