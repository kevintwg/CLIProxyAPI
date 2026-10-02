import { useEffect, useMemo, useState } from "react";
import { Boxes, RefreshCw, Search } from "lucide-react";
import type { Credential, ManagementApi, Model } from "./api";
import { CopyButton, EmptyState, ProviderMark, providerName } from "./ui";

type ListedModel = Model & { providers: string[]; accounts: number };

export function Models({
  api,
  credentials,
}: {
  api: ManagementApi;
  credentials: Credential[];
}) {
  const [models, setModels] = useState<ListedModel[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const [query, setQuery] = useState("");
  const [provider, setProvider] = useState("all");
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    Promise.all(
      credentials
        .filter((item) => !item.disabled)
        .map(async (item) => ({
          provider: item.provider,
          models: await api.models(item.name, controller.signal),
        })),
    )
      .then((groups) => {
        const merged = new Map<string, ListedModel>();
        for (const group of groups)
          for (const model of group.models) {
            const current = merged.get(model.id);
            if (current) {
              current.accounts++;
              if (!current.providers.includes(group.provider))
                current.providers.push(group.provider);
            } else
              merged.set(model.id, {
                ...model,
                providers: [group.provider],
                accounts: 1,
              });
          }
        if (!controller.signal.aborted)
          setModels(
            [...merged.values()].sort((a, b) => a.id.localeCompare(b.id)),
          );
      })
      .catch((reason) => {
        if (!controller.signal.aborted)
          setError(
            reason instanceof Error ? reason.message : "Could not load models.",
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [api, credentials, revision]);
  const visible = useMemo(
    () =>
      models.filter(
        (item) =>
          (provider === "all" || item.providers.includes(provider)) &&
          `${item.id} ${item.display_name ?? ""}`
            .toLowerCase()
            .includes(query.toLowerCase()),
      ),
    [models, provider, query],
  );
  return (
    <section className="panel models-panel">
      <div className="list-toolbar">
        <label className="search-field grow">
          <Search size={17} />
          <input
            placeholder="Find a model"
            aria-label="Search models"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
        <select
          aria-label="Filter models by provider"
          value={provider}
          onChange={(event) => setProvider(event.target.value)}
        >
          <option value="all">All providers</option>
          {[...new Set(credentials.map((item) => item.provider))].map((id) => (
            <option key={id} value={id}>
              {providerName(id)}
            </option>
          ))}
        </select>
        <button
          className="button icon-button"
          aria-label="Refresh models"
          disabled={loading}
          onClick={() => setRevision((value) => value + 1)}
        >
          <RefreshCw size={17} className={loading ? "spinning" : ""} />
        </button>
      </div>
      {error ? (
        <EmptyState
          icon={<Boxes size={25} />}
          title="Models could not be loaded"
          action={
            <button
              className="button secondary"
              onClick={() => setRevision((value) => value + 1)}
            >
              Try again
            </button>
          }
        >
          {error}
        </EmptyState>
      ) : loading ? (
        <div className="loading-block" role="status">
          Loading your model library…
        </div>
      ) : !visible.length ? (
        <EmptyState
          icon={<Boxes size={26} />}
          title={
            models.length
              ? "No matching models"
              : "Your model library starts here"
          }
        >
          {models.length
            ? "Try another name or provider."
            : "Models appear here when connected accounts make them available. Paused accounts are excluded."}
        </EmptyState>
      ) : (
        <div className="model-list">
          {visible.map((model) => (
            <div className="model-row" key={model.id}>
              <ProviderMark provider={model.providers[0]} small />
              <div className="model-identity">
                <strong>{model.display_name || model.id}</strong>
                {model.display_name && <code>{model.id}</code>}
                <span>
                  {model.providers.map(providerName).join(", ")} ·{" "}
                  {model.accounts}{" "}
                  {model.accounts === 1 ? "account" : "accounts"}
                </span>
              </div>
              <CopyButton value={model.id} compact label={`Copy ${model.id}`} />
            </div>
          ))}
        </div>
      )}
      <div className="list-footer">
        <span>
          {loading || error ? "Model library" : `${visible.length} models`}
        </span>
        <span>Copy a model ID to use it in your app.</span>
      </div>
    </section>
  );
}
