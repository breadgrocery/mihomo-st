# AGENTS.md

## Role

This repository is a Go CLI and REST API for testing mihomo proxy nodes. It reads
only top-level `proxies` from mihomo-compatible YAML configs.

Keep the project narrow. Do not turn it into a full mihomo runtime: no proxy
groups, proxy listener, DNS hijacking, rules engine, or unrelated runtime
features. Treat any upstream mihomo checkout as reference only; do not vendor it
or add a local `replace` for `github.com/metacubex/mihomo`.

## Source Of Truth

- Use `docs/API.md` for REST behavior.
- Use `docs/CLI.md` for command-line behavior.
- Use package tests as executable behavior documentation.
- Keep `README.md` concise; do not use it as a changelog or design dump.
- When behavior changes, update the affected docs and tests in the same change.

## Package Ownership

- `cmd/mihomo-st`: process startup and shutdown.
- `internal/cli`: CLI parsing and listen address normalization.
- `internal/app`: runtime orchestration and cross-package workflows.
- `internal/config`: runtime config model, validation, API patch behavior, and
  API field mapping.
- `internal/digest`: project-wide proxy identity digest algorithm and
  terminology.
- `internal/httpclient`: shared HTTP client/request construction, default
  headers, redirect behavior, and case-insensitive header merging.
- `internal/proxyconfig`: proxy config sources, YAML parsing, expansion, and
  mihomo proxy construction.
- `internal/store`: in-memory proxy inventory.
- `internal/tester`: delay and download execution.
- `internal/server`: routing, handlers, request decoding, response envelopes.
- `internal/version`: version metadata.

Put new behavior in the package that owns it. Avoid cross-package shortcuts that
blur these boundaries.

## Engineering Style

- Keep `go.mod` compatible with Go 1.20.
- Before adding or upgrading dependencies, check their `go` directive and prefer
  mature Go 1.20-compatible libraries over custom code.
- Prefer the existing stack and patterns already in the repository.
- Do not add broad `common`, `utils`, or helper packages.
- Add interfaces only at consumer boundaries, and only when they remove real
  coupling or enable useful tests.
- Keep public API names kebab-case.
- Keep config and request decoding type-strict; do not add compatibility
  coercion such as string-to-number parsing.
- Keep proxy identity digest-based. Do not add name-based proxy APIs.
- Do not mutate raw proxy mappings while digesting or parsing.
- Preserve best-effort proxy config loading semantics unless explicitly asked to change them.
- Do not document behavior that is not implemented.

## Workflow

1. Read this file and inspect `git status --short` before editing.
2. Preserve user work in progress. Do not revert unrelated changes.
3. Make small, behavior-focused changes that follow nearby code style.
4. Add or update focused tests for changed behavior.
5. Before handoff, format Go code under `cmd` and `internal`, run the full
   test suite, run `go vet` for all packages, build a release-style local
   binary for the current platform, and verify `--version` prints only the
   version value.
6. If verification requires starting the built executable as a long-running
   server, start it in the background using the native mechanism for the
   current platform, capture logs, and stop the process before handoff. Commands
   such as `--version` that exit immediately may run in the foreground.

## Release Constraints

- Build release artifacts on native GitHub-hosted runners; do not cross-compile.
- Use fixed runner labels, not `latest`.
- Release target matrix is Windows, Linux, and Darwin on amd64 and arm64.
- Keep release artifact naming consistent with the existing workflow.
