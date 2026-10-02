import {
  ArrowRight,
  Check,
  CircleHelp,
  Command,
  KeyRound,
  Link2,
  Plus,
  Radio,
  Route,
} from "lucide-react";
import type { Credential, RoutingStrategy } from "./api";
import {
  CopyButton,
  credentialState,
  EmptyState,
  ProviderMark,
  providerName,
  SectionHeading,
  Status,
} from "./ui";

export const routingNames: Record<RoutingStrategy, string> = {
  "round-robin": "Share the work",
  "weighted-round-robin": "Use account weights",
  "fill-first": "One account at a time",
  "subscription-first": "Use subscription order",
};

type Props = {
  credentials: Credential[];
  strategy: RoutingStrategy;
  connected: boolean;
  onConnect: () => void;
  onAdd: () => void;
  onAccounts: () => void;
  onSettings: () => void;
};

export function Overview({
  credentials,
  strategy,
  connected,
  onConnect,
  onAdd,
  onAccounts,
  onSettings,
}: Props) {
  const available = credentials.filter(
    (item) => credentialState(item).tone === "green",
  ).length;
  const groups = [...new Set(credentials.map((item) => item.provider))];
  const successes = credentials.reduce(
    (sum, item) => sum + (item.success ?? 0),
    0,
  );
  const failures = credentials.reduce(
    (sum, item) => sum + (item.failed ?? 0),
    0,
  );
  const attention = credentials.filter(
    (item) => credentialState(item).tone === "amber",
  );
  return (
    <>
      {connected ? (
        <div className="metrics" aria-label="Gateway summary">
          <div>
            <span>Available accounts</span>
            <strong>
              {available}
              <small> / {credentials.length}</small>
            </strong>
            <p>
              {credentials.length
                ? "Ready for new requests"
                : "Connect your first provider"}
            </p>
          </div>
          <div>
            <span>Connected providers</span>
            <strong>{groups.length.toString().padStart(2, "0")}</strong>
            <p>Across your accounts</p>
          </div>
          <div>
            <span>Account requests</span>
            <strong>{(successes + failures).toLocaleString()}</strong>
            <p>
              {failures > 0
                ? `${failures.toLocaleString()} failed · current counters`
                : "Current account counters"}
            </p>
          </div>
        </div>
      ) : (
        <div className="welcome-panel">
          <div className="welcome-copy">
            <span className="welcome-symbol">
              <Command size={30} strokeWidth={1.6} />
            </span>
            <h2>One home for your AI.</h2>
            <p>
              Bring your provider accounts together.
              <br />
              Give every app one simple connection.
            </p>
            <button className="button primary" onClick={onConnect}>
              <KeyRound size={16} />
              Connect gateway
              <ArrowRight size={16} />
            </button>
          </div>
          <div
            className="connection-map"
            aria-label="OpenAI, Claude, Antigravity and Grok connect through Relay"
          >
            <div className="map-providers">
              {["codex", "claude", "antigravity", "xai"].map((id) => (
                <ProviderMark key={id} provider={id} />
              ))}
            </div>
            <div className="map-lines">
              <i />
              <i />
              <i />
              <i />
            </div>
            <div className="map-relay">
              <Command size={24} />
              <span>Relay</span>
              <span className="map-dot" />
            </div>
            <div className="map-stem" />
            <div className="map-app">
              <span>Your apps</span>
              <Link2 size={17} />
            </div>
          </div>
        </div>
      )}
      <div className="overview-columns">
        <section className="panel provider-panel">
          <SectionHeading
            title="Provider connections"
            subtitle="Your accounts, working together."
            action={
              connected && (
                <button className="text-button" onClick={onAccounts}>
                  Manage
                  <ArrowRight size={15} />
                </button>
              )
            }
          />
          {groups.length ? (
            <div className="provider-list">
              {groups.map((provider) => {
                const items = credentials.filter(
                  (item) => item.provider === provider,
                );
                const ready = items.filter(
                  (item) => credentialState(item).tone === "green",
                ).length;
                return (
                  <button
                    className="provider-row"
                    key={provider}
                    onClick={onAccounts}
                  >
                    <ProviderMark provider={provider} />
                    <span className="row-title">
                      <strong>{providerName(provider)}</strong>
                      <small>
                        {items.length}{" "}
                        {items.length === 1 ? "account" : "accounts"}
                      </small>
                    </span>
                    <Status
                      label={ready ? `${ready} available` : "Not available"}
                      tone={ready ? "green" : "muted"}
                    />
                    <ArrowRight className="row-arrow" size={16} />
                  </button>
                );
              })}
            </div>
          ) : (
            <EmptyState
              icon={<Radio size={26} />}
              title={
                connected
                  ? "Your first connection starts here"
                  : "Make room for your providers"
              }
              action={
                <button
                  className="button secondary"
                  onClick={connected ? onAdd : onConnect}
                >
                  <Plus size={16} />
                  {connected ? "Connect an account" : "Connect gateway"}
                </button>
              }
            >
              Connect an account to make its models available to your apps.
            </EmptyState>
          )}
          {groups.length > 0 && (
            <button className="add-provider" onClick={onAdd}>
              <Plus size={17} />
              Connect another account
            </button>
          )}
        </section>
        <aside className="overview-aside">
          <section className="endpoint-panel">
            <span className="panel-symbol">
              <Link2 size={20} />
            </span>
            <h2>
              One endpoint.
              <br />
              All your models.
            </h2>
            <p>Use this base URL in any OpenAI-compatible app.</p>
            <div className="endpoint-code">
              <code>{window.location.origin}/v1</code>
              <CopyButton
                value={`${window.location.origin}/v1`}
                compact
                label="Copy API base URL"
              />
            </div>
            <div className="small-note">
              <KeyRound size={14} />
              <span>
                Apps use a client API key, separate from your management key.
              </span>
            </div>
          </section>
          <button className="routing-summary" onClick={onSettings}>
            <span className="routing-icon">
              <Route size={20} />
            </span>
            <span>
              <small>Account routing</small>
              <strong>
                {connected ? routingNames[strategy] : "Set up after connecting"}
              </strong>
            </span>
            <ArrowRight size={17} />
          </button>
        </aside>
      </div>
      <div className={`notice ${attention.length ? "attention-notice" : ""}`}>
        <span>
          {attention.length ? (
            <CircleHelp size={18} />
          ) : connected ? (
            <Check size={18} />
          ) : (
            <CircleHelp size={18} />
          )}
        </span>
        <p>
          {attention.length
            ? `${attention.length} ${attention.length === 1 ? "account needs" : "accounts need"} attention. Open Accounts to see what happened.`
            : connected
              ? "Account availability reflects the gateway's current state. Provider quotas and model access still apply."
              : "Your management key stays in this tab's memory. No account details are loaded until you connect."}
        </p>
        {attention.length > 0 && (
          <button className="text-button" onClick={onAccounts}>
            View accounts
            <ArrowRight size={14} />
          </button>
        )}
      </div>
    </>
  );
}
