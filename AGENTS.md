# Repository Guidelines

## Project Structure & Module Organization
The main application entrypoint is `cmd/server/`. Core implementation lives in `internal/`, organized by concern: `api/` (Gin HTTP server and modules), `runtime/executor/` (per-provider execution), `thinking/` (reasoning pipeline), `translator/` (protocol adapters), `registry/`, `store/`, `watcher/`, `tui/`, and others. Reusable SDK code is under `sdk/cliproxy/`. Cross-module integration tests reside in `test/`. Configuration templates and examples are in the repository root and `examples/`.

## Build, Test, and Development Commands
- `gofmt -w .` — Format all Go source files (run after every change).
- `go build -o cli-proxy-api ./cmd/server` — Build the production server binary.
- `go run ./cmd/server` — Start the development server.
- `go test ./...` — Execute the full test suite.
- `go test -v -run TestName ./path/to/pkg` — Run a specific test verbosely.
- `go build -o test-output ./cmd/server && rm test-output` — Verify the project compiles cleanly (required after edits).

Common runtime flags: `--config <path>`, `--tui`, `--standalone`, `--local-model`.

## Coding Style & Naming Conventions
Follow standard Go conventions and idioms. Always run `gofmt` before committing. Use clear, descriptive names for packages, types, and variables. Wrap errors with contextual information. Prefer returning errors over terminating with `log.Fatal*`.

## Testing Guidelines
Tests use Go's standard `testing` package. Place unit tests in `*_test.go` files next to the code they cover. Integration tests live in the `test/` directory. Run `go test ./...` (or targeted paths) to validate changes. Ensure all tests pass and the project compiles before submitting pull requests.

## Commit & Pull Request Guidelines
Use conventional commit messages matching project history (e.g., `feat(scope): description`, `fix(docker): description`). Pull requests must have clear descriptions, reference related issues when applicable, and include reproduction steps or logs for bug fixes. Screenshots or CLI output are encouraged for user-facing or behavioral changes.

## Architecture Notes
Key subsystems include the thinking pipeline in `internal/thinking/`, provider executors in `internal/runtime/executor/`, and protocol translators. Changes touching multiple subsystems should be kept minimal and focused.
