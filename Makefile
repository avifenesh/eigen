EIGEN := bin/eigen
# All packages including internal/gui (now tagless after guiserver migration).
# The GUI package previously imported Wails (webkitgtk via cgo) and was excluded
# from the default gate. Post-guiserver it compiles headless, so it joins the
# webkit-free gate. The Wails build (bin/eigen-gui) stays separate with its own
# gui-phase gate (scripts/verify-gui-phase.sh), but internal/gui tests run in
# both contexts (tagless for contract tests, wails tag for integration).
PKGS := $(shell go list ./...)

.PHONY: build core gui-run gui-smoke gui-desktop gui-legacy gui-frontend vet test race fmt gate harness perf perf-soak perf-bench stats clean

# `make` / `make build` builds EVERYTHING: the core CLI/daemon binary
# (bin/eigen) and the desktop GUI (bin/eigen-gui, Svelte frontend embedded,
# `wails production` tags). A plain checkout build gives you both, ready to run.
#
# `make core` builds ONLY the webkit-free CLI/daemon binary — no GUI, no cgo
# webkit dependency. Use it when you don't need the desktop shell (and it's
# what the webkit-free CI Go-gate builds; `gate`/`harness`/`stats` depend on it,
# never on the GUI).
build: core gui-desktop
	@echo "make: OK — bin/eigen (CLI/daemon) + bin/eigen-gui (desktop GUI)"

core:
	go build -o $(EIGEN) .

# Build the Svelte bundle into internal/gui/frontend/dist, which the Go binary
# embeds via go:embed. The bundle is NOT committed (gitignored), so the GUI
# targets below MUST build it first or the binary embeds a stale/missing
# frontend — the cause of "my GUI fix didn't show up after rebuild". Uses pnmp
# if present, else npm.
gui-frontend:
	cd internal/gui/frontend && (command -v pnpm >/dev/null 2>&1 && pnpm install --frozen-lockfile && pnpm build || (npm ci && npm run build))

gui-run: gui-frontend
	go run -tags 'wails production' . gui

gui-desktop: gui-frontend
	go build -tags 'wails production' -o bin/eigen-gui .

gui-legacy: gui-frontend
	go build -tags 'wails production' -o bin/eigen-gui-legacy .

gui-smoke:
	scripts/gui-smoke.sh

vet:
	go vet $(PKGS)

test:
	go test $(PKGS)

race:
	go test -race ./internal/daemon/ ./internal/agent/

fmt:
	gofmt -l -w .

# Full pre-commit gate: core build (webkit-free, matches CI's Go-gate runner),
# vet, test, fmt-check. Does NOT build the GUI — that's the separate gui-phase
# gate (scripts/verify-gui-phase.sh).
gate: core vet test
	@test -z "$$(gofmt -l . | grep -v '^vendor/')" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	@echo "gate: OK"

# Build/install Eigen-bundled helpers for the full local harness
# (orientation + connector-only chrome bridge + computer-use-linux +
# agent-workspace-linux). Requires Rust/Cargo only for the desktop helpers.
harness: core
	$(EIGEN) harness install

# Tier 23 performance + resource-health guard.
#  - soak: session/attach/detach churn must not leak goroutines or sessions.
#  - bench: per-event wire encode + stats snapshot allocs/latency baselines.
#  - tokens: token-efficiency guards (cache parsing, schema compaction,
#    memory cap, compaction trigger, subtask effort).
perf: perf-soak perf-tokens perf-bench
	@echo "perf: OK"

perf-soak:
	go test ./internal/daemon/ ./internal/agent/ \
		-run 'TestSoak|TestBgRegistry|TestReplayBuffer' -count=1 -v

perf-tokens:
	go test ./internal/llm/ ./internal/tool/ ./internal/memory/ ./internal/agent/ ./internal/daemon/ \
		-run 'Usage|CacheHitRate|Cached|RegistryCompacts|CompactJSON|ClampMemoryTail|MaybeCompactFiresAtThreshold|ApplySubtaskEffort|StatsAggregatesCacheTokens' \
		-count=1 -v

perf-bench:
	go test ./internal/daemon/ -run x -bench 'BenchmarkWireEventEncode|BenchmarkHostStats' -benchmem -benchtime 2000x

# Live daemon resource snapshot (default instance). EIGEN_INSTANCE=dev for dev.
stats: core
	$(EIGEN) daemon stats

clean:
	rm -f $(EIGEN) bin/eigen-gui
