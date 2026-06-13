package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// imageGenRunner builds the generate_image tool's runner for a session rooted
// at dir: render the prompt via the configured image model (Bedrock default),
// save the PNG(s) under <dir>/eigen-images/, and return the paths + the images
// inline (so they reach the model through the tool-result image plumbing).
// Saves into the project so the user finds the output where they work.
func imageGenRunner(dir string) tool.ImageGenRun {
	return func(ctx context.Context, prompt string, width, height, count int) ([]string, []llm.Image, error) {
		gen, ok := llm.NewImageGenerator()
		if !ok {
			return nil, nil, fmt.Errorf("image generation unavailable (no image model configured)")
		}
		imgs, err := gen.Generate(ctx, prompt, llm.ImageOpts{Width: width, Height: height, Count: count})
		if err != nil {
			return nil, nil, err
		}
		outDir := filepath.Join(dir, "eigen-images")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("save dir: %w", err)
		}
		stamp := time.Now().Format("20060102-150405")
		var paths []string
		for i, img := range imgs {
			name := stamp + ".png"
			if len(imgs) > 1 {
				name = fmt.Sprintf("%s-%d.png", stamp, i+1)
			}
			p := filepath.Join(outDir, name)
			if err := os.WriteFile(p, img.Data, 0o644); err != nil {
				return nil, nil, fmt.Errorf("write %s: %w", p, err)
			}
			paths = append(paths, p)
		}
		return paths, imgs, nil
	}
}
