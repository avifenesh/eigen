EIGEN := bin/eigen
# All packages EXCEPT internal/gui: the GUI package imports Wails, which pulls in
# webkitgtk via cgo. The default gate (CI's Go-gate job) builds webkit-free, so
# it skips internal/gui — that package is built/vetted/tested under the wails
# tag by the separate gui-phase gate (scripts/verify-gui-phase.sh). Local builds
# use the default gtk4/webkitgtk-6.0 backend; CI's runner has only webkit2gtk-4.1
# so that gate adds the `gtk3` tag.
PKGS := $(shell go list ./... | grep -v '/internal/gui')

.PHONY: build gui-run gui-smoke gui-desktop gui-frontend vet test race fmt gate harness perf perf-soak perf-bench stats clean

build:
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

# Full pre-commit gate: build, vet, test, fmt-check.
gate: build vet test
	@test -z "$$(gofmt -l . | grep -v '^vendor/')" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	@echo "gate: OK"

# Build/install Eigen-bundled helpers for the full local harness
# (orientation + connector-only chrome bridge + computer-use-linux +
# agent-workspace-linux). Requires Rust/Cargo only for the desktop helpers.
harness: build
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
stats: build
	$(EIGEN) daemon stats

clean:
	rm -f $(EIGEN)
