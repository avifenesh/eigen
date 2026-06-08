package session

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
)

const titlePrompt = `Give a short, specific title (max 6 words, no quotes, no trailing punctuation) for a coding-assistant conversation that begins with the following user message. Reply with only the title.`

// Titler names a conversation from its opening text.
type Titler interface {
	Title(ctx context.Context, head string) (string, error)
}

// ProviderTitler titles via a (preferably small/cheap) model.
type ProviderTitler struct{ P llm.Provider }

func (t ProviderTitler) Title(ctx context.Context, head string) (string, error) {
	if len(head) > 4000 {
		head = head[:4000]
	}
	resp, err := t.P.Complete(ctx, llm.Request{
		System:   titlePrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: head}},
	})
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(resp.Text)
	title = strings.Trim(title, `"'`)
	if i := strings.IndexByte(title, '\n'); i >= 0 {
		title = title[:i]
	}
	if len(title) > 80 {
		title = title[:80]
	}
	return title, nil
}

// TitleUntitled titles, in the background, up to `limit` of the most recent
// sessions that still lack a title, reading only a cheap head of each
// transcript. It returns immediately; titles fill in and persist as they land.
func (s *Store) TitleUntitled(ctx context.Context, t Titler, limit int) {
	if t == nil {
		return
	}
	todo := s.List() // newest first, deduped
	var pick []*Meta
	for _, m := range todo {
		if strings.TrimSpace(m.Title) == "" {
			pick = append(pick, m)
			if limit > 0 && len(pick) >= limit {
				break
			}
		}
	}
	if len(pick) == 0 {
		return
	}

	go func() {
		sem := make(chan struct{}, 3) // bounded concurrency
		var wg sync.WaitGroup
		for _, m := range pick {
			head := firstUserText(m.Source, m.Origin)
			if strings.TrimSpace(head) == "" {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(m *Meta, head string) {
				defer wg.Done()
				defer func() { <-sem }()
				title, err := t.Title(ctx, head)
				if err != nil || title == "" {
					return
				}
				s.mu.Lock()
				m.Title = title
				s.save()
				s.mu.Unlock()
			}(m, head)
		}
		wg.Wait()
	}()
}

// firstUserText cheaply extracts the first user message from a transcript
// without a full parse (reads a bounded prefix of the file).
func firstUserText(src transcript.Source, origin string) string {
	if src == transcript.SourceOpenCode {
		return "" // OpenCode titles come from its DB
	}
	f, err := os.Open(origin)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lines := 0
	for sc.Scan() && lines < 300 {
		lines++
		line := sc.Bytes()
		if t := userTextFromLine(src, line); t != "" {
			return t
		}
	}
	return ""
}

func userTextFromLine(src transcript.Source, line []byte) string {
	switch src {
	case transcript.SourceHermes:
		var r struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(line, &r) == nil && r.Role == "user" {
			return r.Content
		}
	case transcript.SourceClaude, transcript.SourcePi:
		var r struct {
			Type    string `json:"type"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &r) == nil && r.Message.Role == "user" {
			var str string
			if json.Unmarshal(r.Message.Content, &str) == nil {
				return str
			}
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(r.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					if b.Type == "text" {
						return b.Text
					}
				}
			}
		}
	case transcript.SourceCodex:
		var r struct {
			Type    string `json:"type"`
			Payload struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &r) == nil && r.Type == "response_item" && r.Payload.Type == "message" && r.Payload.Role == "user" {
			for _, c := range r.Payload.Content {
				if c.Text != "" {
					return c.Text
				}
			}
		}
	case transcript.SourceEigen:
		var m llm.Message
		if json.Unmarshal(line, &m) == nil && m.Role == llm.RoleUser {
			return m.Text
		}
	}
	return ""
}
