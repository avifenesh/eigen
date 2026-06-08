package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// mantleDefaultRegion is where Bedrock serves the OpenAI-family models today.
// GPT-5.5 is us-east-2 in-region only, so we default here rather than reusing
// AWS_REGION (which may point at us-east-1 for the Converse/Claude path).
const mantleDefaultRegion = "us-east-2"

// Mantle drives an OpenAI-family model on the Bedrock "mantle" endpoint via the
// OpenAI Responses API. Auth is a Bedrock long-term API key (Bearer token).
type Mantle struct {
	BaseURL string
	Model   string
	token   string
	http    *http.Client
}

// NewMantle builds a Mantle provider from the environment.
// Requires AWS_BEARER_TOKEN_BEDROCK; region defaults to us-east-2.
func NewMantle(model string) (*Mantle, error) {
	token := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	if token == "" {
		return nil, fmt.Errorf("AWS_BEARER_TOKEN_BEDROCK not set")
	}
	region := os.Getenv("EIGEN_MANTLE_REGION")
	if region == "" {
		region = mantleDefaultRegion
	}
	if model == "" {
		model = "openai.gpt-5.5"
	}
	return &Mantle{
		BaseURL: fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1", region),
		Model:   model,
		token:   token,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (m *Mantle) Name() string { return m.Model + " (bedrock mantle)" }

// responsesInputItem is one entry in the Responses API "input" array.
type responsesInputItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesRequest struct {
	Model string               `json:"model"`
	Input []responsesInputItem `json:"input"`
}

type responsesReply struct {
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (m *Mantle) Complete(ctx context.Context, req Request) (*Response, error) {
	input := make([]responsesInputItem, 0, len(req.Messages)+1)
	if req.System != "" {
		// The Responses API uses the "developer" role for system instructions.
		input = append(input, responsesInputItem{Role: "developer", Content: req.System})
	}
	for _, msg := range req.Messages {
		input = append(input, responsesInputItem{Role: string(msg.Role), Content: msg.Text})
	}

	body, err := json.Marshal(responsesRequest{Model: m.Model, Input: input})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mantle request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mantle HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var reply responsesReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if reply.Error != nil {
		return nil, fmt.Errorf("mantle error: %s", reply.Error.Message)
	}

	var text string
	for _, item := range reply.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				text += part.Text
			}
		}
	}
	return &Response{Text: text}, nil
}
