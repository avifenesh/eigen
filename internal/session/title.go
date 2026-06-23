package session

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"sort"
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

// titleJob is an immutable snapshot of the per-session fields the background
// titler needs. Snapshotting decouples the worker goroutines from the shared
// *Meta pointers (which Discover/upsert mutate under s.mu), so the worker never
// reads m.Source/m.Origin off the live map without the lock — it carries copies
// and re-looks-up the meta by id under s.mu only when it writes the title.
type titleJob struct {
	id     string
	source transcript.Source
	origin string
}

// TitleUntitled titles, in the background, up to `limit` of the most recent
// sessions that still lack a title, reading only a cheap head of each
// transcript. It returns immediately; titles fill in and persist as they land.
func (s *Store) TitleUntitled(ctx context.Context, t Titler, limit int) {
	if t == nil {
		return
	}
	// Snapshot the untitled sessions' id/source/origin into value copies under
	// s.mu, so neither this loop nor any worker ever reads a live *Meta field
	// (Title/Source/Origin) that Discover/upsert mutates concurrently.
	pick := s.untitledJobs(limit)
	if len(pick) == 0 {
		return
	}

	go func() {
		sem := make(chan struct{}, 3) // bounded concurrency
		var wg sync.WaitGroup
		for _, job := range pick {
			head := firstUserText(job.source, job.origin)
			if strings.TrimSpace(head) == "" {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(job titleJob, head string) {
				defer wg.Done()
				defer func() { <-sem }()
				title, err := t.Title(ctx, head)
				if err != nil || title == "" {
					return
				}
				s.setTitleIfEmpty(job.id, title)
			}(job, head)
		}
		wg.Wait()
	}()
}

// untitledJobs snapshots, under s.mu, up to `limit` of the most recent
// still-untitled sessions (newest-first, fingerprint-deduped — matching List)
// into value copies. Reading m.Title/m.Source/m.Origin here, while holding the
// lock, is what lets the background titler run without touching the live *Meta
// pointers that Discover/upsert mutate.
func (s *Store) untitledJobs(limit int) []titleJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	metas := make([]*Meta, 0, len(s.metas))
	for _, m := range s.metas {
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Updated > metas[j].Updated })
	seen := map[string]bool{}
	var jobs []titleJob
	for _, m := range metas {
		if m.Fingerprint != "" {
			if seen[m.Fingerprint] {
				continue
			}
			seen[m.Fingerprint] = true
		}
		if strings.TrimSpace(m.Title) != "" {
			continue
		}
		jobs = append(jobs, titleJob{id: m.ID, source: m.Source, origin: m.Origin})
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}
	return jobs
}

// setTitleIfEmpty looks the meta up by id and, if it still exists and lacks a
// title, sets it and persists — all under s.mu, so the background titler never
// mutates a *Meta that Discover/upsert may be writing concurrently. It only
// fills an empty title so it never clobbers one a meanwhile-running Discover
// peek already derived.
func (s *Store) setTitleIfEmpty(id, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.metas[id]
	if m == nil || strings.TrimSpace(m.Title) != "" {
		return
	}
	m.Title = title
	s.save()
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
