package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// ImageGenRun is injected by main/buildSession: render a prompt to images and
// save them under the project, returning the saved paths. Returns the images
// too (so they ride the tool-result image plumbing — the model sees its
// output). nil run → tool reports "unavailable".
type ImageGenRun func(ctx context.Context, prompt string, width, height, count int) (paths []string, images []llm.Image, err error)

// GenerateImage returns the image-generation tool (Tier 18 #4): eigen could not
// produce images before. The model describes an image; the tool renders it
// (Bedrock by default) and saves PNG(s) into the project, returning the paths
// AND the images inline (so the model can see what it made and iterate).
// Mutating (writes files) — gated like other writers.
func GenerateImage(run ImageGenRun) Definition {
	return Definition{
		Name:        "generate_image",
		Description: "Generate image(s) from a text prompt (diagrams, mockups, assets, illustrations) and save them as PNG files in the project. Returns the saved file paths and the images themselves. Describe the image concretely (subject, style, composition, colors). Optional width/height (default 1024×1024) and count (default 1).",
		ReadOnly:    false, // writes PNG files
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "prompt": { "type": "string", "description": "Concrete description of the image to generate (subject, style, composition, colors)." },
    "width":  { "type": "integer", "description": "Image width in px (default 1024)." },
    "height": { "type": "integer", "description": "Image height in px (default 1024)." },
    "count":  { "type": "integer", "description": "How many variations to generate (default 1, max 4)." }
  },
  "required": ["prompt"],
  "additionalProperties": false
}`),
		RunRich: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var in struct {
				Prompt string `json:"prompt"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
				Count  int    `json:"count"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{}, fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(in.Prompt) == "" {
				return Result{}, fmt.Errorf("prompt is required")
			}
			if run == nil {
				return Result{}, fmt.Errorf("image generation is unavailable (no image model configured)")
			}
			paths, images, err := run(ctx, in.Prompt, in.Width, in.Height, in.Count)
			if err != nil {
				return Result{}, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "generated %d image(s):\n", len(paths))
			for _, p := range paths {
				b.WriteString("  " + p + "\n")
			}
			return Result{Text: strings.TrimRight(b.String(), "\n"), Images: images}, nil
		},
	}
}
