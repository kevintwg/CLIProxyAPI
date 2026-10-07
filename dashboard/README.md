# Relay dashboard

Relay is this fork's custom management interface. Open `/dashboard/` on a running
CLIProxyAPI server and enter its management key. The original `/management.html`
panel remains available. Relay uses only `/v8/management` endpoints and follows the
server's existing remote management, disabled-panel and Home-mode policies.

## Included

- Account availability and current request counters.
- Provider sign-in, status polling, cancellation and manual callback submission.
- Pause, resume and remove signed-in accounts. Removal deletes the saved connection; reconnecting requires signing in again.
- Searchable model library from connected, unpaused accounts.
- Persisted routing preferences and copyable client connection details.
- Optional remembered sign-in and dashboard password changes.
- Desktop, mobile, light, dark and reduced-motion interfaces.

By default the management key stays in browser memory, and reloading or
disconnecting clears it. Check "Remember me on this device" when signing in to
save the key in this browser's local storage, so Relay signs in automatically on
the next visit. Disconnecting removes the saved key, and a saved key the server
no longer accepts is removed and the sign-in form is shown. Only use this on a
device you trust. If the browser blocks storage, Relay keeps the key in memory.

Settings can change the dashboard password (the `secret-key` management key).
It needs the current password and a new one of at least 8 characters. The
config file stores only the new bcrypt hash, this browser stays signed in with
the new password, and a separate `MANAGEMENT_PASSWORD` environment secret keeps
working.
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

## Routing customization

Settings separates shared Relay policy from Codex and Claude account settings.
The provider tabs show account counts; Other accounts appears when another provider
is connected. Arrow keys, Home and End move between tabs. Switching tabs and
refreshing accounts retain unsaved drafts and errors. Save Relay settings changes
shared policy; Save account changes only that account. Runtime-only accounts are
shown with disabled fields because they cannot be edited through this API.

Relay offers rotation, fill-first, weighted rotation and subscription order.
The first three prefer higher priority accounts. Weighted rotation divides work
within the highest available priority group; a zero weight excludes the account.
Subscription order uses lower tier ranks first, then the earliest usable weekly
reset when weekly reset preference is enabled. Codex uses the earliest usable
banked reset expiry to break remaining ties, or as the first reset criterion when
weekly reset preference is disabled. Banked resets are never redeemed automatically.
Codex accounts stop routing at 5% remaining quota, even with purchased usage
credits. Existing conversations switch on their next request when an eligible
account in the same tier has an earlier usable weekly reset and weekly preference
is enabled. They also switch at the quota cutoff or unavailability, or return to
a lower tier when that option is enabled. Banked expiry and priority changes
alone do not move existing conversations. Codex plan ranks are detected (free 0, go 1, plus 2, pro 3); Claude ranks
can be entered manually. Unknown ranks are used last.

Codex quota and banked-reset observations refresh on startup and every minute.
Weekly resets also come from provider request traffic. Past resets and ranking
observations older than the configured maximum age are ignored. A known exhausted
allowance remains blocked until fresh usage confirms recovery; a passed reset
time alone does not reopen it. Failed probes preserve the last known state. Provider reads follow the server
transport policy without response deadlines; shutdown cancels outstanding probes. An account can have a future manual reset override;
clearing its reset returns to provider observations. Clearing a Codex rank uses
the detected plan; Claude requires a manual rank, so a blank rank is unknown.

Conversation affinity helps preserve provider caches while an assigned account
is available. Routing settings changes may reset existing assignments. Account
field edits are saved separately from global settings. Each save patches only
changed fields and reads back persisted values before confirming success.
Refreshing account data preserves unsaved routing drafts. API-key accounts
configured only in YAML are outside this dashboard's account controls.
