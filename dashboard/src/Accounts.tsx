import { useState } from "react";
import { Pause, Play, Plus, Search, Trash2, Users } from "lucide-react";
import type { Credential, ManagementApi } from "./api";
import {
  credentialState,
  EmptyState,
  Dialog,
  ProviderMark,
  providerName,
  Status,
} from "./ui";

export function Accounts({
  credentials,
  api,
  onAdd,
  onRefresh,
  onRemoved,
  notify,
}: {
  credentials: Credential[];
  api: ManagementApi;
  onAdd: () => void;
  onRefresh: () => Promise<void>;
  onRemoved?: (name: string) => void;
  notify: (message: string) => void;
}) {
  const [filter, setFilter] = useState("All accounts");
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [removing, setRemoving] = useState<Credential | null>(null);
  const [removeError, setRemoveError] = useState("");
  const filtered = credentials.filter((item) => {
    const matchesQuery =
      `${providerName(item.provider)} ${item.label ?? ""} ${item.email ?? ""} ${item.name}`
        .toLowerCase()
        .includes(query.toLowerCase());
    return (
      matchesQuery &&
      (filter === "All accounts" ||
        (filter === "Available" && credentialState(item).tone === "green") ||
        (filter === "Paused" && item.disabled))
    );
  });
  async function toggle(item: Credential) {
    setBusy(item.name);
    setError("");
    try {
      await api.setCredentialEnabled(item, item.disabled);
      await onRefresh();
      notify(item.disabled ? "Account resumed" : "Account paused");
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : "Could not update this account. Refresh before trying again.",
      );
    } finally {
      setBusy(null);
    }
  }
  async function remove(item: Credential) {
    if (busy !== null) return;
    setBusy(item.name);
    setRemoveError("");
    setError("");
    try {
      if (
        item.runtime_only ||
        credentials.some((other) => other.id === item.id && other.runtime_only)
      ) {
        throw new Error("This account has no saved connection to remove.");
      }
      if (
        credentials.some(
          (other) => other.id !== item.id && other.name === item.name,
        )
      ) {
        throw new Error(
          "This connection contains multiple accounts and cannot be removed separately.",
        );
      }
      await api.removeCredential(item);
    } catch (reason) {
      setRemoveError(
        reason instanceof Error
          ? reason.message
          : "Could not remove this account. Refresh before trying again.",
      );
      setBusy(null);
      return;
    }
    onRemoved?.(item.name);
    setRemoving(null);
    notify("Account removed");
    try {
      await onRefresh();
    } catch {
      setError(
        "Account removed, but the gateway could not refresh. Refresh to update the list and models.",
      );
    } finally {
      setBusy(null);
    }
  }
  return (
    <section className="panel accounts-panel">
      <div className="list-toolbar">
        <div className="segments" aria-label="Filter accounts">
          {["All accounts", "Available", "Paused"].map((value) => (
            <button
              key={value}
              onClick={() => setFilter(value)}
              className={filter === value ? "selected" : ""}
              aria-pressed={filter === value}
            >
              {value}
            </button>
          ))}
        </div>
        <label className="search-field">
          <Search size={16} />
          <input
            aria-label="Search accounts"
            placeholder="Search accounts"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
      </div>
      {error && (
        <p className="inline-error" role="alert">
          {error}
        </p>
      )}
      {!filtered.length ? (
        <EmptyState
          icon={<Users size={26} />}
          title={
            credentials.length
              ? "No matching accounts"
              : "Connect your first account"
          }
          action={
            !credentials.length && (
              <button className="button primary" onClick={onAdd}>
                <Plus size={16} />
                Connect an account
              </button>
            )
          }
        >
          {credentials.length
            ? "Try another search or filter."
            : "Sign in to a provider to bring its available models into Relay."}
        </EmptyState>
      ) : (
        <div className="account-list">
          {filtered.map((item) => {
            const status = credentialState(item);
            const sharedConnection = credentials.some(
              (other) => other.id !== item.id && other.name === item.name,
            );
            return (
              <div className="account-row" key={item.id}>
                <ProviderMark provider={item.provider} />
                <div className="account-identity">
                  <strong>
                    {item.label || item.email || providerName(item.provider)}
                  </strong>
                  <span>
                    {providerName(item.provider)}
                    <span className="middot">·</span>
                    {item.label && item.email ? item.email : item.name}
                  </span>
                  {item.runtime_only && (
                    <p className="account-warning">
                      This account has no saved connection to remove.
                    </p>
                  )}
                  {sharedConnection && (
                    <p className="account-warning">
                      This connection contains multiple accounts and cannot be
                      removed separately.
                    </p>
                  )}
                  {item.status_message && status.tone === "amber" && (
                    <p className="account-warning">{item.status_message}</p>
                  )}
                </div>
                <Status {...status} />
                <div className="account-actions">
                  <button
                    className="button secondary account-toggle"
                    disabled={busy !== null}
                    aria-label={`${item.disabled ? "Resume" : "Pause"} ${item.label || item.email || item.name}`}
                    onClick={() => void toggle(item)}
                  >
                    {item.disabled ? <Play size={14} /> : <Pause size={14} />}
                    {busy === item.name
                      ? "Updating…"
                      : item.disabled
                        ? "Resume"
                        : "Pause"}
                  </button>
                  <button
                    className="button secondary account-toggle account-remove"
                    disabled={
                      busy !== null || sharedConnection || item.runtime_only
                    }
                    aria-label={`Remove ${item.label || item.email || item.name}`}
                    onClick={() => {
                      setRemoveError("");
                      setRemoving(item);
                    }}
                  >
                    <Trash2 size={14} /> Remove
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}
      {removing && (
        <Dialog
          title="Remove account?"
          onClose={() => {
            if (busy === null) setRemoving(null);
          }}
        >
          <p className="remove-description">
            Remove{" "}
            <strong>{removing.email || removing.label || removing.name}</strong>{" "}
            from Relay? Its saved connection will be deleted. To reconnect,
            you’ll need to sign in again.
          </p>
          {removeError && (
            <p className="inline-error" role="alert">
              {removeError}
            </p>
          )}
          <div className="dialog-actions">
            <button
              className="button secondary"
              autoFocus
              disabled={busy !== null}
              onClick={() => setRemoving(null)}
            >
              Cancel
            </button>
            <button
              className="button secondary account-remove"
              disabled={busy !== null}
              onClick={() => void remove(removing)}
            >
              <Trash2 size={16} />{" "}
              {busy !== null ? "Removing…" : "Remove account"}
            </button>
          </div>
        </Dialog>
      )}
      <div className="list-footer">
        <span>
          {filtered.length} of {credentials.length} accounts
        </span>
        <span>Pausing keeps your account connected.</span>
      </div>
    </section>
  );
}
