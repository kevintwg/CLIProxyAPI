# Relay dashboard

Relay is this fork's custom management interface. Open `/dashboard/` on a running
CLIProxyAPI server and enter its management key. The original `/management.html`
panel remains available. Relay uses only `/v8/management` endpoints and follows the
server's existing remote management, disabled-panel and Home-mode policies.

## Included

- Account availability and current request counters.
- Provider sign-in, status polling, cancellation and manual callback submission.
- Pause and resume signed-in accounts.
- Searchable model library from connected, unpaused accounts.
- Persisted routing preferences and copyable client connection details.
- Desktop, mobile, light, dark and reduced-motion interfaces.

The management key stays in browser memory. Reloading or disconnecting clears it.
Relay does not install usage history storage or change provider quotas. Requests
handled are the sum of current account counters, not durable historical analytics.
Provider authentication still requires an eligible account. API-key providers can
be configured in the server configuration. This version manages file-backed
accounts returned by the credentials API; configuration-only API-key providers are
not included in its account inventory, counters or model library.

## Develop and build

Requires Node 22.12+ and the Go version declared by the repository.

```sh
cd dashboard
npm ci
npm run dev
```

Vite serves `/dashboard/` on port 5178 and proxies the v8 management API to
`http://127.0.0.1:8318`. For a normal server, use that port in its configuration.
The production dashboard and API share the server's origin.

```sh
npm test
npm run format:check
npm run build
cd ..
go build -o cli-proxy-api ./cmd/server
```

The build writes `internal/dashboard/web`, which is embedded in the Go binary.
Commit these generated assets with source changes so Go-only builds work. CI
rebuilds and checks that the committed bundle matches its source. Do not hand-edit
the generated files. Upstream's management panel updater cannot overwrite Relay.

## Isolated proof instance

Build the server, then from `dashboard/` run:

```sh
DASHBOARD_BINARY=/absolute/path/to/cli-proxy-api npm run proof
```

The script creates a private temporary configuration and credential directory,
uses generated file-backed Codex and Claude credentials, disables remote model
and panel updates, and blocks other outbound HTTP traffic with a closed loopback
proxy. Credentials have a distant expiry and no refresh token. It prints the
path to a private `access.json` containing the generated management key. No real
provider account or inference is exercised. `-- --empty` starts with no accounts.
The isolated backend listens on loopback port 8318; use the Vite development URL
for a remote preview. Do not start a real provider login during a proof recording.

Capture a fresh browser context. The `example.invalid` accounts are generated
fixture data. Account/routing writes reach the real Go management API;
provider sign-in completion is covered by deterministic frontend tests. Stop the
script with Ctrl+C to terminate the server and remove its disposable data. A failed
start preserves diagnostic files and reports their path. No credentials belong in
Git, screenshots, PR descriptions or shared logs.

To run the browser checks and capture safe proof, install Chromium once with
`npx playwright install chromium`, then set `DASHBOARD_PROOF_ACCESS` to the
runner's access file and `DASHBOARD_EVIDENCE_DIR` to a private output directory
and run `npm run test:browser`. The test requires the populated proof instance.
