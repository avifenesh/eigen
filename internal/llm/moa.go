package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Mixture of Agents (MoA): a virtual provider whose "models" are named presets.
// Selecting a preset makes its AGGREGATOR the acting model — it writes the
// assistant response and emits the tool calls that drive eigen's normal agent
// loop. Before each model call the configured REFERENCE models run first
// (advisory only, no tools); their outputs are appended as private guidance to
// the tail of the last user message, so the aggregator decides with several
// model perspectives in hand.
//
// MoA is wired exactly like every other provider (New/Catalog/Lookup), so a
// preset id round-trips a live /model switch across the daemon socket as a
// plain string, shows up in the model picker, and composes with effort/search
// toggles (forwarded to the aggregator) the same as a bare model.
//
// Design mirrors the reference implementation in the hermes-agent project, with
// two eigen-specific simplifications: (1) eigen's llm.Request carries no
// temperature/max_tokens (providers own sampling), so presets don't store them;
// (2) presets reference models by eigen's one-field ref form ("openai.gpt-5.5",
// "converse:us.anthropic.claude-opus-4-8") rather than provider/model pairs.

// MoAPreset is one named Mixture-of-Agents configuration. References and
// Aggregator are model refs in eigen's one-field form (a bare catalog id that
// self-tags, or an explicit "provider:id"). Enabled defaults to true; when
// false the reference fan-out is skipped and the aggregator acts alone (the
// per-preset off switch).
type MoAPreset struct {
	Name       string   `json:"name"`
	References []string `json:"references"`
	Aggregator string   `json:"aggregator"`
	Enabled    *bool    `json:"enabled,omitempty"`
}

// enabled reports whether the reference fan-out runs for this preset (nil ==
// true; only an explicit false disables it).
func (p MoAPreset) enabled() bool { return p.Enabled == nil || *p.Enabled }

type moaFile struct {
	Presets []MoAPreset `json:"presets"`
}

// MoAPresetsPath returns the per-user MoA preset file path (~/.eigen/moa.json).
func MoAPresetsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "moa.json")
}

// moaFileMu serializes read-modify-write access to ~/.eigen/moa.json from
// within this process. The on-disk write is already atomic (temp + rename), so
// a reader never sees a partial file; this lock additionally prevents two
// concurrent UpsertMoAPreset/DeleteMoAPreset transactions in the same daemon
// from losing each other's update. (Cross-process races are out of scope — the
// CLI mutator is a separate short-lived process — but the atomic rename keeps
// even that case from ever corrupting the file.)
var moaFileMu sync.RWMutex

// LoadMoAPresets reads ~/.eigen/moa.json. A missing file is normal (no presets).
func LoadMoAPresets() ([]MoAPreset, error) {
	moaFileMu.RLock()
	defer moaFileMu.RUnlock()
	return loadMoAPresetsLocked()
}

// loadMoAPresetsLocked is the body of LoadMoAPresets; callers must hold
// moaFileMu (R or W) — used by the locked Upsert/Delete transactions.
func loadMoAPresetsLocked() ([]MoAPreset, error) {
	p := MoAPresetsPath()
	if p == "" {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f moaFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	presets := normalizeMoAPresets(f.Presets)
	if err := validateMoACatalog(presets); err != nil {
		return nil, err
	}
	return presets, nil
}

// SaveMoAPresets writes the complete preset list atomically (0600), validating
// first so a bad write can never land on disk.
func SaveMoAPresets(presets []MoAPreset) error {
	moaFileMu.Lock()
	defer moaFileMu.Unlock()
	return saveMoAPresetsLocked(presets)
}

// saveMoAPresetsLocked is the body of SaveMoAPresets; callers must hold
// moaFileMu for writing.
func saveMoAPresetsLocked(presets []MoAPreset) error {
	p := MoAPresetsPath()
	if p == "" {
		return os.ErrNotExist
	}
	presets = normalizeMoAPresets(presets)
	if err := validateMoACatalog(presets); err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	b, err := json.MarshalIndent(moaFile{Presets: presets}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".moa-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		return err
	}
	ok = true
	return nil
}

// UpsertMoAPreset validates and inserts/replaces a preset by name. The whole
// read-modify-write runs under moaFileMu so a concurrent upsert/delete in the
// same process can't clobber it.
func UpsertMoAPreset(preset MoAPreset) error {
	preset = normalizeMoAPreset(preset)
	moaFileMu.Lock()
	defer moaFileMu.Unlock()
	presets, err := loadMoAPresetsLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i := range presets {
		if presets[i].Name == preset.Name {
			presets[i] = preset
			replaced = true
			break
		}
	}
	if !replaced {
		presets = append(presets, preset)
	}
	return saveMoAPresetsLocked(presets)
}

// DeleteMoAPreset removes a preset by name. Removing an absent preset is a
// no-op. The read-modify-write runs under moaFileMu (see UpsertMoAPreset).
func DeleteMoAPreset(name string) error {
	name = strings.TrimSpace(name)
	moaFileMu.Lock()
	defer moaFileMu.Unlock()
	presets, err := loadMoAPresetsLocked()
	if err != nil {
		return err
	}
	out := presets[:0]
	for _, p := range presets {
		if p.Name != name {
			out = append(out, p)
		}
	}
	return saveMoAPresetsLocked(out)
}

func normalizeMoAPresets(in []MoAPreset) []MoAPreset {
	out := make([]MoAPreset, 0, len(in))
	seen := map[string]bool{}
	for _, p := range in {
		p = normalizeMoAPreset(p)
		if p.Name == "" || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out
}

func normalizeMoAPreset(p MoAPreset) MoAPreset {
	p.Name = strings.TrimSpace(p.Name)
	p.Aggregator = strings.TrimSpace(p.Aggregator)
	refs := make([]string, 0, len(p.References))
	for _, r := range p.References {
		if r = strings.TrimSpace(r); r != "" {
			refs = append(refs, r)
		}
	}
	p.References = refs
	return p
}

// validateMoACatalog checks the whole preset set (cross-preset name uniqueness
// is already enforced by normalizeMoAPresets dropping duplicates).
func validateMoACatalog(presets []MoAPreset) error {
	names := make(map[string]bool, len(presets))
	for _, p := range presets {
		names[p.Name] = true
	}
	for _, p := range presets {
		if err := validateMoAPreset(p, names); err != nil {
			name := p.Name
			if name == "" {
				name = "(unnamed)"
			}
			return fmt.Errorf("moa preset %s: %w", name, err)
		}
	}
	return nil
}

// ValidateMoAPreset checks one preset against the current set of preset names
// (used by the CLI/UI before an upsert). It loads the existing presets so the
// recursion guard can see sibling presets.
func ValidateMoAPreset(p MoAPreset) error {
	p = normalizeMoAPreset(p)
	names := map[string]bool{p.Name: true}
	if existing, err := LoadMoAPresets(); err == nil {
		for _, e := range existing {
			names[e.Name] = true
		}
	}
	return validateMoAPreset(p, names)
}

// validateMoAPreset enforces the preset invariants: a non-colliding name, a
// non-empty reference list and aggregator, and the recursion guard (no slot may
// point at a MoA preset — its own or a sibling's).
func validateMoAPreset(p MoAPreset, presetNames map[string]bool) error {
	if p.Name == "" {
		return fmt.Errorf("preset name is required")
	}
	if strings.ContainsAny(p.Name, " \t\n:/") {
		return fmt.Errorf("preset name must not contain whitespace, ':' or '/'")
	}
	if knownProvider(p.Name) {
		return fmt.Errorf("preset name %q is a reserved provider name", p.Name)
	}
	if _, ok := lookupNonMoA(p.Name); ok {
		return fmt.Errorf("preset name %q collides with an existing model id", p.Name)
	}
	if len(p.References) == 0 {
		return fmt.Errorf("at least one reference model is required")
	}
	if p.Aggregator == "" {
		return fmt.Errorf("an aggregator model is required")
	}
	for _, r := range p.References {
		if isMoARef(r, presetNames) {
			return fmt.Errorf("reference %q is a MoA preset; MoA presets cannot reference MoA (no recursion)", r)
		}
	}
	if isMoARef(p.Aggregator, presetNames) {
		return fmt.Errorf("aggregator %q is a MoA preset; a MoA aggregator cannot be another MoA preset (no recursion)", p.Aggregator)
	}
	return nil
}

// isMoARef reports whether a model ref points at the MoA virtual provider —
// either by an explicit "moa:" tag or by naming a known MoA preset. This is the
// recursion guard: it must catch both forms so a preset can never (directly)
// fan out into another MoA run. An explicit NON-moa tag (e.g. "custom:review")
// is taken at its word — it is never a MoA ref even if its bare id happens to
// collide with a preset name; only an UNTAGGED ref is matched against preset
// names.
func isMoARef(ref string, presetNames map[string]bool) bool {
	if tag, _ := ParseRef(ref); tag != "" {
		return canonicalProvider(tag) == "moa"
	}
	return presetNames[strings.TrimSpace(ref)]
}

func moaPresetByName(name string) (MoAPreset, bool) {
	presets, err := LoadMoAPresets()
	if err != nil {
		return MoAPreset{}, false
	}
	for _, p := range presets {
		if p.Name == name {
			return p, true
		}
	}
	return MoAPreset{}, false
}

// lookupNonMoA resolves a model id against the BUILT-IN catalog and custom
// providers only — deliberately excluding MoA presets. moaModels() needs an
// aggregator's window/capabilities, and that lookup must not re-enter Models()
// (which appends moaModels) or it would recurse. The recursion guard ensures an
// aggregator is never itself a MoA preset, so this restricted lookup is correct.
func lookupNonMoA(model string) (ModelInfo, bool) {
	if model == "" {
		return ModelInfo{}, false
	}
	_, model = ParseRef(model)
	models := append(append([]ModelInfo{}, Catalog...), customModels()...)
	for _, m := range models {
		if m.ID == model {
			return m, true
		}
	}
	var best ModelInfo
	found := false
	for _, m := range models {
		if strings.HasPrefix(model, m.ID) && (!found || len(m.ID) > len(best.ID)) {
			best, found = m, true
		}
	}
	return best, found
}

// moaModels exposes each saved preset as a selectable model under provider
// "moa". A preset inherits its AGGREGATOR's window and capabilities, because the
// aggregator is the acting model — context budgeting, vision routing, and the
// effort ladder all key off whatever model actually serves the turn.
func moaModels() []ModelInfo {
	presets, err := LoadMoAPresets()
	if err != nil {
		return nil
	}
	out := make([]ModelInfo, 0, len(presets))
	for _, p := range presets {
		agg, _ := lookupNonMoA(p.Aggregator)
		out = append(out, ModelInfo{
			ID:              p.Name,
			Provider:        "moa",
			ContextWindow:   agg.ContextWindow,
			Cache:           agg.Cache,
			Context1M:       agg.Context1M,
			ContextWindow1M: agg.ContextWindow1M,
			Reasoning:       agg.Reasoning,
			Effort:          agg.Effort,
			EffortLevels:    agg.EffortLevels,
			Vision:          agg.Vision,
			Search:          agg.Search,
			Social:          agg.Social,
		})
	}
	return out
}

// ── runtime ─────────────────────────────────────────────────────────────────

// moaMaxReferenceWorkers caps concurrent reference calls. References are
// independent advisory calls (no tools, no inter-dependence), so they fan out at
// once like a delegate batch; this bound just protects against a pathologically
// large preset opening dozens of sockets.
const moaMaxReferenceWorkers = 8

// moaReferenceTimeout bounds each reference call so one hung endpoint can't
// stall the turn — references are advisory, so a slow one is dropped (as a
// labelled note) rather than blocking the acting aggregator. The aggregator
// call itself is NEVER bounded here: it is the real model doing the work.
const moaReferenceTimeout = 90 * time.Second

// moaProvider is the acting-aggregator runtime for a preset. It implements
// Provider; Streamer/EffortSetter/Searcher/FastModer are forwarded to the
// aggregator so the acting model's live toggles work exactly as a bare model's.
type moaProvider struct {
	preset     string
	references []Provider
	refIDs     []string
	aggregator Provider
	aggID      string
	enabled    bool
}

// newMoAProvider builds the runtime for a preset: it constructs the aggregator
// and every reference provider up front (so credential/typo errors surface at
// switch time, not mid-turn). modelName is the preset id; empty selects the
// first saved preset.
func newMoAProvider(modelName string) (Provider, error) {
	preset, ok := moaPresetByName(modelName)
	if !ok {
		if modelName == "" {
			presets, err := LoadMoAPresets()
			if err != nil {
				return nil, fmt.Errorf("load MoA presets: %w", err)
			}
			if len(presets) == 0 {
				return nil, fmt.Errorf("no MoA presets configured (see: eigen moa configure)")
			}
			preset = presets[0]
		} else {
			return nil, fmt.Errorf("unknown MoA preset %q (see: eigen moa list)", modelName)
		}
	}

	if moaRefTargetsMoA(preset.Aggregator) {
		return nil, fmt.Errorf("moa preset %q aggregator cannot be another MoA preset", preset.Name)
	}
	agg, err := New("", preset.Aggregator)
	if err != nil {
		return nil, fmt.Errorf("moa preset %q aggregator %q: %w", preset.Name, preset.Aggregator, err)
	}

	m := &moaProvider{
		preset:     preset.Name,
		aggregator: agg,
		aggID:      preset.Aggregator,
		enabled:    preset.enabled(),
	}
	if m.enabled {
		for _, ref := range preset.References {
			if moaRefTargetsMoA(ref) {
				return nil, fmt.Errorf("moa preset %q reference %q cannot be a MoA preset", preset.Name, ref)
			}
			rp, err := New("", ref)
			if err != nil {
				return nil, fmt.Errorf("moa preset %q reference %q: %w", preset.Name, ref, err)
			}
			m.references = append(m.references, rp)
			m.refIDs = append(m.refIDs, ref)
		}
	}
	return m, nil
}

// moaRefTargetsMoA reports whether a model ref resolves to the MoA virtual
// provider — checked BEFORE construction so newMoAProvider can never recurse
// into New (which would dispatch a moa ref straight back here). Covers both an
// explicit "moa:" tag and a bare preset name (whose catalog provider is "moa").
func moaRefTargetsMoA(ref string) bool {
	return canonicalProvider(ResolveProvider("", strings.TrimSpace(ref))) == "moa"
}

func (m *moaProvider) Name() string {
	return fmt.Sprintf("%s · moa:%s", m.aggregator.Name(), m.preset)
}

// ModelID returns the preset name — NOT the aggregator's id — so a live /model
// switch round-trips the preset across the daemon socket and rebuilds the same
// MoA runtime on the other side.
func (m *moaProvider) ModelID() string { return m.preset }

// Complete runs the reference fan-out (when enabled), appends the reference
// context to the tail of the conversation, then lets the aggregator act with
// the full tool schema. The aggregator's response IS the MoA response. This
// mirrors hermes-agent's MoAChatCompletions.create.
func (m *moaProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	return m.aggregator.Complete(ctx, m.withReferences(ctx, req))
}

// Stream delegates streaming to the aggregator (the acting model) after the
// reference fan-out, so the acting model's text + reasoning deltas reach the UI
// exactly as for a bare model. An aggregator that can't stream falls back to a
// single final chunk via streamAny.
func (m *moaProvider) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	return streamAny(ctx, m.aggregator, m.withReferences(ctx, req), sink)
}

// withReferences runs the reference models and returns a request with their
// reference context appended to the tail of the last user message. When the
// preset is disabled (or has no references) the request is returned unchanged.
// This mirrors hermes-agent: references run before the aggregator and their
// joined outputs are appended as private context to the last user message.
func (m *moaProvider) withReferences(ctx context.Context, req Request) Request {
	if !m.enabled || len(m.references) == 0 {
		return req
	}
	outputs := m.runReferences(ctx, req)
	guidance := m.buildGuidance(outputs)
	if guidance == "" {
		return req
	}
	return appendToLastUser(req, guidance)
}

// moaRefOutput is one reference model's labelled result. Like hermes-agent, a
// failed reference is kept as a labelled note (e.g. "[failed: …]") rather than
// dropped, so the aggregator sees that a perspective was attempted.
type moaRefOutput struct {
	label string
	text  string
}

// runReferences fans out every reference model in parallel and returns their
// outputs in preset order (stable "Reference N" labelling). A reference never
// aborts the turn: a failure becomes a labelled note so the aggregator still
// acts with partial context — exactly as hermes-agent's _run_references_parallel.
func (m *moaProvider) runReferences(ctx context.Context, req Request) []moaRefOutput {
	refReq := Request{Messages: moaReferenceMessages(req)} // no system, no tools
	outputs := make([]moaRefOutput, len(m.references))
	sem := make(chan struct{}, moaMaxReferenceWorkers)
	var wg sync.WaitGroup
	for i := range m.references {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			label := m.refIDs[i]
			// Recover so one reference provider's panic can't take down the
			// whole daemon (shared across sessions) — convert it to a note.
			defer func() {
				if r := recover(); r != nil {
					outputs[i] = moaRefOutput{label: label, text: fmt.Sprintf("[failed: %v]", r)}
				}
			}()
			// Acquire a worker slot, but abandon immediately if the turn's
			// context is already cancelled rather than blocking behind the
			// in-flight workers — a cancelled parent should free the goroutine.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				outputs[i] = moaRefOutput{label: label, text: fmt.Sprintf("[failed: %v]", ctx.Err())}
				return
			}
			cctx, cancel := context.WithTimeout(ctx, moaReferenceTimeout)
			defer cancel()
			resp, err := m.references[i].Complete(cctx, refReq)
			switch {
			case err != nil:
				outputs[i] = moaRefOutput{label: label, text: fmt.Sprintf("[failed: %v]", err)}
			case resp == nil || strings.TrimSpace(resp.Text) == "":
				outputs[i] = moaRefOutput{label: label, text: "(empty response)"}
			default:
				outputs[i] = moaRefOutput{label: label, text: strings.TrimSpace(resp.Text)}
			}
		}(i)
	}
	wg.Wait()
	return outputs
}

// buildGuidance renders the reference outputs into the private context block
// appended to the conversation tail. This matches hermes-agent's
// MoAChatCompletions guidance verbatim in structure: a header naming the preset,
// the aggregator, and the references, then each reference's output.
func (m *moaProvider) buildGuidance(outputs []moaRefOutput) string {
	if len(outputs) == 0 {
		return ""
	}
	labels := make([]string, len(outputs))
	for i, o := range outputs {
		labels[i] = o.label
	}
	var b strings.Builder
	b.WriteString("[Mixture of Agents reference context]\n")
	fmt.Fprintf(&b, "Preset: %s\n", m.preset)
	fmt.Fprintf(&b, "Aggregator/acting model: %s\n", m.aggID)
	fmt.Fprintf(&b, "References: %s\n\n", strings.Join(labels, ", "))
	b.WriteString("Use the reference responses below as private context. You are the aggregator and acting model: answer the user directly or call tools as needed.\n")
	for i, o := range outputs {
		fmt.Fprintf(&b, "\nReference %d — %s:\n%s\n", i+1, o.label, o.text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// moaReferenceMessages builds an advisory-safe view of the conversation for
// reference models: only user/assistant TEXT turns. The system prompt is
// dropped (references aren't running eigen's loop and shouldn't re-bill it),
// tool-role messages and tool-call-only assistant turns are dropped (references
// emit no tools, and replaying orphan tool turns 400s strict providers). If
// trimming leaves nothing, fall back to the last user message so the references
// still have something to answer.
func moaReferenceMessages(req Request) []Message {
	var out []Message
	for _, msg := range req.Messages {
		if msg.Role != RoleUser && msg.Role != RoleAssistant {
			continue // drop tool results (system isn't in Messages anyway)
		}
		if strings.TrimSpace(msg.Text) == "" {
			continue // tool-call-only assistant turn — nothing advisory
		}
		out = append(out, Message{Role: msg.Role, Text: msg.Text})
	}
	if len(out) == 0 {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == RoleUser && strings.TrimSpace(req.Messages[i].Text) != "" {
				return []Message{{Role: RoleUser, Text: req.Messages[i].Text}}
			}
		}
	}
	return out
}

// appendToLastUser returns a copy of req with text appended to the last user
// message. Appending at the TAIL keeps the cached prefix (system prompt + prior
// history) byte-stable, so the aggregator gets a prompt-cache hit on everything
// above the freshly added guidance — the same shape as any normal new turn.
// When there is no user message, the guidance is added as a trailing user turn.
func appendToLastUser(req Request, text string) Request {
	msgs := make([]Message, len(req.Messages))
	copy(msgs, req.Messages)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			msgs[i].Text = strings.TrimRight(msgs[i].Text, "\n") + "\n\n" + text
			req.Messages = msgs
			return req
		}
	}
	req.Messages = append(msgs, Message{Role: RoleUser, Text: text})
	return req
}

// activeProvider is the target for forwarded runtime-capability calls: the
// aggregator, which is the acting model.
func (m *moaProvider) activeProvider() Provider { return m.aggregator }

func (m *moaProvider) SetEffort(level string) bool {
	if es, ok := m.activeProvider().(EffortSetter); ok {
		return es.SetEffort(level)
	}
	return false
}

func (m *moaProvider) Effort() string {
	if es, ok := m.activeProvider().(EffortSetter); ok {
		return es.Effort()
	}
	return ""
}

func (m *moaProvider) SetSearch(mode string) bool {
	if sr, ok := m.activeProvider().(Searcher); ok {
		return sr.SetSearch(mode)
	}
	return false
}

func (m *moaProvider) SearchMode() string {
	if sr, ok := m.activeProvider().(Searcher); ok {
		return sr.SearchMode()
	}
	return ""
}

func (m *moaProvider) SetFast(on bool) bool {
	if fm, ok := m.activeProvider().(FastModer); ok {
		return fm.SetFast(on)
	}
	return false
}

func (m *moaProvider) FastMode() bool {
	if fm, ok := m.activeProvider().(FastModer); ok {
		return fm.FastMode()
	}
	return false
}
