@AGENTS.md

## Custom dashboard
- Source: `dashboard/`; generated embedded assets: `internal/dashboard/web/`.
- Run `cd dashboard && npm ci && npm test && npm run format:check && npm run build` for dashboard changes.
- Commit rebuilt assets with source changes; the Go binary serves them at `/dashboard/`.
- Isolated visual proof: build the Go binary, then run `DASHBOARD_BINARY=/absolute/path/to/cli-proxy-api npm run proof` from `dashboard/`.
- The proof runner uses only generated data, private temporary storage and loopback provider endpoints. See `dashboard/README.md` for setup, cleanup and evidence boundaries. Never record real accounts.

