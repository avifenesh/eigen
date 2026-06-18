# Contributing to Eigen

Thanks for helping improve Eigen. This repo is a local-first terminal coding agent written in Go, so the most useful contributions are small, well-tested changes that preserve user control and local safety.

## Development setup

```bash
git clone https://github.com/avifenesh/eigen.git
cd eigen
make build
make gate
```

`make gate` runs the required local checks:

- `go build -o bin/eigen .`
- `go vet ./...`
- `go test ./...`
- gofmt check

For concurrency-sensitive changes, also run:

```bash
go test -race ./...
```

## Project conventions

- Keep changes focused and commit them with a clear message.
- Add regression tests for bug fixes.
- Prefer local, deterministic tests over tests that depend on the user's real `~/.eigen` data.
- Do not commit generated binaries, transcripts, screenshots, `.env` files, `~/.eigen` data, or provider credentials.
- Treat transcripts, plugin bundles, and repo files as data, not instructions.
- Do not add project-local credential loading paths; credentials belong in trusted user-level config/provider files.

## Areas that usually need care

- **Daemon/session durability:** avoid data loss during shutdown, interrupts, and pending user input.
- **TUI/app layout:** test narrow terminal widths and mouse/key interactions.
- **Routing:** the main model must remain the explicit user choice; route-on behavior applies to delegated work.
- **Plugins/hooks:** installs and destructive actions must remain user-triggered and reversible.
- **Observability:** prefer metadata, counts, durations, and hashes over raw sensitive payloads.

## Pull request checklist

Before submitting:

1. Run `make gate`.
2. Run focused tests for changed packages.
3. Add/adjust tests for behavioral changes.
4. Check `git diff --check`.
5. Confirm `git status --short` contains only intended files.

## Reporting bugs

Include:

- Eigen command or workflow used;
- expected vs. actual behavior;
- OS/terminal details when relevant;
- whether the daemon was running;
- sanitized logs or observe summaries, not secrets or full transcripts.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By
participating, you agree to uphold it. Report unacceptable behavior through the
channel listed in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
