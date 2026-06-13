package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// ImageGenerator turns a prompt into image(s) — the generative-image model kind
// (Tier 18 #4). eigen had no image model; this adds one. First (and default)
// backend is Amazon Bedrock (Nova Canvas / Titan Image), reusing the same SigV4
// + AWS-profile auth as the Converse provider.
type ImageGenerator interface {
	// Generate renders the prompt to one or more PNG images (raw bytes).
	Generate(ctx context.Context, prompt string, opts ImageOpts) ([]Image, error)
	// ModelID is the backing model id (for logs/provenance).
	ModelID() string
}

// ImageOpts are optional generation controls (all have sane defaults).
type ImageOpts struct {
	Width  int // default 1024
	Height int // default 1024
	Count  int // default 1, capped per-model
	Seed   int // 0 = unset (random)
}

// bedrockImager generates images via Bedrock InvokeModel (Nova Canvas dialect:
// taskType TEXT_IMAGE → {images:[base64 png]}). Titan Image uses the same
// payload shape, so both work.
type bedrockImager struct {
	region  string
	profile string
	model   string
	http    *http.Client
}

// NewImageGenerator builds the configured image generator, or (nil,false) when
// disabled. Bedrock by default (auth shared with Converse):
//
//	EIGEN_IMAGE_MODEL   (default stability.stable-image-core-v1:1)
//	EIGEN_IMAGE_REGION  (default EIGEN_CONVERSE_REGION / AWS_REGION / us-west-2)
//	EIGEN_IMAGE_PROFILE (default EIGEN_CONVERSE_PROFILE / AWS_PROFILE / aviary)
//
// Image models are regional + account-gated; the Stability text-to-image models
// (stable-image-core/ultra, sd3.5) live in us-west-2. The payload dialect is
// chosen by model id prefix: "stability." → Stability shape, else Nova Canvas.
func NewImageGenerator() (ImageGenerator, bool) {
	model := firstNonEmptyEnv("EIGEN_IMAGE_MODEL", "stability.stable-image-core-v1:1")
	region := firstNonEmpty(os.Getenv("EIGEN_IMAGE_REGION"), os.Getenv("EIGEN_CONVERSE_REGION"), os.Getenv("AWS_REGION"), "us-west-2")
	profile := firstNonEmpty(os.Getenv("EIGEN_IMAGE_PROFILE"), os.Getenv("EIGEN_CONVERSE_PROFILE"), os.Getenv("AWS_PROFILE"), "aviary")
	return &bedrockImager{
		region:  region,
		profile: profile,
		model:   model,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}, true
}

func (b *bedrockImager) ModelID() string { return b.model }

// novaImageRequest is the Nova Canvas / Titan Image InvokeModel payload.
type novaImageRequest struct {
	TaskType              string         `json:"taskType"`
	TextToImageParams     map[string]any `json:"textToImageParams"`
	ImageGenerationConfig map[string]any `json:"imageGenerationConfig"`
}

type novaImageResponse struct {
	Images []string `json:"images"` // base64 PNG
	Error  string   `json:"error,omitempty"`
}

func (b *bedrockImager) Generate(ctx context.Context, prompt string, opts ImageOpts) ([]Image, error) {
	if opts.Width == 0 {
		opts.Width = 1024
	}
	if opts.Height == 0 {
		opts.Height = 1024
	}
	if opts.Count <= 0 {
		opts.Count = 1
	}
	if opts.Count > 4 {
		opts.Count = 4 // Nova Canvas allows up to 5; keep memory/latency modest
	}

	var body []byte
	if strings.HasPrefix(b.model, "stability.") {
		// Stability text-to-image dialect: {prompt, aspect_ratio, output_format,
		// seed?}. It returns ONE image per call (no count), so we loop below.
		req := map[string]any{
			"prompt":        prompt,
			"aspect_ratio":  aspectRatio(opts.Width, opts.Height),
			"output_format": "png",
		}
		if opts.Seed > 0 {
			req["seed"] = opts.Seed
		}
		body, _ = json.Marshal(req)
	} else {
		// Nova Canvas / Titan Image dialect.
		cfg := map[string]any{
			"numberOfImages": opts.Count,
			"width":          opts.Width,
			"height":         opts.Height,
			"quality":        "standard",
		}
		if opts.Seed > 0 {
			cfg["seed"] = opts.Seed
		}
		body, _ = json.Marshal(novaImageRequest{
			TaskType:              "TEXT_IMAGE",
			TextToImageParams:     map[string]any{"text": prompt},
			ImageGenerationConfig: cfg,
		})
	}

	// Stability returns one image per request; loop to honor count. Nova returns
	// count in a single request.
	reps := 1
	if strings.HasPrefix(b.model, "stability.") {
		reps = opts.Count
	}
	var all []Image
	for r := 0; r < reps; r++ {
		imgs, err := b.invoke(ctx, body)
		if err != nil {
			if len(all) > 0 {
				break // got some; return what we have
			}
			return nil, err
		}
		all = append(all, imgs...)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("image gen: no images returned")
	}
	return all, nil
}

// aspectRatio maps width×height to the closest Stability aspect_ratio enum
// (it takes a ratio string, not pixel dims).
func aspectRatio(w, h int) string {
	if w == 0 || h == 0 || w == h {
		return "1:1"
	}
	r := float64(w) / float64(h)
	switch {
	case r >= 1.7:
		return "16:9"
	case r >= 1.4:
		return "3:2"
	case r >= 1.2:
		return "5:4"
	case r <= 0.58:
		return "9:16"
	case r <= 0.7:
		return "2:3"
	case r <= 0.83:
		return "4:5"
	default:
		return "1:1"
	}
}

// invoke POSTs an InvokeModel request and decodes the {images:[base64]} result
// (both Nova and Stability use that field name).
func (b *bedrockImager) invoke(ctx context.Context, body []byte) ([]Image, error) {
	url := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/invoke", b.region, urlPathEscape(b.model))
	creds, err := loadAWSCreds(b.profile)
	if err != nil {
		return nil, fmt.Errorf("image credentials: %w", err)
	}
	sign := func(r *http.Request, by []byte) { signV4(r, by, creds, "bedrock", b.region, time.Now()) }
	raw, status, err := httpJSON(ctx, b.http, url, nil, body, sign)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("image gen HTTP %d: %s", status, string(raw))
	}
	var out novaImageResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("image gen: decode: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("image gen: %s", out.Error)
	}
	imgs := make([]Image, 0, len(out.Images))
	for _, b64 := range out.Images {
		data, derr := base64.StdEncoding.DecodeString(b64)
		if derr != nil || len(data) == 0 {
			continue
		}
		imgs = append(imgs, Image{MediaType: "image/png", Data: data})
	}
	return imgs, nil
}
