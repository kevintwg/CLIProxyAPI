import { useEffect, useMemo, useState } from "react";
import { Activity, CircleHelp, RefreshCw, RotateCcw } from "lucide-react";
import type {
  Credential,
  ManagementApi,
  UsageSnapshot,
  UsageWindow,
} from "./api";
import {
  Dialog,
  EmptyState,
  ProviderMark,
  providerName,
  SectionHeading,
} from "./ui";

type UsageState = Record<string, UsageSnapshot>;

function displayName(account: Credential) {
  return account.label || account.email || account.name;
}

function initialSnapshot(account: Credential): UsageSnapshot {
  const profile = account.routing_profile;
  return {
    ...account.usage_limits,
    ...(profile?.banked_reset_count === undefined ||
    account.usage_limits?.banked_reset_count !== undefined
      ? {}
      : { banked_reset_count: profile.banked_reset_count }),
    ...(profile?.banked_reset_expires_at === undefined ||
    account.usage_limits?.banked_reset_expires_at !== undefined
      ? {}
      : { banked_reset_expires_at: profile.banked_reset_expires_at }),
    ...(profile?.banked_reset_observed_at === undefined ||
    account.usage_limits?.banked_reset_observed_at !== undefined
      ? {}
      : { banked_reset_observed_at: profile.banked_reset_observed_at }),
  };
}

function usageLabel(window: UsageWindow) {
  if (window.window_minutes >= 10080) return "Weekly limit";
  if (window.window_minutes >= 1440) return "Daily limit";
  if (window.window_minutes >= 60)
    return `${Math.round(window.window_minutes / 60)}h limit`;
  return `${window.window_minutes}m limit`;
}

function Meter({ label, window }: { label?: string; window: UsageWindow }) {
  const used = Math.max(0, Math.min(100, window.used_percent));
  return (
    <div className="usage-meter">
      <div className="usage-meter-heading">
        <span>{label || usageLabel(window)}</span>
        <strong>{Math.round(used)}% used</strong>
      </div>
      <div
        className="usage-meter-track"
        role="meter"
        aria-label={`${label || usageLabel(window)} usage`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={used}
      >
        <span style={{ width: `${used}%` }} />
      </div>
      <div className="usage-meter-foot">
        <span>
          {Math.round(Math.max(0, window.remaining_percent))}% remaining
        </span>
        <span>Resets {new Date(window.resets_at).toLocaleString()}</span>
      </div>
    </div>
  );
}

function GenericMeter({
  label,
  remaining,
}: {
  label: string;
  remaining: number;
}) {
  const remainingPercent = Math.max(0, Math.min(100, remaining * 100));
  const used = 100 - remainingPercent;
  return (
    <div className="usage-meter">
      <div className="usage-meter-heading">
        <span>{label}</span>
        <strong>{Math.round(used)}% used</strong>
      </div>
      <div
        className="usage-meter-track"
        role="meter"
        aria-label={`${label} usage`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={used}
      >
        <span style={{ width: `${used}%` }} />
      </div>
      <div className="usage-meter-foot">
        <span>{Math.round(remainingPercent)}% remaining</span>
      </div>
    </div>
  );
}

export function Usage({
  credentials,
  api,
  onRefresh,
  notify,
}: {
  credentials: Credential[];
  api: ManagementApi;
  onRefresh: () => Promise<void>;
  notify: (message: string) => void;
}) {
  const [snapshots, setSnapshots] = useState<UsageState>(() =>
    Object.fromEntries(
      credentials.map((account) => [account.id, initialSnapshot(account)]),
    ),
  );
  const [fetching, setFetching] = useState<string | null>(null);
  const [fetchError, setFetchError] = useState("");
  const [redeeming, setRedeeming] = useState<Credential | null>(null);
  const [redeemError, setRedeemError] = useState("");
  const [fetchAllBusy, setFetchAllBusy] = useState(false);

  useEffect(() => {
    setSnapshots((current) => {
      const next = { ...current };
      for (const account of credentials)
        next[account.id] = {
          ...current[account.id],
          ...initialSnapshot(account),
        };
      return next;
    });
  }, [credentials]);

  const usableAccounts = useMemo(
    () =>
      credentials.filter(
        (account) => !account.disabled && !account.runtime_only,
      ),
    [credentials],
  );

  async function fetchAccount(account: Credential) {
    setFetching(account.id);
    setFetchError("");
    try {
      const snapshot = await api.fetchUsage(account);
      setSnapshots((current) => ({ ...current, [account.id]: snapshot }));
      notify(`Usage updated for ${displayName(account)}`);
    } catch (reason) {
      setFetchError(
        reason instanceof Error
          ? reason.message
          : "Could not fetch usage limits.",
      );
    } finally {
      setFetching(null);
    }
  }

  async function fetchAll() {
    setFetchAllBusy(true);
    setFetchError("");
    const results = await Promise.allSettled(
      usableAccounts.map((account) => api.fetchUsage(account)),
    );
    const next: UsageState = {};
    const errors: string[] = [];
    results.forEach((result, index) => {
      const account = usableAccounts[index]!;
      if (result.status === "fulfilled") next[account.id] = result.value;
      else
        errors.push(
          `${displayName(account)}: ${result.reason instanceof Error ? result.reason.message : "fetch failed"}`,
        );
    });
    if (Object.keys(next).length)
      setSnapshots((current) => ({ ...current, ...next }));
    if (errors.length) setFetchError(errors.join(" "));
    else notify("Usage limits updated");
    setFetchAllBusy(false);
  }

  async function redeem(account: Credential) {
    setFetching(account.id);
    setRedeemError("");
    try {
      const snapshot = await api.redeemReset(account);
      if (snapshot)
        setSnapshots((current) => ({ ...current, [account.id]: snapshot }));
      setRedeeming(null);
      notify(`Reset redeemed for ${displayName(account)}`);
      try {
        await onRefresh();
      } catch {
        setFetchError(
          "Reset redeemed, but the account list could not be refreshed.",
        );
      }
    } catch (reason) {
      setRedeemError(
        reason instanceof Error
          ? reason.message
          : "Could not redeem this reset.",
      );
    } finally {
      setFetching(null);
    }
  }

  return (
    <>
      <section className="usage-intro panel">
        <div>
          <SectionHeading
            title="Usage limits"
            subtitle="Fetch a fresh provider reading when you need to see the current allowance."
          />
          {fetchError && (
            <p className="inline-error" role="alert">
              {fetchError}
            </p>
          )}
        </div>
        <button
          className="button secondary"
          onClick={() => void fetchAll()}
          disabled={fetchAllBusy || fetching !== null || !usableAccounts.length}
        >
          <RefreshCw size={15} className={fetchAllBusy ? "spinning" : ""} />
          {fetchAllBusy ? "Fetching…" : "Fetch usage"}
        </button>
      </section>
      {!credentials.length ? (
        <section className="panel">
          <EmptyState
            icon={<Activity size={26} />}
            title="No accounts to measure"
          >
            Connect an account to see its provider usage limits and reset
            credits.
          </EmptyState>
        </section>
      ) : (
        <div className="usage-grid">
          {credentials.map((account) => {
            const snapshot = snapshots[account.id] ?? {};
            const windows = [snapshot.primary, snapshot.secondary].filter(
              (window): window is UsageWindow => !!window,
            );
            const groups =
              snapshot.groups?.flatMap((group) =>
                group.buckets.map((bucket) => ({
                  label: `${group.display_name || "Usage"}${bucket.window ? ` · ${bucket.window}` : ""}`,
                  bucket,
                })),
              ) ?? [];
            const hasData =
              windows.length > 0 ||
              groups.length > 0 ||
              (snapshot.summary?.length ?? 0) > 0;
            const count = snapshot.banked_reset_count;
            const canRedeem = account.supports_reset === true;
            const hasRedeemableCredit =
              account.provider.toLowerCase() === "codex"
                ? count !== undefined && count > 0
                : true;
            return (
              <article className="usage-card panel" key={account.id}>
                <div className="usage-card-heading">
                  <ProviderMark provider={account.provider} />
                  <div>
                    <h2>{displayName(account)}</h2>
                    <p>
                      {providerName(account.provider)}
                      {(snapshot.plan ||
                        snapshot.subscription?.plan ||
                        snapshot.subscription?.tierName) &&
                        ` · ${
                          snapshot.plan ||
                          snapshot.subscription?.plan ||
                          snapshot.subscription?.tierName
                        }`}
                    </p>
                  </div>
                  <button
                    className="button icon-button"
                    aria-label={`Fetch usage for ${displayName(account)}`}
                    title="Fetch usage"
                    disabled={
                      fetchAllBusy ||
                      fetching !== null ||
                      account.disabled ||
                      account.runtime_only
                    }
                    onClick={() => void fetchAccount(account)}
                  >
                    <RefreshCw
                      size={16}
                      className={fetching === account.id ? "spinning" : ""}
                    />
                  </button>
                </div>
                {hasData ? (
                  <div className="usage-card-content">
                    {windows.map((window, index) => (
                      <Meter
                        key={`${account.id}-${index}`}
                        label={
                          index === 0 && account.provider === "codex"
                            ? "Primary limit"
                            : index === 1
                              ? "Secondary limit"
                              : undefined
                        }
                        window={window}
                      />
                    ))}
                    {groups.map(({ label, bucket }) => (
                      <GenericMeter
                        key={label}
                        label={label}
                        remaining={bucket.remaining_fraction}
                      />
                    ))}
                    {snapshot.summary?.map((metric) => (
                      <div className="usage-summary-row" key={metric.key}>
                        <span>{metric.label}</span>
                        <strong>
                          {metric.format === "currency"
                            ? `${metric.currency ?? ""} ${metric.value.toLocaleString()}`
                            : metric.value.toLocaleString()}{" "}
                          {metric.unit ?? ""}
                        </strong>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="usage-empty">
                    <CircleHelp size={17} />
                    <span>
                      No usage reading yet. Fetch this account to load the
                      provider meter.
                    </span>
                  </div>
                )}
                <div className="banked-reset">
                  <div>
                    <span>Banked resets</span>
                    <strong>
                      {count === undefined
                        ? "Unknown"
                        : count === 0
                          ? "None available"
                          : `${count} available`}
                    </strong>
                    {snapshot.banked_reset_expires_at && (
                      <small>
                        Earliest expiry{" "}
                        {new Date(
                          snapshot.banked_reset_expires_at,
                        ).toLocaleString()}
                      </small>
                    )}
                  </div>
                  <button
                    className="button secondary"
                    title={
                      canRedeem
                        ? "Redeem one available reset"
                        : "Reset redemption is not available for this account"
                    }
                    disabled={
                      !canRedeem ||
                      !hasRedeemableCredit ||
                      fetchAllBusy ||
                      fetching !== null ||
                      account.disabled ||
                      account.runtime_only
                    }
                    onClick={() => {
                      setRedeemError("");
                      setRedeeming(account);
                    }}
                  >
                    <RotateCcw size={15} /> Redeem reset
                  </button>
                </div>
              </article>
            );
          })}
        </div>
      )}
      {redeeming && (
        <Dialog
          title="Redeem a banked reset?"
          onClose={() => {
            if (fetching === null) setRedeeming(null);
          }}
        >
          <p className="dialog-intro">
            Redeem one available reset for{" "}
            <strong>{displayName(redeeming)}</strong>. This uses the provider
            credit immediately and cannot be undone.
          </p>
          {redeemError && (
            <p className="inline-error" role="alert">
              {redeemError}
            </p>
          )}
          <div className="dialog-actions">
            <button
              className="button secondary"
              autoFocus
              disabled={fetching !== null}
              onClick={() => setRedeeming(null)}
            >
              Cancel
            </button>
            <button
              className="button primary"
              disabled={fetching !== null}
              onClick={() => void redeem(redeeming)}
            >
              <RotateCcw size={15} />
              {fetching === redeeming.id ? "Redeeming…" : "Redeem reset"}
            </button>
          </div>
        </Dialog>
      )}
    </>
  );
}
