EIGEN := bin/eigen
PKGS := ./...

.PHONY: build gui-desktop vet test race fmt gate harness perf perf-soak perf-bench stats clean

build:
	go build -o $(EIGEN) .

gui-desktop:
	go build -tags 'wails dev webkit2_41' -o bin/eigen-gui .

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
