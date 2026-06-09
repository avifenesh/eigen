package dream

import (
	"context"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

type fakeProv struct {
	reply     string
	gotSystem string
	gotUser   string
}

func (f *fakeProv) Name() string { return "fake" }
func (f *fakeProv) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	f.gotSystem = req.System
	if len(req.Messages) > 0 {
		f.gotUser = req.Messages[len(req.Messages)-1].Text
	}
	return &llm.Response{Text: f.reply}, nil
}

func TestDistillParsesBullets(t *testing.T) {
	p := &fakeProv{reply: "Here are notes:\n- use `go test ./...` to test\n- entrypoint is main.go\n* bash is gated\nnot a bullet"}
	notes, err := Distill(context.Background(), p, []string{"user: build it\nassistant: done"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d: %v", len(notes), notes)
	}
	if notes[0] != "use `go test ./...` to test" {
		t.Fatalf("first note wrong: %q", notes[0])
	}
	if strings.Contains(strings.Join(notes, "\n"), "not a bullet") {
		t.Fatal("non-bullet lines must be excluded")
	}
}

func TestDistillDedupesAgainstExisting(t *testing.T) {
	p := &fakeProv{reply: "- use go test\n- new fact about caching"}
	notes, err := Distill(context.Background(), p, []string{"x: y"}, "- use go test\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0] != "new fact about caching" {
		t.Fatalf("should drop the already-known note, got %v", notes)
	}
}

func TestDistillNoTranscriptsIsNoop(t *testing.T) {
	p := &fakeProv{reply: "- something"}
	notes, err := Distill(context.Background(), p, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatal("no transcripts should produce no notes (and not call the model)")
	}
	if p.gotUser != "" {
		t.Fatal("model should not be called without transcripts")
	}
}

func TestDistillEmptyReplyIsNoop(t *testing.T) {
	p := &fakeProv{reply: "Nothing new worth saving."}
	notes, err := Distill(context.Background(), p, []string{"x: y"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("a reply with no bullets yields no notes, got %v", notes)
	}
}

func TestRenderSessionFlattens(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: "add a feature"},
		{Role: llm.RoleAssistant, Text: "done"},
		{Role: llm.RoleTool, Text: ""}, // empty: skipped
	}
	out := RenderSession(msgs)
	if !strings.Contains(out, "user: add a feature") || !strings.Contains(out, "assistant: done") {
		t.Fatalf("render wrong:\n%s", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("empty messages should be skipped:\n%s", out)
	}
}

func TestDistillNilProvider(t *testing.T) {
	if _, err := Distill(context.Background(), nil, []string{"x"}, ""); err == nil {
		t.Fatal("nil provider should error")
	}
}

func TestSynthesizeSkillParses(t *testing.T) {
	p := &fakeProv{reply: "NAME: deploy-flow\nDESCRIPTION: Use when deploying the service.\nBODY:\n1. build\n2. push\n3. verify"}
	d, ok, err := SynthesizeSkill(context.Background(), p, []string{"user: deploy\nassistant: ok"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("a well-formed draft should be ok")
	}
	if d.Name != "deploy-flow" || !strings.Contains(d.Body, "push") {
		t.Fatalf("draft parsed wrong: %+v", d)
	}
}

func TestSynthesizeSkillNone(t *testing.T) {
	p := &fakeProv{reply: "NONE"}
	_, ok, err := SynthesizeSkill(context.Background(), p, []string{"user: hi"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("NONE should yield ok=false")
	}
}

func TestSynthesizeSkillNoTranscripts(t *testing.T) {
	p := &fakeProv{reply: "NAME: x\nDESCRIPTION: y\nBODY:\nz"}
	_, ok, _ := SynthesizeSkill(context.Background(), p, nil)
	if ok {
		t.Fatal("no transcripts should not synthesize")
	}
}
