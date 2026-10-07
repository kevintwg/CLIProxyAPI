import {
  durationSeconds,
  integer,
  parseRouting,
  patchMatches,
  type RoutingSettings,
} from "./routing";
export interface Credential {
  id: string;
  name: string;
  auth_index?: string;
  provider: string;
  label?: string;
  email?: string;
  status: string;
  status_message?: string;
  disabled: boolean;
  unavailable: boolean;
  runtime_only?: boolean;
  success?: number;
  failed?: number;
  updated_at?: string;
  priority?: number;
  weight?: number;
  routing_tier?: number;
  routing_weekly_reset_at?: string;
  plan_type?: string;
  supports_quota?: boolean;
  supports_reset?: boolean;
  quota_provider?: string;
  usage_limits?: UsageSnapshot;
  routing_profile?: {
    tier?: number;
    tier_source: "manual" | "plan" | "unknown";
    plan?: string;
    weekly_reset_at?: string;
    reset_source: "manual" | "observed" | "unknown";
    observed_at?: string;
    banked_reset_expires_at?: string;
    banked_reset_observed_at?: string;
    banked_reset_count?: number;
    quota_reserve_percent?: number;
    quota_reserve_blocked?: boolean;
  };
}

export interface UsageWindow {
  used_percent: number;
  remaining_percent: number;
  window_minutes: number;
  resets_at: string;
  observed_at: string;
}

export interface UsageBucket {
  window?: string;
  remaining_fraction: number;
  reset_time?: string;
  description?: string;
}

export interface UsageGroup {
  display_name?: string;
  buckets: UsageBucket[];
}

export interface UsageMetric {
  key: string;
  label: string;
  value: number;
  unit?: string;
  format?: "number" | "currency";
  currency?: string;
}

export interface UsageSnapshot {
  observed_at?: string;
  plan?: string;
  primary?: UsageWindow;
  secondary?: UsageWindow;
  banked_reset_count?: number;
  banked_reset_expires_at?: string;
  banked_reset_observed_at?: string;
  subscription?: { plan?: string; tierName?: string; tierId?: string };
  summary?: UsageMetric[];
  groups?: UsageGroup[];
}

export interface Model {
  id: string;
  display_name?: string;
  owned_by?: string;
  type?: string;
}

export type RoutingStrategy =
  "round-robin" | "weighted-round-robin" | "fill-first" | "subscription-first";
export interface LoginSession {
  url: string;
  state: string;
  flow?: "device";
  user_code?: string;
}
export interface LoginStatus {
  status: "wait" | "ok" | "error";
  error?: string;
}

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("Unexpected management response.");
  }
  return value as Record<string, unknown>;
}

function string(value: unknown): string {
  if (typeof value !== "string" || value.length === 0) {
    throw new Error("Unexpected management response: missing string.");
  }
  return value;
}

function boolean(value: unknown): boolean {
  if (typeof value !== "boolean")
    throw new Error("Unexpected management response: missing boolean.");
  return value;
}

function optionalFields<T extends object>(
  source: Record<string, unknown>,
  target: T,
  fields: readonly string[],
  type: "string" | "number" | "boolean",
): void {
  for (const field of fields) {
    const value = source[field];
    if (value === undefined) continue;
    if (
      typeof value !== type ||
      (typeof value === "number" && !Number.isFinite(value))
    ) {
      throw new Error(`Unexpected management response: invalid ${field}.`);
    }
    Object.assign(target, { [field]: value });
  }
}

function credential(value: unknown): Credential {
  const item = object(value);
  const result: Credential = {
    id: string(item.id),
    name: string(item.name),
    provider: string(item.provider),
    status: string(item.status),
    disabled: boolean(item.disabled),
    unavailable: boolean(item.unavailable),
  };
  optionalFields(
    item,
    result,
    ["auth_index", "label", "email", "status_message", "updated_at"],
    "string",
  );
  optionalFields(item, result, ["success", "failed"], "number");
  optionalFields(item, result, ["runtime_only"], "boolean");
  optionalFields(item, result, ["supports_quota", "supports_reset"], "boolean");
  optionalFields(item, result, ["quota_provider"], "string");
  optionalFields(
    item,
    result,
    ["priority", "weight", "routing_tier"],
    "number",
  );
  optionalFields(
    item,
    result,
    ["routing_weekly_reset_at", "plan_type"],
    "string",
  );
  if (
    result.routing_weekly_reset_at !== undefined &&
    !Number.isFinite(Date.parse(result.routing_weekly_reset_at))
  )
    throw new Error("Unexpected routing reset date.");
  if (result.priority !== undefined)
    integer(result.priority, -2147483648, 2147483647, "Priority");
  if (result.weight !== undefined) integer(result.weight, 0, 1000000, "Weight");
  if (result.routing_tier !== undefined)
    integer(result.routing_tier, 0, 1000, "Tier rank");
  if (item.routing_profile !== undefined) {
    const profile = object(item.routing_profile);
    if (
      !["manual", "plan", "unknown"].includes(String(profile.tier_source)) ||
      !["manual", "observed", "unknown"].includes(String(profile.reset_source))
    )
      throw new Error("Unexpected routing profile response.");
    const parsed = {
      tier_source: profile.tier_source,
      reset_source: profile.reset_source,
    } as NonNullable<Credential["routing_profile"]>;
    optionalFields(
      profile,
      parsed,
      ["tier", "quota_reserve_percent", "banked_reset_count"],
      "number",
    );
    optionalFields(
      profile,
      parsed,
      [
        "plan",
        "weekly_reset_at",
        "observed_at",
        "banked_reset_expires_at",
        "banked_reset_observed_at",
      ],
      "string",
    );
    if (parsed.tier !== undefined) integer(parsed.tier, 0, 1000, "Tier rank");
    optionalFields(profile, parsed, ["quota_reserve_blocked"], "boolean");
    if (
      parsed.quota_reserve_percent !== undefined &&
      (parsed.quota_reserve_percent < 0 || parsed.quota_reserve_percent > 100)
    )
      throw new Error("Unexpected routing quota reserve percentage.");
    for (const date of [
      parsed.weekly_reset_at,
      parsed.observed_at,
      parsed.banked_reset_expires_at,
      parsed.banked_reset_observed_at,
    ])
      if (date !== undefined && !Number.isFinite(Date.parse(date)))
        throw new Error("Unexpected routing profile date.");
    if (
      parsed.banked_reset_count !== undefined &&
      (!Number.isInteger(parsed.banked_reset_count) ||
        parsed.banked_reset_count < 0)
    )
      throw new Error("Unexpected banked reset count.");
    result.routing_profile = parsed;
  }
  if (item.usage_limits !== undefined)
    result.usage_limits = usageSnapshot(item.usage_limits);
  return result;
}

function finite(value: unknown, message: string): number {
  if (typeof value !== "number" || !Number.isFinite(value))
    throw new Error(`Unexpected management response: invalid ${message}.`);
  return value;
}

function date(value: unknown, message: string): string {
  if (typeof value !== "string" || !Number.isFinite(Date.parse(value)))
    throw new Error(`Unexpected management response: invalid ${message}.`);
  return value;
}

function optionalDate(
  item: Record<string, unknown>,
  key: string,
): string | undefined {
  if (item[key] === undefined || item[key] === null) return undefined;
  return date(item[key], key);
}

function usageWindow(value: unknown): UsageWindow {
  const item = object(value);
  return {
    used_percent: finite(item.used_percent, "used_percent"),
    remaining_percent: finite(item.remaining_percent, "remaining_percent"),
    window_minutes: finite(item.window_minutes, "window_minutes"),
    resets_at: date(item.resets_at, "resets_at"),
    observed_at: date(item.observed_at, "observed_at"),
  };
}

function usageSnapshot(value: unknown): UsageSnapshot {
  const item = object(value);
  const result: UsageSnapshot = {};
  if (item.observed_at !== undefined)
    result.observed_at = date(item.observed_at, "observed_at");
  if (item.plan !== undefined) result.plan = string(item.plan);
  if (item.primary !== undefined && item.primary !== null)
    result.primary = usageWindow(item.primary);
  if (item.secondary !== undefined && item.secondary !== null)
    result.secondary = usageWindow(item.secondary);
  if (item.banked_reset_count !== undefined) {
    const count = finite(item.banked_reset_count, "banked_reset_count");
    if (!Number.isInteger(count) || count < 0)
      throw new Error(
        "Unexpected management response: invalid banked_reset_count.",
      );
    result.banked_reset_count = count;
  }
  result.banked_reset_expires_at = optionalDate(
    item,
    "banked_reset_expires_at",
  );
  result.banked_reset_observed_at = optionalDate(
    item,
    "banked_reset_observed_at",
  );
  if (item.subscription !== undefined && item.subscription !== null) {
    const subscription = object(item.subscription);
    result.subscription = {};
    if (subscription.plan !== undefined)
      result.subscription.plan = string(subscription.plan);
    if (subscription.tierName !== undefined)
      result.subscription.tierName = string(subscription.tierName);
    if (subscription.tierId !== undefined)
      result.subscription.tierId = string(subscription.tierId);
  }
  if (item.summary !== undefined) {
    if (!Array.isArray(item.summary))
      throw new Error("Unexpected management response: invalid summary.");
    result.summary = item.summary.map((value) => {
      const metric = object(value);
      const format = metric.format;
      if (format !== undefined && format !== "number" && format !== "currency")
        throw new Error(
          "Unexpected management response: invalid metric format.",
        );
      return {
        key: string(metric.key),
        label: string(metric.label),
        value: finite(metric.value, "metric value"),
        ...(metric.unit === undefined ? {} : { unit: string(metric.unit) }),
        ...(format === undefined ? {} : { format }),
        ...(metric.currency === undefined
          ? {}
          : { currency: string(metric.currency) }),
      };
    });
  }
  if (item.groups !== undefined) {
    if (!Array.isArray(item.groups))
      throw new Error("Unexpected management response: invalid groups.");
    result.groups = item.groups.map((value) => {
      const group = object(value);
      if (!Array.isArray(group.buckets))
        throw new Error("Unexpected management response: invalid buckets.");
      return {
        ...(group.display_name === undefined && group.displayName === undefined
          ? {}
          : {
              display_name: string(group.display_name ?? group.displayName),
            }),
        buckets: group.buckets.map((bucketValue) => {
          const bucket = object(bucketValue);
          return {
            ...(bucket.window === undefined
              ? {}
              : { window: string(bucket.window) }),
            remaining_fraction: finite(
              bucket.remaining_fraction ?? bucket.remainingFraction,
              "remaining_fraction",
            ),
            ...(bucket.reset_time === undefined &&
            bucket.resetTime === undefined
              ? {}
              : {
                  reset_time: date(
                    bucket.reset_time ?? bucket.resetTime,
                    "reset_time",
                  ),
                }),
            ...(bucket.description === undefined
              ? {}
              : { description: string(bucket.description) }),
          };
        }),
      };
    });
  }
  return result;
}

function model(value: unknown): Model {
  const item = object(value);
  const result: Model = { id: string(item.id) };
  optionalFields(item, result, ["display_name", "owned_by", "type"], "string");
  return result;
}

function array<T>(value: unknown, parse: (item: unknown) => T): T[] {
  if (!Array.isArray(value))
    throw new Error("Unexpected management response: missing list.");
  return value.map(parse);
}

function strategy(value: unknown): RoutingStrategy {
  if (
    value !== "round-robin" &&
    value !== "weighted-round-robin" &&
    value !== "fill-first" &&
    value !== "subscription-first"
  ) {
    throw new Error(
      "Unexpected management response: invalid routing strategy.",
    );
  }
  return value;
}

function readStrategy(value: unknown): RoutingStrategy {
  const normalized =
    typeof value === "string" ? value.trim().toLowerCase() : value;
  switch (normalized) {
    case "ff":
    case "fillfirst":
      return "fill-first";
    case "wrr":
    case "weightedroundrobin":
      return "weighted-round-robin";
    default:
      return strategy(normalized);
  }
}

class HttpError extends Error {
  constructor(
    readonly status: number,
    readonly missingConfigPath: boolean,
    message: string,
    readonly detail?: string,
  ) {
    super(message);
  }
}

/** True when the server refused the management key itself. */
export function isKeyRejected(error: unknown): boolean {
  return (
    error instanceof HttpError && (error.status === 401 || error.status === 403)
  );
}

export class ManagementApi {
  readonly #key: string;

  constructor(key: string) {
    if (!key.trim()) throw new Error("A management key is required.");
    this.#key = key;
  }

  private redact(message: string): string {
    return message.split(this.#key).join("[redacted]");
  }

  private async request(
    path: string,
    method = "GET",
    body?: unknown,
    signal?: AbortSignal,
  ): Promise<unknown> {
    let response: Response;
    let text: string;
    try {
      response = await fetch(`/v8/management${path}`, {
        method,
        headers: {
          Authorization: `Bearer ${this.#key}`,
          "Content-Type": "application/json",
        },
        body: body === undefined ? undefined : JSON.stringify(body),
        signal,
        cache: "no-store",
        redirect: "error",
      });
      text = await response.text();
    } catch (error) {
      // Browser DOMExceptions can come from a different realm than Error.
      if (
        typeof error === "object" &&
        error !== null &&
        "name" in error &&
        error.name === "AbortError"
      )
        throw error;
      throw new Error(
        this.redact(
          error instanceof Error ? error.message : "Management request failed.",
        ),
      );
    }
    let data: unknown;
    try {
      data = JSON.parse(text) as unknown;
    } catch {
      data = undefined;
    }
    if (!response.ok) {
      const detail =
        typeof data === "object" && data !== null && !Array.isArray(data)
          ? (data as Record<string, unknown>)
          : undefined;
      const missingConfigPath =
        response.status === 404 &&
        detail?.error === "not_found" &&
        Object.keys(detail).length === 1;
      const message = [detail?.error, detail?.message]
        .filter((part) => typeof part === "string")
        .join(": ");
      const explanation =
        response.status === 401
          ? "Management key was not accepted. Check your key and reconnect."
          : response.status === 403
            ? "Management access was denied. Check whether remote management is enabled and this connection is allowed."
            : "Management request failed";
      throw new HttpError(
        response.status,
        missingConfigPath,
        this.redact(
          `${explanation} (${response.status})${message ? `: ${message}` : "."}`,
        ),
        typeof detail?.error === "string"
          ? this.redact(detail.error)
          : undefined,
      );
    }
    if (data === undefined)
      throw new Error("Unexpected management response: invalid JSON.");
    return data;
  }

  async credentials(signal?: AbortSignal): Promise<Credential[]> {
    return array(
      object(await this.request("/credentials", "GET", undefined, signal))
        .files,
      credential,
    );
  }

  async models(name: string, signal?: AbortSignal): Promise<Model[]> {
    const data = await this.request(
      `/credentials/models?name=${encodeURIComponent(name)}`,
      "GET",
      undefined,
      signal,
    );
    return array(object(data).models, model);
  }

  async routing(signal?: AbortSignal): Promise<RoutingStrategy> {
    try {
      return readStrategy(
        await this.request(
          "/config/routing/strategy",
          "GET",
          undefined,
          signal,
        ),
      );
    } catch (error) {
      // ConfigV8 distinguishes an absent path from disabled management's empty 404.
      if (error instanceof HttpError && error.missingConfigPath)
        return "round-robin";
      throw error;
    }
  }

  async routingSettings(signal?: AbortSignal): Promise<RoutingSettings> {
    try {
      const raw = object(
        await this.request("/config/routing", "GET", undefined, signal),
      );
      if (raw.strategy !== undefined) raw.strategy = readStrategy(raw.strategy);
      return parseRouting(raw);
    } catch (error) {
      if (error instanceof HttpError && error.missingConfigPath)
        return parseRouting({});
      throw error;
    }
  }

  async setRoutingSettings(
    patch: Record<string, unknown>,
  ): Promise<RoutingSettings> {
    parseRouting(patch);
    await this.mutate("/config/routing", "PATCH", patch);
    const saved = await this.routingSettings();
    // The server can normalize durations to equivalent spellings.
    const comparable = { ...saved };
    for (const key of [
      "session-affinity-ttl",
      "subscription-first-max-observation-age",
    ]) {
      if (
        typeof patch[key] === "string" &&
        durationSeconds(patch[key]) === durationSeconds(saved[key])
      )
        comparable[key] = patch[key];
    }
    if (!patchMatches(comparable, patch))
      throw new Error(
        "Routing write was accepted, but saved values could not be confirmed. Your draft is retained.",
      );
    return saved;
  }

  async setCredentialFields(
    name: string,
    fields: Record<string, unknown>,
  ): Promise<Credential> {
    const allowed = [
      "priority",
      "weight",
      "routing_tier",
      "routing_weekly_reset_at",
    ];
    if (
      !Object.keys(fields).length ||
      Object.keys(fields).some((key) => !allowed.includes(key))
    )
      throw new Error("Invalid account routing fields.");
    if (fields.priority !== undefined)
      integer(fields.priority, -2147483648, 2147483647, "Priority");
    if (fields.weight !== undefined)
      integer(fields.weight, 0, 1000000, "Weight");
    if (fields.routing_tier !== undefined && fields.routing_tier !== null)
      integer(fields.routing_tier, 0, 1000, "Tier rank");
    const reset = fields.routing_weekly_reset_at;
    if (
      reset !== undefined &&
      reset !== null &&
      (typeof reset !== "string" ||
        !Number.isFinite(Date.parse(reset)) ||
        Date.parse(reset) <= Date.now())
    )
      throw new Error("Weekly reset must be a future date and time.");
    await this.mutate("/credentials/fields", "PATCH", { name, ...fields });
    const saved = (await this.credentials()).find((item) => item.name === name);
    if (
      !saved ||
      !Object.entries(fields).every(([key, value]) =>
        value === null
          ? saved[key as keyof Credential] === undefined
          : key === "routing_weekly_reset_at"
            ? Date.parse(String(saved[key])) === Date.parse(String(value))
            : saved[key as keyof Credential] === value,
      )
    )
      throw new Error(
        "Account write was accepted, but saved values could not be confirmed. Your draft is retained.",
      );
    return saved;
  }

  private async mutate(
    path: string,
    method: string,
    body?: unknown,
  ): Promise<void> {
    const data = object(await this.request(path, method, body));
    if (data.status !== "ok")
      throw new Error(
        "Unexpected management response: mutation was not confirmed.",
      );
  }

  async changePassword(current: string, next: string): Promise<void> {
    try {
      await this.mutate("/password", "PUT", {
        current_password: current,
        new_password: next,
      });
    } catch (error) {
      // These statuses carry a plain explanation meant for the person signing in.
      if (
        error instanceof HttpError &&
        error.detail &&
        [400, 403, 409].includes(error.status)
      )
        throw new Error(error.detail);
      throw error;
    }
  }

  async setRouting(value: RoutingStrategy): Promise<void> {
    await this.mutate("/config/routing/strategy", "PUT", strategy(value));
  }

  async setCredentialEnabled(
    value: Credential,
    enabled: boolean,
  ): Promise<void> {
    await this.mutate("/credentials/status", "PATCH", {
      name: value.name,
      auth_index: value.auth_index,
      disabled: !enabled,
    });
  }

  async removeCredential(value: Credential): Promise<void> {
    await this.mutate(
      `/credentials?name=${encodeURIComponent(value.name)}`,
      "DELETE",
    );
  }

  async fetchUsage(value: Credential): Promise<UsageSnapshot> {
    return usageSnapshot(
      await this.request("/credentials/usage/fetch", "POST", {
        auth_index: value.auth_index,
        provider: value.quota_provider ?? value.provider,
      }),
    );
  }

  async redeemReset(value: Credential): Promise<UsageSnapshot | undefined> {
    const data = object(
      await this.request("/credentials/usage/redeem", "POST", {
        auth_index: value.auth_index,
        provider: value.quota_provider ?? value.provider,
      }),
    );
    if (data.status !== "ok")
      throw new Error(
        "Unexpected management response: reset was not confirmed.",
      );
    return data.usage === undefined ? undefined : usageSnapshot(data.usage);
  }

  async startLogin(
    provider: string,
    signal?: AbortSignal,
  ): Promise<LoginSession> {
    const data = object(
      await this.request(
        `/oauth/auth-url?provider=${encodeURIComponent(provider)}`,
        "GET",
        undefined,
        signal,
      ),
    );
    const url = string(data.url);
    let parsed: URL;
    try {
      parsed = new URL(url);
    } catch {
      throw new Error("Unexpected management response: invalid login URL.");
    }
    if (
      !["http:", "https:"].includes(parsed.protocol) ||
      parsed.username ||
      parsed.password
    ) {
      throw new Error("Unexpected management response: invalid login URL.");
    }
    const result: LoginSession = { url, state: string(data.state) };
    if (data.flow !== undefined) {
      if (data.flow !== "device")
        throw new Error("Unexpected management response: invalid login flow.");
      result.flow = data.flow;
    }
    if (data.user_code !== undefined) result.user_code = string(data.user_code);
    return result;
  }

  async loginStatus(state: string, signal?: AbortSignal): Promise<LoginStatus> {
    const data = object(
      await this.request(
        `/oauth/status?state=${encodeURIComponent(state)}`,
        "GET",
        undefined,
        signal,
      ),
    );
    if (
      data.status !== "wait" &&
      data.status !== "ok" &&
      data.status !== "error"
    ) {
      throw new Error("Unexpected management response: invalid login status.");
    }
    const result: LoginStatus = { status: data.status };
    optionalFields(data, result, ["error"], "string");
    if (result.error !== undefined) result.error = this.redact(result.error);
    return result;
  }

  async cancelLogin(state: string): Promise<void> {
    await this.mutate(
      `/oauth/session?state=${encodeURIComponent(state)}`,
      "DELETE",
    );
  }

  async submitCallback(provider: string, redirect_url: string): Promise<void> {
    await this.mutate("/oauth/callback", "POST", { provider, redirect_url });
  }
}
