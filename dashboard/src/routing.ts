import type { RoutingStrategy } from "./api";

export type RoutingSettings = Record<string, unknown> & {
  strategy: RoutingStrategy;
  "session-affinity": boolean;
  "session-affinity-ttl": string;
  "session-affinity-subagents": boolean;
  "subscription-first-max-observation-age": string;
  "subscription-first-prefer-weekly-reset": boolean;
  retry: Record<string, unknown> & {
    "request-retry": number;
    "max-retry-credentials": number;
    "max-retry-interval": number;
  };
};
export const routingDefaults: RoutingSettings = {
  strategy: "round-robin",
  "session-affinity": false,
  "session-affinity-ttl": "1h",
  "session-affinity-subagents": true,
  "subscription-first-max-observation-age": "30m",
  "subscription-first-prefer-weekly-reset": true,
  retry: {
    "request-retry": 0,
    "max-retry-credentials": 0,
    "max-retry-interval": 0,
  },
};
export function durationSeconds(value: unknown): number {
  if (
    typeof value !== "string" ||
    !/^(?:\d+(?:\.\d+)?(?:ns|us|µs|μs|ms|s|m|h))+$/.test(value)
  )
    throw new Error("Use a positive duration such as 30m or 1h.");
  const units: Record<string, number> = {
    ns: 1e-9,
    us: 1e-6,
    µs: 1e-6,
    μs: 1e-6,
    ms: 0.001,
    s: 1,
    m: 60,
    h: 3600,
  };
  const total = [
    ...value.matchAll(/(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/g),
  ].reduce((sum, match) => sum + Number(match[1]) * units[match[2]]!, 0);
  if (!Number.isFinite(total) || total <= 0 || total > 9223372036)
    throw new Error("Duration is outside the supported range.");
  return total;
}
export function integer(
  value: unknown,
  min: number,
  max: number,
  label: string,
): number {
  if (
    typeof value !== "number" ||
    !Number.isInteger(value) ||
    value < min ||
    value > max
  )
    throw new Error(`${label} must be a whole number from ${min} to ${max}.`);
  return value;
}
export function parseRouting(value: unknown): RoutingSettings {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Unexpected routing settings response.");
  const raw = value as Record<string, unknown>;
  if (
    raw.retry !== undefined &&
    (!raw.retry || typeof raw.retry !== "object" || Array.isArray(raw.retry))
  )
    throw new Error("Unexpected retry settings response.");
  const result = {
    ...routingDefaults,
    ...raw,
    retry: { ...routingDefaults.retry, ...(raw.retry as object | undefined) },
  } as RoutingSettings;
  if (
    ![
      "round-robin",
      "fill-first",
      "weighted-round-robin",
      "subscription-first",
    ].includes(result.strategy)
  )
    throw new Error("Unexpected routing strategy.");
  for (const key of [
    "session-affinity",
    "session-affinity-subagents",
    "subscription-first-prefer-weekly-reset",
  ] as const)
    if (typeof result[key] !== "boolean")
      throw new Error(`Unexpected routing setting: ${key}.`);
  durationSeconds(result["session-affinity-ttl"]);
  durationSeconds(result["subscription-first-max-observation-age"]);
  for (const key of [
    "request-retry",
    "max-retry-credentials",
    "max-retry-interval",
  ] as const)
    integer(result.retry[key], 0, 2147483647, key);
  return result;
}
export function routingChanges(
  before: RoutingSettings,
  after: RoutingSettings,
): Record<string, unknown> {
  parseRouting(after);
  const patch: Record<string, unknown> = {};
  for (const key of Object.keys(routingDefaults).filter(
    (key) => key !== "retry",
  ))
    if (after[key] !== before[key]) patch[key] = after[key];
  const retry = Object.fromEntries(
    Object.keys(routingDefaults.retry)
      .filter((key) => before.retry[key] !== after.retry[key])
      .map((key) => [key, after.retry[key]]),
  );
  if (Object.keys(retry).length) patch.retry = retry;
  return patch;
}
export function patchMatches(
  actual: Record<string, unknown>,
  patch: Record<string, unknown>,
): boolean {
  return Object.entries(patch).every(([key, value]) =>
    value && typeof value === "object" && !Array.isArray(value)
      ? !!actual[key] &&
        typeof actual[key] === "object" &&
        patchMatches(
          actual[key] as Record<string, unknown>,
          value as Record<string, unknown>,
        )
      : actual[key] === value,
  );
}
