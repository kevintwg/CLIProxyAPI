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
  routing_profile?: {
    tier?: number;
    tier_source: "manual" | "plan" | "unknown";
    plan?: string;
    weekly_reset_at?: string;
    reset_source: "manual" | "observed" | "unknown";
    observed_at?: string;
  };
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
    optionalFields(profile, parsed, ["tier"], "number");
    optionalFields(
      profile,
      parsed,
      ["plan", "weekly_reset_at", "observed_at"],
      "string",
    );
    if (parsed.tier !== undefined) integer(parsed.tier, 0, 1000, "Tier rank");
    for (const date of [parsed.weekly_reset_at, parsed.observed_at])
      if (date !== undefined && !Number.isFinite(Date.parse(date)))
        throw new Error("Unexpected routing profile date.");
    result.routing_profile = parsed;
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
  ) {
    super(message);
  }
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
