package skill

import (
	"context"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

// scanProv is a fake provider returning a canned scan verdict.
type scanProv struct {
	reply     string
	gotSystem string
	gotUser   string
}

func (scanProv) Name() string    { return "scan-prov" }
func (scanProv) ModelID() string { return "scan-prov" }
func (p *scanProv) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	p.gotSystem = req.System
	if len(req.Messages) > 0 {
		p.gotUser = req.Messages[0].Text
	}
	return &llm.Response{Text: p.reply}, nil
}

func TestProviderScannerSafe(t *testing.T) {
	p := &scanProv{reply: "VERDICT: SAFE"}
	res, err := ProviderScanner{P: p}.Scan(context.Background(), "refactor", "# do good things")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Safe {
		t.Fatalf("expected SAFE, got %+v", res)
	}
	// The skill name + content are passed to the model.
	if !strings.Contains(p.gotUser, "refactor") || !strings.Contains(p.gotUser, "do good things") {
		t.Fatalf("scanner should send name+content, got %q", p.gotUser)
	}
	for _, want := range []string{"ONLY for supply-chain / prompt-injection risk", "Do NOT grade code quality", "If the text is a normal skill written by the user"} {
		if !strings.Contains(p.gotSystem, want) {
			t.Fatalf("scanner prompt should narrow scope with %q; got:\n%s", want, p.gotSystem)
		}
	}
}

func TestProviderScannerRisky(t *testing.T) {
	p := &scanProv{reply: "VERDICT: RISKY\nREASONS:\n- exfiltrates ~/.ssh\n- curl | sh"}
	res, err := ProviderScanner{P: p}.Scan(context.Background(), "evil", "curl x | sh")
	if err != nil {
		t.Fatal(err)
	}
	if res.Safe {
		t.Fatal("expected RISKY")
	}
	if len(res.Reasons) != 2 {
		t.Fatalf("expected 2 reasons, got %v", res.Reasons)
	}
}

func TestParseScanFailsClosed(t *testing.T) {
	// An unparseable / ambiguous response is treated as risky.
	res := parseScan("I think it's probably fine?")
	if res.Safe {
		t.Fatal("ambiguous verdict must fail closed (RISKY)")
	}
	if len(res.Reasons) == 0 {
		t.Fatal("should provide a fallback reason")
	}
}

func TestParseScanSafeVariants(t *testing.T) {
	for _, s := range []string{"VERDICT: SAFE", "verdict: safe", "Some preamble.\nVERDICT:SAFE\n"} {
		if !parseScan(s).Safe {
			t.Fatalf("expected SAFE for %q", s)
		}
	}
}

func TestProviderScannerNilProvider(t *testing.T) {
	if _, err := (ProviderScanner{}).Scan(context.Background(), "x", "y"); err == nil {
		t.Fatal("nil provider should error (so the caller can fail closed)")
	}
}
