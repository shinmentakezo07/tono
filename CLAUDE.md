# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CLIProxyAPI is a Go proxy server that exposes OpenAI / Gemini / Claude / Codex / Grok compatible API surfaces on top of CLI-backed models (Google Gemini CLI, OpenAI Codex, Claude Code, Antigravity, Grok, Kimi, xAI, AI Studio, Vertex). It also ships a reusable Go SDK at `sdk/cliproxy/` that external programs can embed to host the same routing/auth/watcher/translation pipeline without depending on the CLI binary.

The project also has a public REST "Management API" (see `MANAGEMENT_API.md` link in `README.md`) and an optional TUI mode (`--tui`, `--standalone`) that embeds the server and connects a bubbletea client to it.

## Build, Run, Test

Standard Go workflow. The server binary is produced from `./cmd/server`.

```bash
# Format
gofmt -w .

# Build (also serves as the CI compile check)
go build -o cli-proxy-api ./cmd/server
go build -o test-output ./cmd/server && rm test-output   # CI-style smoke build

# Run
go run ./cmd/server                                       # default: reads ./config.yaml
go run ./cmd/server --config /path/to/config.yaml         # explicit config
go run ./cmd/server --tui --standalone                    # embedded TUI + local server
go run ./cmd/server --codex-login                         # one-shot OAuth logins:
                                                         #   --login, --codex-login, --codex-device-login,
                                                         #   --claude-login, --antigravity-login,
                                                         #   --kimi-login, --xai-login
go run ./cmd/server --vertex-import key.json --vertex-import-prefix my-prefix

# Tests
go test ./...                                             # full suite
go test -v -run TestName ./internal/thinking              # single test / package
go test ./internal/runtime/executor -run Claude           # executor-specific

# SDK (embedded use) — examples live under examples/custom-provider
go build ./examples/...
```

Notable env vars picked up by `cmd/server/main.go` (in addition to flags):
`DEPLOY=cloud`, `HOME_JWT` (control-plane bootstrap), `PGSTORE_*` (Postgres-backed token store), `GITSTORE_*` (Git-backed token store), `OBJECTSTORE_*` (S3-compatible object store), `USAGE_HISTORY_POSTGRES_ENABLED` (TimescaleDB writer toggle).

## High-Level Architecture

The codebase splits into three concerns: **inbound HTTP/routing** (`internal/api`, `sdk/api`), **outbound provider execution** (`internal/runtime/executor`, `sdk/cliproxy/auth`, `sdk/cliproxy/executor`), and **protocol translation + reasoning shaping** (`internal/translator`, `internal/thinking`). A hot-reload **watcher** (`internal/watcher`) and a multi-backend **token store** (`internal/store`) keep auth + config in sync.

### Entry point and SDK boundary

- `cmd/server/main.go` is a thin wrapper. Its only job is: parse flags, choose a config + token-store backend, wire the SDK builder, then call `cmd.StartService` (or run a TUI).
- The actual service lives in `sdk/cliproxy/`. `Builder` (`sdk/cliproxy/builder.go`) is the public embedding surface: `WithConfig`, `WithConfigPath`, `WithServerOptions(...)`, `WithCoreAuthManager`, `WithHooks`. `Service.Run(ctx)` is the lifecycle.
- `internal/cmd` contains the CLI-only orchestration that the binary uses; the SDK re-implements the same wiring via `Builder` for embedders.

### HTTP server (`internal/api`, `sdk/api`)

- `internal/api/server.go` constructs the Gin engine, applies middleware, and registers the **protocol multiplexer** (`internal/api/protocol_multiplexer.go`). The same TCP listener serves both plain HTTP and a Redis-RESP bridge (`internal/api/redis_queue_protocol.go`, `internal/redisqueue`) so an external Redis client can enqueue usage records and stream management commands.
- Modules under `internal/api/modules/` (notably `amp/`) attach extra routes when their config is enabled — Amp CLI/IDE integration is the largest of these.
- `sdk/api/handlers/` holds the request/response handler logic for the three public API families: `claude/`, `gemini/`, `openai/`. `BaseAPIHandler` is the cross-protocol base class.
- `internal/api/middleware/` adds request logging (`request_logging.go`, `response_writer.go`) and usage metrics (`usage_metrics.go`).

### Provider executors (`internal/runtime/executor`)

Each upstream backend has its own executor implementing the `sdk/cliproxy/executor.ProviderExecutor` interface. They share helpers in `internal/runtime/executor/helps/`:

- `claude_executor.go` (+ `claude_signing.go`) — Anthropic messages API. Contains the OAuth tool-name fingerprint remap (`oauthToolRenameMap`) and request signing used to mimic Claude Code traffic.
- `codex_executor.go` + `codex_websockets_executor.go` — OpenAI Codex over both HTTPS and the Codex WebSocket transport; image gen lives in `codex_openai_images.go`.
- `gemini_executor.go`, `gemini_vertex_executor.go`, `gemini_cli_executor.go` — AI Studio, Vertex service-account, and Gemini-CLI OAuth paths.
- `antigravity_executor.go` — Antigravity provider.
- `xai_executor.go`, `kimi_executor.go`, `aistudio_executor.go`, `openai_compat_executor.go` — OpenAI-compatible upstreams and the remaining CLI backends.
- Heavy use of `gjson`/`sjson` for non-streaming JSON surgery; streaming paths use `bufio.Scanner` + SSE framing (see `claude_executor.go` for the canonical pattern).

### Translation layer (`internal/translator`)

Translators convert between client-visible protocol shapes (OpenAI Chat/Responses, Claude messages, Gemini generateContent) and the executor's native protocol. The directory layout is `internal/translator/{client}/{provider}/{...}` (e.g. `openai/claude`, `claude/gemini-cli`, `antigravity/openai/responses`). `internal/translator/init.go` is a blank-imports registry — every translator package is registered there for its `init()` side effects, so adding a new pair means adding a subpackage **and** an import line in `init.go`.

### Thinking / reasoning pipeline (`internal/thinking`)

Provider-agnostic reasoning-effort handling, used by all executors:

- `convert.go` — `ConvertLevelToBudget` (level→token) and `ConvertBudgetToLevel` (token→level) with per-level thresholds and `max` support for Claude adaptive thinking.
- `apply.go`, `strip.go`, `suffix.go` — inject/remove/suffix thinking content into outgoing requests and incoming responses.
- `validate.go`, `errors.go` — validation and typed errors.
- `provider/` subdir holds provider-specific shaping (e.g. Claude signature, Gemini thought parts, OpenAI `reasoning_effort`).

### Auth & token store (`sdk/cliproxy/auth`, `sdk/auth`, `internal/auth`, `internal/store`)

- `sdk/cliproxy/auth` contains the **runtime** `Manager` used for execution: it picks a credential, calls an executor, and refreshes tokens. Selectors live here too (`RoundRobinSelector`, `FillFirstSelector`, `SessionAffinitySelector`).
- `internal/auth/{claude,codex,gemini,antigravity,vertex,kimi,xai}` holds the per-provider auth JSON loaders/serializers.
- `internal/store` has three swappable token-store backends wired by `main.go` based on env: `FileTokenStore` (default), `PostgresStore`, `GitTokenStore`, `ObjectTokenStore` (S3-compatible). `sdkAuth.RegisterTokenStore(...)` in `main.go` is what makes the chosen store visible to the rest of the system.

### Watcher / hot reload (`internal/watcher`)

`Watcher` (`internal/watcher/watcher.go`) uses `fsnotify` to watch `config.yaml` and the auth directory. Edits are debounced, hashed, and dispatched via `internal/watcher/dispatcher.go` → `synthesizer` to atomically swap the in-memory `*config.Config` and the in-memory `coreauth.Manager` client set. CI builds a fresh `internal/registry/models/models.json` from the `router-for-me/models` repo before compiling — see `.github/workflows/pr-test-build.yml`.

### Usage history (`internal/usagehistory`)

Optional local persistence for usage records. Two writers can be active at once: a JSONL file writer (`store.go`, `compaction.go`, daily rotation) and a TimescaleDB/Postgres writer (`pgstore.go`, `writer.go`). The Postgres writer is the unbounded-queue variant; the recent commits (`1f64da46`, `bc342b17`) replaced a bounded channel that was silently dropping records.

### Management & misc

- `internal/api/handlers/management` — REST management API mounted under `/v0/management` when `remote-management.secret-key` is set.
- `internal/registry` — model catalog (read from `models.json`) plus a remote updater (`model_updater.go`) that pulls from the models repo.
- `internal/logging` — logrus setup, file rotation, request logger, log hook consumed by the TUI.
- `internal/redisqueue` — Redis-RESP bridge used by some external observability tooling.
- `internal/tui` — bubbletea-based admin client.
- `internal/buildinfo` — version/commit/builddate injected at link time.
- `internal/home` — optional "Home" control-plane bootstrap (`-home-jwt`).

## Testing Conventions

- Unit tests live next to the code in `*_test.go` files (e.g. `internal/runtime/executor/claude_executor_test.go`).
- Cross-module / integration tests live in `test/` — note `test/thinking_conversion_test.go` (the largest), `test/usage_logging_test.go`, `test/amp_management_test.go`, `test/builtin_tools_translation_test.go`, `test/claude_code_compatibility_sentinel_test.go`. The `test/testdata/` directory holds fixtures for these.
- `go test ./...` is the contract; targeted runs are common when iterating on a single executor or translator pair.

## Repository Policies

- `AGENTS.md` (symlinked from `CLAUDE.md`) is the canonical contributor guide — read it for commit-message conventions (`feat(scope): …`, `fix(docker): …`), PR requirements, and architecture notes.
- **`.github/workflows/agents-md-guard.yml` will auto-close any PR that modifies `AGENTS.md`** — do not propose changes to that file in a PR. If the guide itself is wrong, surface the issue in the PR description instead.
- **`.github/workflows/pr-path-guard.yml`** restricts which paths may be touched in a PR; check it before changing `Dockerfile`, release, or CI files.
- Conventional commits match the format in the existing log (`feat(usage): …`, `fix(usage): …`, etc.). PR descriptions should reference issues and include reproduction steps or logs for bug fixes.
- The PR test workflow refreshes `internal/registry/models/models.json` from the `router-for-me/models` repo before building — that regeneration is expected to happen on every PR build, do not commit model catalog churn manually.

## SDK Embedding

If a future task is to embed the proxy rather than edit the CLI, the public surface is `sdk/cliproxy` (see `docs/sdk-usage.md` for the worked example and `docs/sdk-advanced.md` for executors/translators). The `examples/custom-provider` directory is the smallest end-to-end embed sample. The `Builder` in `sdk/cliproxy/builder.go` is the single entry point — every embedding customization funnels through `WithServerOptions(...)` / `WithCoreAuthManager(...)` / `WithTokenClientProvider(...)` / `WithAPIKeyClientProvider(...)`.
