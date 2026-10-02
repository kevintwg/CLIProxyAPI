import { useEffect, useState } from "react";
import type { Credential, ManagementApi, RoutingStrategy } from "./api";
import { ProviderMark, providerName } from "./ui";
import { durationSeconds, integer } from "./routing";

function localDate(value?: string): string {
  if (!value || !Number.isFinite(Date.parse(value))) return "";
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}
function draftFor(account: Credential) {
  return {
    priority: String(account.priority ?? 0),
    weight: String(account.weight ?? 1),
    tier:
      account.routing_tier === undefined ? "" : String(account.routing_tier),
    reset: localDate(account.routing_weekly_reset_at),
  };
}
export function AccountRouting({
  account,
  api,
  strategy,
  observationAge,
  onRefresh,
  notify,
}: {
  account: Credential;
  api: ManagementApi;
  strategy: RoutingStrategy;
  observationAge: string;
  onRefresh: () => Promise<void>;
  notify: (text: string) => void;
}) {
  const [base, setBase] = useState(() => draftFor(account));
  const [draft, setDraft] = useState(base);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const dirty = JSON.stringify(base) !== JSON.stringify(draft);
  useEffect(() => {
    if (!dirty && !busy) {
      const next = draftFor(account);
      setBase(next);
      setDraft(next);
    }
  }, [account, dirty, busy]);
  const profile = account.routing_profile;
  const future =
    !!profile?.weekly_reset_at &&
    Date.parse(profile.weekly_reset_at) > Date.now();
  const fresh =
    profile?.reset_source === "manual" ||
    (!!profile?.observed_at &&
      Date.parse(profile.observed_at) <= Date.now() &&
      Date.now() - Date.parse(profile.observed_at) <=
        durationSeconds(observationAge) * 1000);
  async function save() {
    setError("");
    setBusy(true);
    try {
      const fields: Record<string, unknown> = {};
      if (draft.priority !== base.priority)
        fields.priority = integer(
          draft.priority.trim() === "" ? NaN : Number(draft.priority),
          -2147483648,
          2147483647,
          "Priority",
        );
      if (draft.weight !== base.weight)
        fields.weight = integer(
          draft.weight.trim() === "" ? NaN : Number(draft.weight),
          0,
          1000000,
          "Weight",
        );
      if (draft.tier !== base.tier)
        fields.routing_tier =
          draft.tier.trim() === ""
            ? null
            : integer(Number(draft.tier), 0, 1000, "Tier rank");
      if (draft.reset !== base.reset)
        fields.routing_weekly_reset_at = draft.reset
          ? new Date(draft.reset).toISOString()
          : null;
      const saved = await api.setCredentialFields(account.name, fields);
      await onRefresh();
      const next = draftFor(saved);
      setBase(next);
      setDraft(next);
      notify("Account routing saved");
    } catch (reason) {
      setError(
        `${reason instanceof Error ? reason.message : "Could not confirm account routing saved."} Your draft is retained.`,
      );
    } finally {
      setBusy(false);
    }
  }
  function field(
    key: keyof typeof draft,
    label: string,
    type = "number",
    min?: number,
    max?: number,
  ) {
    return (
      <label>
        {label}
        <input
          type={type}
          min={min}
          max={max}
          step={type === "number" ? 1 : undefined}
          value={draft[key]}
          disabled={busy || account.runtime_only}
          onChange={(event) =>
            setDraft({ ...draft, [key]: event.target.value })
          }
        />
      </label>
    );
  }
  return (
    <section
      className="account-routing"
      aria-label={`${account.label || account.email || account.name} routing`}
    >
      <div className="account-routing-heading">
        <ProviderMark provider={account.provider} small />
        <div>
          <h3>{account.label || account.email || account.name}</h3>
          <span className="routing-account-provider">
            {account.provider === "codex"
              ? "Codex"
              : providerName(account.provider)}
          </span>
        </div>
        {dirty && <span className="account-draft-status">Unsaved</span>}
      </div>
      {account.runtime_only && (
        <p>
          This runtime-only account cannot be edited here. Manage its routing in
          the source configuration.
        </p>
      )}
      {strategy === "subscription-first" ? (
        <>
          <p>
            {profile?.tier === undefined
              ? "Tier rank unknown. Used last."
              : `Effective tier rank: ${profile.tier}.`}{" "}
            {profile?.tier_source === "manual"
              ? "Saved manual override."
              : profile?.tier_source === "plan"
                ? `Detected plan${profile.plan ? `: ${profile.plan}` : ""}.`
                : "No tier information available."}
          </p>
          <p>
            {profile?.reset_source === "manual"
              ? "Saved manual reset"
              : "Provider reset observation"}
            :{" "}
            {profile?.weekly_reset_at
              ? new Date(profile.weekly_reset_at).toLocaleString()
              : "unknown"}
            .{" "}
            {!future
              ? "No usable future reset. Ignored for ordering."
              : !fresh
                ? "Observation is stale and ignored."
                : "Available for ordering."}
            {profile?.observed_at &&
              ` Observed ${new Date(profile.observed_at).toLocaleString()}.`}
          </p>
          <div className="routing-fields">
            {field(
              "tier",
              account.provider === "codex"
                ? "Manual tier rank (blank uses detected plan)"
                : "Manual tier rank (blank means unknown)",
              "number",
              0,
              1000,
            )}
            {field(
              "reset",
              "Manual weekly reset (blank uses provider observation)",
              "datetime-local",
            )}
          </div>
        </>
      ) : (
        <div className="routing-fields">
          {field(
            "priority",
            "Priority (higher goes first)",
            "number",
            -2147483648,
            2147483647,
          )}
          {strategy === "weighted-round-robin" &&
            field(
              "weight",
              "Weight (zero skips this account)",
              "number",
              0,
              1000000,
            )}
        </div>
      )}
      {error && (
        <p role="alert" className="inline-error">
          {error}
        </p>
      )}
      <button
        className="button secondary"
        disabled={!dirty || busy || account.runtime_only}
        onClick={() => void save()}
      >
        {busy ? "Saving…" : "Save account"}
      </button>
    </section>
  );
}
