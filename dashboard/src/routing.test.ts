import { describe, expect, it } from "vitest";
import { durationSeconds, parseRouting, routingChanges } from "./routing";
describe("routing edits", () => {
  it("uses runtime zero defaults for omitted retry fields and patches explicit retry settings", () => {
    const base = parseRouting({});
    expect(base.retry).toEqual({
      "request-retry": 0,
      "max-retry-credentials": 0,
      "max-retry-interval": 0,
    });
    const next = {
      ...base,
      retry: { ...base.retry, "request-retry": 3, "max-retry-interval": 30 },
    };
    expect(routingChanges(base, next)).toEqual({
      retry: { "request-retry": 3, "max-retry-interval": 30 },
    });
  });
  it("preserves unrelated fields while producing a minimal nested patch", () => {
    const base = parseRouting({ future: "preserved", retry: { future: 2 } });
    const next = { ...base, retry: { ...base.retry, "request-retry": 5 } };
    expect(routingChanges(base, next)).toEqual({
      retry: { "request-retry": 5 },
    });
    expect(base.retry.future).toBe(2);
  });
  it("accepts composite durations and rejects zero, negative and incomplete values", () => {
    expect(durationSeconds("1h30m")).toBe(5400);
    for (const value of ["0s", "-1h", "1", "1d", "1hgarbage"])
      expect(() => durationSeconds(value)).toThrow();
  });
});
