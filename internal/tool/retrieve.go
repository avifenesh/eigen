package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RetrieveRun is injected by main/buildSession: semantic search over the
// project's indexed files. Returns formatted top-k hits, or an error when no
// embedder is configured (retrieval is optional).
type RetrieveRun func(ctx context.Context, query string, k int) (string, error)

// Retrieve returns the semantic-search tool (Tier 18 #2): the model queries in
// natural language and gets the most RELEVANT code/text spans by meaning —
// retrieving context on demand instead of the user pasting whole files. Backed
// by a local embedder + a per-project vector index. ReadOnly (pure read).
func Retrieve(run RetrieveRun) Definition {
	return Definition{
		Name:        "retrieve",
		Description: "Semantic search over this project's files: find the code/text most relevant to a natural-language query by MEANING (not just keywords). Use it to locate where something is handled, recall how a thing works, or pull just the relevant context instead of reading whole files. Returns the top matching spans (path:lines + snippet). Complements grep (exact text) — use retrieve for 'where/how is X done', grep for a known string.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": { "type": "string", "description": "What you're looking for, in natural language (e.g. 'where are auth tokens validated', 'how is the context budget computed')." },
    "k": { "type": "integer", "description": "How many spans to return (default 8, max 20)." }
  },
  "required": ["query"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Query string `json:"query"`
				K     int    `json:"k"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(in.Query) == "" {
				return "", fmt.Errorf("query is required")
			}
			if run == nil {
				return "", fmt.Errorf("retrieval is unavailable (no embedder configured — set EIGEN_EMBED_BASE_URL / run the embedder)")
			}
			if in.K <= 0 {
				in.K = 8
			}
			if in.K > 20 {
				in.K = 20
			}
			return run(ctx, in.Query, in.K)
		},
	}
}
