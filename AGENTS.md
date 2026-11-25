# Agent notes — ptb-cli

- The CLI is a thin client of the PTB member API `/api/v2/mcp/*` (same REST API the PTB MCP server
  uses). Never add analytics, pricing or entitlement logic here; the server decides.
- Data commands live in `cmd/data.go` (`specs`). Paths and query names must match the MCP server
  (`mcp/src/client.ts` in the backend repo). `cmd/data_test.go` guards them.
- Paid commands: quote (`GET /quote`) → confirm (or `--yes`) → call with `max_tokens` and an
  `Idempotency-Key` → report `billing` on stderr. Keep stdout clean for `--output json|csv|ndjson`.
- Never print or log tokens or API keys. Secrets live in the OS keychain (`internal/auth`).
- `go test ./...` must pass on Linux, macOS and Windows (see `.github/workflows/ci.yml`).
