import { useEffect, useRef, useState } from "react";
import {
  ArrowDownWideNarrow,
  Check,
  CircleEqual,
  GitBranch,
  KeyRound,
  LogOut,
  Network,
} from "lucide-react";
import type { Credential, ManagementApi, RoutingStrategy } from "./api";
import { CopyButton, ProviderMark, SectionHeading } from "./ui";
import { routingNames } from "./Overview";
import { AccountRouting } from "./AccountRouting";
import { parseRouting, routingChanges, type RoutingSettings } from "./routing";

const emptyCredentials: Credential[] = [];
const choices = [
  {
    id: "round-robin" as const,
    icon: CircleEqual,
    detail:
      "Take turns across available accounts. A balanced default for everyday use.",
  },
  {
    id: "fill-first" as const,
    icon: ArrowDownWideNarrow,
    detail:
      "Prefer the first available account, then move to the next when needed.",
  },
  {
    id: "weighted-round-robin" as const,
    icon: GitBranch,
    detail:
      "Share requests by weight within the highest available priority group. Set weights in each provider’s account settings.",
  },
  {
    id: "subscription-first" as const,
    icon: ArrowDownWideNarrow,
    detail:
      "Use the lowest subscription tier first, then the earliest weekly reset. Keep conversations together with session affinity.",
  },
];

export function Settings({
  api,
  strategy,
  credentials = emptyCredentials,
  onRefresh,
  onDisconnect,
  notify,
}: {
  api: ManagementApi;
  strategy: RoutingStrategy;
  credentials?: Credential[];
  onRefresh: () => Promise<void>;
  onDisconnect: () => void;
  notify: (text: string) => void;
}) {
  const [base, setBase] = useState<RoutingSettings | null>(null);
  const [draft, setDraft] = useState(() => parseRouting({ strategy }));
  const selection = draft.strategy;
  const [activeTab, setActiveTab] = useState("relay");
  const tabRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  const groups = [
    {
      id: "codex",
      label: "Codex",
      accounts: credentials.filter((item) => item.provider === "codex"),
    },
    {
      id: "claude",
      label: "Claude",
      accounts: credentials.filter((item) => item.provider === "claude"),
    },
    {
      id: "other",
      label: "Other accounts",
      accounts: credentials.filter(
        (item) => !["codex", "claude"].includes(item.provider),
      ),
    },
  ];
  const tabs = [
    { id: "relay", label: "Relay", count: undefined },
    ...groups
      .filter((group) => group.id !== "other" || group.accounts.length > 0)
      .map((group) => ({
        id: group.id,
        label: group.label,
        count: group.accounts.length,
      })),
  ];
  const selectedTab = tabs.some((tab) => tab.id === activeTab)
    ? activeTab
    : "relay";
  function showRelay() {
    setActiveTab("relay");
    tabRefs.current.relay?.focus();
  }
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const dirty = base !== null && JSON.stringify(base) !== JSON.stringify(draft);
  const editsRef = useRef({ dirty, busy });
  editsRef.current = { dirty, busy };
  useEffect(() => {
    let active = true;
    api
      .routingSettings()
      .then((settings) => {
        if (active && !editsRef.current.dirty && !editsRef.current.busy) {
          setBase(settings);
          setDraft(settings);
        }
      })
      .catch((reason) => {
        if (active)
          setError(
            `${reason instanceof Error ? reason.message : "Could not load routing settings."} Refresh the gateway to try again.`,
          );
      });
    return () => {
      active = false;
    };
  }, [api, credentials]);
  function update(key: keyof RoutingSettings, value: unknown) {
    setDraft({ ...draft, [key]: value });
  }
  async function save() {
    if (!base) return;
    setBusy(true);
    setError("");
    try {
      const saved = await api.setRoutingSettings(routingChanges(base, draft));
      await onRefresh();
      setBase(saved);
      setDraft(saved);
      notify("Relay settings saved");
    } catch (reason) {
      setError(
        `${reason instanceof Error ? reason.message : "Could not confirm routing saved."} Your draft is retained.`,
      );
    } finally {
      setBusy(false);
    }
  }
  function duration(
    key: "session-affinity-ttl" | "subscription-first-max-observation-age",
    label: string,
  ) {
    return (
      <label>
        {label}
        <input
          value={draft[key]}
          disabled={!base || busy}
          onChange={(event) => update(key, event.target.value)}
          placeholder="30m or 1h"
        />
      </label>
    );
  }
  function toggle(
    key:
      | "session-affinity"
      | "session-affinity-subagents"
      | "subscription-first-prefer-weekly-reset",
    label: string,
  ) {
    return (
      <label className="routing-toggle">
        <input
          type="checkbox"
          checked={draft[key]}
          disabled={!base || busy}
          onChange={(event) => update(key, event.target.checked)}
        />
        {label}
      </label>
    );
  }
  return (
    <div className="settings-layout">
      <div className="settings-intro">
        <h2>Settings</h2>
        <p>
          Configure Relay once. Fine-tune each provider’s accounts separately.
        </p>
      </div>
      <div
        className="settings-tabs"
        role="tablist"
        aria-label="Settings sections"
      >
        {tabs.map((tab, index) => (
          <button
            key={tab.id}
            id={`settings-tab-${tab.id}`}
            role="tab"
            ref={(node) => {
              tabRefs.current[tab.id] = node;
            }}
            aria-controls={`settings-panel-${tab.id}`}
            aria-selected={selectedTab === tab.id}
            aria-label={
              tab.count === undefined
                ? tab.label
                : `${tab.label}, ${tab.count} accounts`
            }
            tabIndex={selectedTab === tab.id ? 0 : -1}
            onClick={() => setActiveTab(tab.id)}
            onKeyDown={(event) => {
              let next: number;
              if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
              else if (event.key === "ArrowLeft")
                next = (index - 1 + tabs.length) % tabs.length;
              else if (event.key === "Home") next = 0;
              else if (event.key === "End") next = tabs.length - 1;
              else return;
              event.preventDefault();
              setActiveTab(tabs[next]!.id);
              tabRefs.current[tabs[next]!.id]?.focus();
            }}
          >
            {tab.id === "relay" ? (
              <Network size={17} aria-hidden="true" />
            ) : tab.id !== "other" ? (
              <ProviderMark provider={tab.id} small />
            ) : null}
            {tab.label}
            {tab.count !== undefined && (
              <span className="settings-tab-count" aria-hidden="true">
                {tab.count}
              </span>
            )}
          </button>
        ))}
      </div>
      <div
        id="settings-panel-relay"
        role="tabpanel"
        aria-labelledby="settings-tab-relay"
        hidden={selectedTab !== "relay"}
        className="settings-tab-panel"
        tabIndex={0}
      >
        <section className="panel settings-panel">
          <SectionHeading
            title="Account selection"
            subtitle="One routing strategy applies across all providers. Accounts are selected within the requested provider."
          />
          <fieldset className="routing-choices">
            <legend className="sr-only">Routing strategy</legend>
            {choices.map(({ id, icon: Icon, detail }) => (
              <label
                className={`routing-choice ${selection === id ? "selected" : ""}`}
                key={id}
              >
                <input
                  type="radio"
                  name="routing"
                  value={id}
                  checked={selection === id}
                  disabled={!base || busy}
                  onChange={() => update("strategy", id)}
                />
                <span className="choice-icon">
                  <Icon size={22} />
                </span>
                <span>
                  <strong>{routingNames[id]}</strong>
                  <small>{detail}</small>
                </span>
                <span className="radio-visual">
                  {selection === id && <Check size={12} />}
                </span>
              </label>
            ))}
          </fieldset>
          <div className="routing-options">
            <p>
              {selection === "round-robin"
                ? "Higher priority accounts are used first. Accounts with equal priority take turns."
                : selection === "fill-first"
                  ? "Higher priority accounts are used first. The first available account handles requests until unavailable."
                  : selection === "weighted-round-robin"
                    ? "Higher priority accounts are used first. Equal priority accounts share requests by weight; zero weight skips an account."
                    : "Lower tier ranks go first. Codex plans can be detected; set Claude ranks manually. Unknown ranks go last. Priority only breaks otherwise equal choices."}
            </p>
            {selection === "subscription-first" && (
              <>
                {toggle(
                  "subscription-first-prefer-weekly-reset",
                  "Prefer the earliest weekly reset within a tier",
                )}
                <div className="routing-fields">
                  {duration(
                    "subscription-first-max-observation-age",
                    "Maximum observation age",
                  )}
                </div>
                <p>
                  Reset observations come from provider request traffic. Stale
                  or past resets are ignored. There is no quota polling.
                </p>
              </>
            )}
          </div>
          <div className="routing-options settings-group">
            <h3>Conversation continuity</h3>
            <p>
              Keep related requests together while an account remains available.
            </p>
            {toggle(
              "session-affinity",
              "Keep a conversation on the same account",
            )}
            {draft["session-affinity"] && (
              <>
                <div className="routing-fields">
                  {duration(
                    "session-affinity-ttl",
                    "Conversation affinity lifetime",
                  )}
                </div>
                {toggle(
                  "session-affinity-subagents",
                  "Keep subagents with their parent conversation",
                )}
                <p>
                  Affinity helps preserve provider caches while the pinned
                  account remains available. Saving routing settings can reset
                  existing assignments.
                </p>
              </>
            )}
          </div>
          <div className="routing-options settings-group">
            <details>
              <summary>Retry options</summary>
              <p>
                Retries use the next available credential and respect cooldowns.
                Zero credential limit allows all available accounts.
              </p>
              <div className="routing-fields">
                {(
                  [
                    ["request-retry", "Retry rounds"],
                    ["max-retry-credentials", "Credential limit (0 uses all)"],
                    ["max-retry-interval", "Maximum retry wait (seconds)"],
                  ] as const
                ).map(([key, label]) => (
                  <label key={key}>
                    {label}
                    <input
                      type="number"
                      min="0"
                      max="2147483647"
                      step="1"
                      disabled={!base || busy}
                      value={
                        Number.isNaN(draft.retry[key]) ? "" : draft.retry[key]
                      }
                      onChange={(event) =>
                        update("retry", {
                          ...draft.retry,
                          [key]:
                            event.target.value === ""
                              ? NaN
                              : Number(event.target.value),
                        })
                      }
                    />
                  </label>
                ))}
              </div>
            </details>
          </div>
          {error && (
            <p role="alert" className="inline-error">
              {error}
            </p>
          )}
          <div className="settings-footer">
            <span>
              {dirty ? "Unsaved Relay changes" : "Applies across all providers"}
              . Account edits save separately.
            </span>
            <button
              className="button primary"
              disabled={!base || busy || !dirty}
              onClick={() => void save()}
            >
              {busy ? "Saving…" : "Save Relay settings"}
            </button>
          </div>
        </section>
        <section className="panel settings-panel">
          <SectionHeading
            title="Your connection"
            subtitle="This browser is connected to the gateway below."
          />
          <div className="setting-row">
            <div>
              <strong>API base URL</strong>
              <code>{window.location.origin}/v1</code>
            </div>
            <CopyButton value={`${window.location.origin}/v1`} />
          </div>
          <div className="setting-row">
            <div>
              <strong>Management key</strong>
              <p>
                Held in memory for this tab. Reloading or disconnecting clears
                it.
              </p>
            </div>
            <KeyRound size={19} />
          </div>
          <div className="settings-footer">
            <span>Disconnecting leaves the gateway running.</span>
            <button className="button secondary" onClick={onDisconnect}>
              <LogOut size={15} />
              Disconnect
            </button>
          </div>
        </section>
      </div>
      {groups
        .filter((group) => group.id !== "other" || group.accounts.length > 0)
        .map((group) => (
          <div
            key={group.id}
            id={`settings-panel-${group.id}`}
            role="tabpanel"
            aria-labelledby={`settings-tab-${group.id}`}
            hidden={selectedTab !== group.id}
            className="settings-tab-panel"
            tabIndex={0}
          >
            <section className="panel settings-panel">
              <SectionHeading
                title={`${group.label} accounts`}
                subtitle="Each account saves independently. These controls do not change your shared Relay settings."
              />
              <div className="routing-options provider-settings-context">
                <p>
                  Account selection: <strong>{routingNames[selection]}</strong>
                  {base && selection !== base.strategy
                    ? " (unsaved strategy)"
                    : ""}
                  .{" "}
                  <button className="settings-text-button" onClick={showRelay}>
                    Edit Relay settings
                  </button>
                </p>
                {selection === "subscription-first" && (
                  <>
                    <p>
                      {group.id === "codex"
                        ? "Automatic Codex ranks: Free 0, Go 1, Plus 2, Pro 3. Leave the manual rank blank to use the detected plan, or enter a rank to override it."
                        : group.id === "claude"
                          ? "Claude needs a manual tier rank. A blank rank is unknown and used last."
                          : "Set a manual tier rank when no plan is detected. Unknown ranks are used last."}
                    </p>
                    <p>
                      Lower ranks are preferred within each provider. With
                      weekly reset preference enabled, equal ranks use the
                      earliest usable reset. Provider observations come from
                      request traffic; stale or past resets are ignored.
                    </p>
                  </>
                )}
              </div>
              <div className="account-routing-list">
                {group.accounts.length === 0 ? (
                  <p className="settings-empty">
                    No {group.label} accounts connected. Add an account from
                    Accounts to configure it here.
                  </p>
                ) : (
                  group.accounts.map((account) => (
                    <AccountRouting
                      key={account.name}
                      account={account}
                      api={api}
                      strategy={selection}
                      observationAge={
                        base?.["subscription-first-max-observation-age"] ??
                        "30m"
                      }
                      onRefresh={onRefresh}
                      notify={notify}
                    />
                  ))
                )}
              </div>
            </section>
          </div>
        ))}
    </div>
  );
}
