EIGEN := bin/eigen
PKGS := ./...

.PHONY: build vet test race fmt gate perf perf-soak perf-bench stats clean

build:
	go build -o $(EIGEN) .

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

# Tier 23 performance + resource-health guard.
#  - soak: session/attach/detach churn must not leak goroutines or sessions.
#  - bench: per-event wire encode + stats snapshot allocs/latency baselines.
perf: perf-soak perf-bench
	@echo "perf: OK"

perf-soak:
	go test ./internal/daemon/ ./internal/agent/ \
		-run 'TestSoak|TestBgRegistry|TestReplayBuffer' -count=1 -v

perf-bench:
	go test ./internal/daemon/ -run x -bench 'BenchmarkWireEventEncode|BenchmarkHostStats' -benchmem -benchtime 2000x

# Live daemon resource snapshot (default instance). EIGEN_INSTANCE=dev for dev.
stats: build
	$(EIGEN) daemon stats

clean:
	rm -f $(EIGEN)
