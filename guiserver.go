package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/feed"
	"github.com/avifenesh/eigen/internal/gui"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/session"
)

const guiserverMaxLine = 32 * 1024 * 1024 // 32MB

// runGUIServerCmd implements the `eigen guiserver` subcommand: a headless
// Bridge service over a Unix socket. The Bridge owns zero Wails state — it
// compiles tagless, so it joins `make gate`. Two connection types: RPC for
// request/reply multiplexing, events for push-only streams. Bootstrap mirrors
// main_gui_wails.go (suggester + dirs), Start() with loop ownership, serve
// until SIGINT/SIGTERM, linger 5 minutes after last disconnect, then Stop().
func runGUIServerCmd() {
	ctx, cancel := signalContext()
	defer cancel()

	bridge := gui.NewBridge(ensureDaemon, guiserverSuggester(), guiserverProjectDirs)
	sockEmitter := newSocketEmitter()
	bridge.SetEmitter(sockEmitter)
	bridge.Start()
	defer bridge.Stop()

	sockPath := socketPath()
	if err := os.MkdirAll(filepath.Dir(sockPath), 0755); err != nil {
		fail(fmt.Errorf("guiserver socket dir: %w", err))
	}

	// Remove stale socket on bind if connect fails (mirrors daemon socket pattern).
	if _, err := os.Stat(sockPath); err == nil {
		if c, derr := net.Dial("unix", sockPath); derr == nil {
			c.Close()
			fail(fmt.Errorf("guiserver already running at %s", sockPath))
		}
		_ = os.Remove(sockPath)
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		fail(fmt.Errorf("guiserver listen: %w", err))
	}
	if err := os.Chmod(sockPath, 0600); err != nil {
		ln.Close()
		_ = os.Remove(sockPath) // clean up the socket file on chmod failure
		fail(fmt.Errorf("guiserver chmod: %w", err))
	}
	defer func() {
		ln.Close()
		_ = os.Remove(sockPath)
	}()

	fmt.Fprintf(os.Stderr, "eigen guiserver: listening on %s\n", sockPath)

	dispatcher := newRPCDispatcher(bridge)
	srv := &guiServer{
		bridge:     bridge,
		emitter:    sockEmitter,
		dispatcher: dispatcher,
		conns:      make(map[*guiConn]struct{}),
		linger:     make(chan struct{}),
	}

	go srv.serve(ctx, ln)

	// Wait for shutdown signal OR linger expiry.
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "eigen guiserver: shutdown signal received")
	case <-srv.linger:
		fmt.Fprintln(os.Stderr, "eigen guiserver: linger expired (no clients for 5 minutes)")
	}

	srv.closeAll()
}

func socketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "guiserver.sock")
}

// guiServer manages the socket lifecycle + per-conn dispatch + linger timer.
type guiServer struct {
	bridge     *gui.Bridge
	emitter    *socketEmitter
	dispatcher *rpcDispatcher

	mu         sync.Mutex
	conns      map[*guiConn]struct{}
	lingerStop func() // non-nil when linger timer is running
	linger     chan struct{}
}

func (s *guiServer) serve(ctx context.Context, ln net.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *guiServer) handleConn(ctx context.Context, raw net.Conn) {
	defer raw.Close()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "guiserver: connection panic: %v\n", r)
		}
	}()

	// Cancel linger when a client connects.
	s.mu.Lock()
	if s.lingerStop != nil {
		s.lingerStop()
		s.lingerStop = nil
	}
	s.mu.Unlock()

	sc := bufio.NewScanner(raw)
	sc.Buffer(make([]byte, 0, 64*1024), guiserverMaxLine)

	// First line declares role: {"role":"rpc"} or {"role":"events"}.
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "guiserver: scanner error on role read: %v\n", err)
		}
		return
	}
	var roleMsg struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(sc.Bytes(), &roleMsg); err != nil {
		return
	}

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	gc := &guiConn{
		role:   roleMsg.Role,
		raw:    raw,
		ctx:    connCtx,
		cancel: cancel,
	}

	s.mu.Lock()
	s.conns[gc] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.conns, gc)
		remaining := len(s.conns)
		s.mu.Unlock()

		if remaining == 0 {
			s.startLinger()
		}
	}()

	switch gc.role {
	case "rpc":
		s.handleRPC(gc, sc)
	case "events":
		s.handleEvents(gc, sc)
	default:
		return
	}
}

func (s *guiServer) handleRPC(gc *guiConn, sc *bufio.Scanner) {
	var writeMu sync.Mutex
	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b = append(b, '\n')
		writeMu.Lock()
		_, werr := gc.raw.Write(b)
		writeMu.Unlock()
		return werr
	}

	for sc.Scan() {
		select {
		case <-gc.ctx.Done():
			return
		default:
		}

		var req rpcRequest
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = send(rpcResponse{ID: 0, Error: "bad request"})
			continue
		}

		// hello is handled by the server (not Bridge).
		if req.Call == "hello" {
			sha, manifest := buildInfo()
			_ = send(rpcResponse{ID: req.ID, Result: map[string]string{"sha": sha, "manifest": manifest}})
			continue
		}

		// Dispatch to Bridge via reflection in a goroutine (id correlates).
		go func(r rpcRequest) {
			result, err := s.dispatcher.dispatch(gc.ctx, r.Call, r.Args)
			if err != nil {
				_ = send(rpcResponse{ID: r.ID, Error: err.Error()})
				return
			}
			_ = send(rpcResponse{ID: r.ID, Result: result})
		}(req)
	}
	// Log scanner errors (e.g., line exceeds 32MB budget).
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "guiserver: rpc scanner error: %v\n", err)
	}
}

func (s *guiServer) handleEvents(gc *guiConn, sc *bufio.Scanner) {
	var writeMu sync.Mutex
	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b = append(b, '\n')
		writeMu.Lock()
		_, werr := gc.raw.Write(b)
		writeMu.Unlock()
		return werr
	}

	sub := newSubscription(256, send) // 256-message bounded queue
	s.emitter.register(sub)
	defer s.emitter.unregister(sub)

	for sc.Scan() {
		select {
		case <-gc.ctx.Done():
			return
		default:
		}

		var msg struct {
			Sub   []string `json:"sub,omitempty"`
			Unsub []string `json:"unsub,omitempty"`
		}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			continue
		}
		for _, ch := range msg.Sub {
			sub.subscribe(ch)
		}
		for _, ch := range msg.Unsub {
			sub.unsubscribe(ch)
		}
	}
	// Log scanner errors (e.g., line exceeds 32MB budget).
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "guiserver: events scanner error: %v\n", err)
	}
}

func (s *guiServer) startLinger() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lingerStop != nil {
		return // already running
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.lingerStop = cancel

	go func() {
		timer := time.NewTimer(5 * time.Minute)
		defer timer.Stop()
		select {
		case <-timer.C:
			close(s.linger)
		case <-ctx.Done():
			return
		}
	}()
}

func (s *guiServer) closeAll() {
	s.mu.Lock()
	conns := make([]*guiConn, 0, len(s.conns))
	for gc := range s.conns {
		conns = append(conns, gc)
	}
	s.mu.Unlock()

	for _, gc := range conns {
		gc.cancel()
		_ = gc.raw.Close()
	}
}

type guiConn struct {
	role   string
	raw    net.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

// ---- RPC protocol ----

type rpcRequest struct {
	ID   int               `json:"id"`
	Call string            `json:"call"`
	Args []json.RawMessage `json:"args"`
}

type rpcResponse struct {
	ID     int    `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// rpcDispatcher reflects over *gui.Bridge to expose all exported methods by
// name, automatically and forever. Methods first param can be context.Context
// (injected); trailing error return becomes the error field; multiple
// non-error results marshal as an array.
type rpcDispatcher struct {
	bridge  *gui.Bridge
	methods map[string]reflect.Method
}

func newRPCDispatcher(b *gui.Bridge) *rpcDispatcher {
	t := reflect.TypeOf(b)
	methods := make(map[string]reflect.Method)

	// Collect exported methods that are not Start/Stop/SetEmitter/Shutdown.
	skip := map[string]bool{
		"Start":      true,
		"Stop":       true,
		"SetEmitter": true,
		"Shutdown":   true,
		"SetApp":     true, // wails-only coupling
	}

	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if !m.IsExported() || skip[m.Name] {
			continue
		}
		methods[m.Name] = m
	}

	return &rpcDispatcher{bridge: b, methods: methods}
}

func (d *rpcDispatcher) dispatch(ctx context.Context, name string, args []json.RawMessage) (any, error) {
	m, ok := d.methods[name]
	if !ok {
		return nil, fmt.Errorf("unknown method %q", name)
	}

	mt := m.Type
	// receiver is arg 0 (method is bound to *Bridge)
	numIn := mt.NumIn() - 1

	// Check if first param is context.Context.
	injectCtx := false
	paramOffset := 1 // skip receiver
	if numIn > 0 && mt.In(1).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
		injectCtx = true
		paramOffset = 2
		numIn--
	}

	if len(args) != numIn {
		return nil, fmt.Errorf("method %q expects %d args, got %d", name, numIn, len(args))
	}

	// Build call args.
	callArgs := []reflect.Value{reflect.ValueOf(d.bridge)}
	if injectCtx {
		callArgs = append(callArgs, reflect.ValueOf(ctx))
	}

	for i, raw := range args {
		paramType := mt.In(paramOffset + i)
		paramVal := reflect.New(paramType)
		if err := json.Unmarshal(raw, paramVal.Interface()); err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		callArgs = append(callArgs, paramVal.Elem())
	}

	// Call.
	results := m.Func.Call(callArgs)

	// Extract results + error.
	var (
		out []any
		err error
	)
	for _, r := range results {
		if r.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			// Only call IsNil() for kinds that support it (Chan, Func, Interface,
			// Map, Pointer, Slice). A Bridge method returning a concrete non-pointer
			// error type (e.g., func() MyError) would panic here otherwise.
			if canBeNil(r.Kind()) && !r.IsNil() {
				err = r.Interface().(error)
			} else if !canBeNil(r.Kind()) {
				// Concrete error type (non-nil by definition if it exists).
				err = r.Interface().(error)
			}
			continue
		}
		out = append(out, r.Interface())
	}

	if err != nil {
		return nil, err
	}

	// Single result → bare; multiple → array.
	if len(out) == 0 {
		return nil, nil
	}
	if len(out) == 1 {
		return out[0], nil
	}
	return out, nil
}

// canBeNil reports whether a reflect.Kind supports IsNil(). Calling IsNil() on
// kinds not in this set panics with "reflect: call of reflect.Value.IsNil on
// <kind> Value". Used to guard the error-extraction logic above.
func canBeNil(k reflect.Kind) bool {
	switch k {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	default:
		return false
	}
}

// ---- Events protocol ----

// socketEmitter fans out Emit(name, data) to subscribed connections. Each
// connection has a bounded 256-message queue; on overflow drop messages and
// send {"event":"dropped","channel":"<name>"} once per burst.
type socketEmitter struct {
	mu   sync.Mutex
	subs []*subscription
}

func newSocketEmitter() *socketEmitter {
	return &socketEmitter{}
}

func (e *socketEmitter) Emit(name string, data any) {
	// Map event names: session events → "session:<id>"; everything else → literal.
	channel := mapEventChannel(name)

	e.mu.Lock()
	subs := append([]*subscription{}, e.subs...)
	e.mu.Unlock()

	for _, sub := range subs {
		sub.emit(channel, data)
	}
}

func (e *socketEmitter) register(sub *subscription) {
	e.mu.Lock()
	e.subs = append(e.subs, sub)
	e.mu.Unlock()
}

func (e *socketEmitter) unregister(sub *subscription) {
	e.mu.Lock()
	for i, s := range e.subs {
		if s == sub {
			e.subs = append(e.subs[:i], e.subs[i+1:]...)
			break
		}
	}
	e.mu.Unlock()
	sub.close()
}

// subscription is one client's event stream with per-connection subscriptions
// and a bounded queue. Overflow → drop + send "dropped" notice once. The
// closed flag guards against send-on-closed-channel races when unregister()
// closes the queue while a concurrent emit() still holds a queue ref.
type subscription struct {
	mu       sync.Mutex
	channels map[string]bool
	queue    chan eventWire
	send     func(any) error
	dropping bool
	closed   bool
}

type eventWire struct {
	Event   string `json:"event"`
	Channel string `json:"channel"`
	Data    any    `json:"data,omitempty"`
}

func newSubscription(qsize int, send func(any) error) *subscription {
	sub := &subscription{
		channels: make(map[string]bool),
		queue:    make(chan eventWire, qsize),
		send:     send,
	}
	go sub.pump()
	return sub
}

func (sub *subscription) subscribe(ch string) {
	sub.mu.Lock()
	sub.channels[ch] = true
	sub.mu.Unlock()
}

func (sub *subscription) unsubscribe(ch string) {
	sub.mu.Lock()
	delete(sub.channels, ch)
	sub.mu.Unlock()
}

func (sub *subscription) emit(channel string, data any) {
	// Acquire the lock ONCE and hold it for the entire channel-check + queue-send
	// decision. This prevents the send-on-closed-channel race: unregister() can
	// now only close the queue AFTER all emits have either sent or seen closed=true.
	sub.mu.Lock()
	defer sub.mu.Unlock()

	if sub.closed || !sub.channels[channel] {
		return
	}

	ev := eventWire{Event: "data", Channel: channel, Data: data}

	select {
	case sub.queue <- ev:
		sub.dropping = false
	default:
		// Queue full. Send "dropped" notice once per burst (best-effort; if the
		// queue is STILL full after setting dropping=true, the notice is silently
		// lost — acceptable since the client is already behind).
		if !sub.dropping {
			sub.dropping = true
			dropped := eventWire{Event: "dropped", Channel: channel}
			select {
			case sub.queue <- dropped:
			default:
			}
		}
	}
}

func (sub *subscription) pump() {
	for ev := range sub.queue {
		_ = sub.send(ev)
	}
}

func (sub *subscription) close() {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.closed {
		return // idempotent: double-close is a no-op
	}
	sub.closed = true
	close(sub.queue)
}

// mapEventChannel maps Bridge event names to client channel names. Session
// events (eigen:session:<id>:event) → "session:<id>"; all others → literal.
func mapEventChannel(name string) string {
	// Session events: "eigen:session:<id>:event" → "session:<id>"
	if strings.HasPrefix(name, "eigen:session:") && strings.HasSuffix(name, ":event") {
		parts := strings.Split(name, ":")
		if len(parts) >= 3 {
			return "session:" + parts[2]
		}
	}
	// Session closed: "eigen:session:<id>:closed" → "session:<id>"
	if strings.HasPrefix(name, "eigen:session:") && strings.HasSuffix(name, ":closed") {
		parts := strings.Split(name, ":")
		if len(parts) >= 3 {
			return "session:" + parts[2]
		}
	}
	// Everything else → literal (eigen:daemon:stats, eigen:daemon:health, etc.)
	return name
}

// buildInfo returns the VCS revision (SHA) and a manifest hash for the hello
// handshake. The manifest hash catches agent-driven renames (a renamed JSON
// tag silently breaks Qt under the reflect dispatcher). The hash is computed
// from the committed golden manifest file (internal/gui/bridge.manifest.json).
func buildInfo() (sha, manifest string) {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				sha = s.Value
				break
			}
		}
	}
	return sha, gui.ManifestHash()
}

// guiserverSuggester adapts a suggestion model into the proactive feed's
// Suggester. Mirrors the TUI/Wails pattern: EIGEN_SUGGEST_MODEL, else
// glm-5.2, else the small model. Nil when none can be built.
func guiserverSuggester() feed.Suggester {
	prov := guiserverSuggestProvider()
	if prov == nil {
		return nil
	}
	return func(ctx context.Context, system, prompt string) (string, error) {
		resp, err := prov.Complete(ctx, llm.Request{
			System:   system,
			Messages: []llm.Message{{Role: llm.RoleUser, Text: prompt}},
		})
		if err != nil {
			return "", err
		}
		return resp.Text, nil
	}
}

func guiserverSuggestProvider() llm.Provider {
	if id := os.Getenv("EIGEN_SUGGEST_MODEL"); id != "" {
		if p, err := llm.New("", id); err == nil {
			return p
		}
	}
	// Prefer glm-5.2 (1M-ctx, web_search included) with fallback to small model.
	small := smallProvider(nil)
	if llm.ProviderAvailable("glm") {
		if p, err := llm.New("glm", "glm-5.2"); err == nil {
			return llm.NewFallback(p, small)
		}
	}
	return small
}

// guiserverProjectDirs returns the distinct working dirs across saved sessions
// (newest-first), the universe the feed scans for loose ends.
func guiserverProjectDirs() []string {
	store, err := session.SharedOpen()
	if err != nil || store == nil {
		if wd, e := os.Getwd(); e == nil {
			return []string{wd}
		}
		return nil
	}
	_ = store.Discover()
	seen := map[string]bool{}
	var dirs []string
	for _, m := range store.List() {
		if m.Cwd == "" || seen[m.Cwd] {
			continue
		}
		if st, e := os.Stat(m.Cwd); e != nil || !st.IsDir() {
			continue
		}
		seen[m.Cwd] = true
		dirs = append(dirs, m.Cwd)
	}
	if len(dirs) == 0 {
		if wd, e := os.Getwd(); e == nil {
			dirs = append(dirs, wd)
		}
	}
	return dirs
}
