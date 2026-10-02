import { useEffect, useState } from "react";
import {
  ArrowDownWideNarrow,
  Check,
  CircleEqual,
  GitBranch,
  KeyRound,
  LogOut,
} from "lucide-react";
import type { ManagementApi, RoutingStrategy } from "./api";
import { CopyButton, SectionHeading } from "./ui";
import { routingNames } from "./Overview";

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
      "Share requests according to account weights already set in your configuration.",
  },
];

export function Settings({
  api,
  strategy,
  onRefresh,
  onDisconnect,
  notify,
}: {
  api: ManagementApi;
  strategy: RoutingStrategy;
  onRefresh: () => Promise<void>;
  onDisconnect: () => void;
  notify: (text: string) => void;
}) {
  const [selection, setSelection] = useState(strategy);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => setSelection(strategy), [strategy]);
  async function save() {
    setBusy(true);
    setError("");
    try {
      await api.setRouting(selection);
      await onRefresh();
      notify("Routing preference saved");
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : "Could not save routing. Refresh before trying again.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="settings-layout">
      <section className="panel settings-panel">
        <SectionHeading
          title="How requests find an account"
          subtitle="Choose how the gateway uses your available accounts."
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
                disabled={busy}
                onChange={() => setSelection(id)}
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
        {error && (
          <p role="alert" className="inline-error">
            {error}
          </p>
        )}
        <div className="settings-footer">
          <span>Applies to new account selections.</span>
          <button
            className="button primary"
            disabled={busy || selection === strategy}
            onClick={() => void save()}
          >
            {busy ? "Saving…" : "Save changes"}
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
              Held in memory for this tab. Reloading or disconnecting clears it.
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
  );
}
