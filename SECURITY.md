# Security Policy

Eigen is a local-first coding agent with access to files, shell commands, model providers, plugins, hooks, and local session data. Please report security issues responsibly.

## Supported versions

Security fixes target the `main` branch unless a release branch is explicitly maintained.

## Reporting a vulnerability

If GitHub private vulnerability reporting is enabled for this repository, use it.

If it is not enabled, open a minimal public issue asking for a private security contact. Do **not** include exploit details, tokens, transcripts, private paths, or other sensitive material in a public issue.

Useful report details:

- affected commit/version;
- operating system and terminal environment;
- provider/plugin/hook path involved, if any;
- minimal reproduction steps;
- impact and suggested mitigation;
- sanitized logs or observe summaries.

## Security expectations

- Do not commit credentials, `.env` files, provider auth files, custom provider files with inline API keys, transcripts, screenshots, or `~/.eigen` runtime data.
- Project-local repositories must not be able to change Eigen's credential or permission posture through `.env` files.
- Bundled harness helpers (`orientation`, `computer-use-linux`, `agent-workspace-linux`) are installed only by explicit user action (`eigen harness install`, `eigen orientation install`, `eigen computer-use install`, or `eigen workspace install`).
- Plugin installs and destructive plugin operations must remain explicit user actions.
- Observability should record metadata, counts, durations, and hashes rather than raw sensitive payloads.
- Remote-control features should fail closed and avoid unauthenticated public daemon access.
