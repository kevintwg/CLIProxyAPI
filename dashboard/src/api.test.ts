import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ManagementApi, type Credential } from "./api";

const key = "private-management-key";
const account: Credential = {
  id: "account-1",
  name: "name &?#.json",
  auth_index: "index-1",
  provider: "codex",
  status: "active",
  disabled: false,
  unavailable: false,
};
const fetchMock = vi.fn<typeof fetch>();
const api = new ManagementApi(key);

function respond(data: unknown, status = 200): void {
  fetchMock.mockResolvedValueOnce(
    new Response(JSON.stringify(data), { status }),
  );
}

function call(index = 0): { url: string; options: RequestInit } {
  const [url, options] = fetchMock.mock.calls[index]!;
  return { url: String(url), options: options! };
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ManagementApi", () => {
  it("requires a key and keeps authenticated requests on the same origin", async () => {
    expect(() => new ManagementApi(" ")).toThrow("required");
    respond({ files: [account] });
    expect(await api.credentials()).toEqual([account]);
    expect(call()).toMatchObject({
      url: "/v8/management/credentials",
      options: {
        method: "GET",
        cache: "no-store",
        redirect: "error",
        headers: { Authorization: `Bearer ${key}` },
      },
    });
    expect(JSON.stringify(api)).not.toContain(key);
  });

  it("retains relevant credential metadata and discards unrelated fields", async () => {
    const metadata = {
      label: "Work",
      email: "test@example.com",
      success: 2,
      failed: 1,
      runtime_only: true,
      updated_at: "2026-10-02",
    };
    respond({ files: [{ ...account, ...metadata, access_token: "secret" }] });
    expect(await api.credentials()).toEqual([{ ...account, ...metadata }]);
  });

  it.each([
    {},
    { files: null },
    { files: [{}] },
    { files: [{ ...account, disabled: "false" }] },
    { files: [{ ...account, success: "2" }] },
  ])("rejects malformed credentials: %j", async (data) => {
    respond(data);
    await expect(api.credentials()).rejects.toThrow(
      "Unexpected management response",
    );
  });

  it("encodes credential names and permits optional model metadata", async () => {
    respond({
      models: [
        { id: "model-1" },
        {
          id: "model-2",
          display_name: "Two",
          owned_by: "codex",
          type: "chat",
          secret: "discard",
        },
      ],
    });
    expect(await api.models(account.name)).toEqual([
      { id: "model-1" },
      { id: "model-2", display_name: "Two", owned_by: "codex", type: "chat" },
    ]);
    expect(call().url).toBe(
      `/v8/management/credentials/models?name=${encodeURIComponent(account.name)}`,
    );
  });

  it.each([
    { models: {} },
    { models: [{ id: 42 }] },
    { models: [{ id: "x", type: false }] },
  ])("rejects malformed model responses: %j", async (data) => {
    respond(data);
    await expect(api.models("name")).rejects.toThrow(
      "Unexpected management response",
    );
  });

  it("uses the raw routing scalar and defaults only for ConfigV8 missing paths", async () => {
    respond("weighted-round-robin");
    expect(await api.routing()).toBe("weighted-round-robin");
    respond({ error: "not_found" }, 404);
    expect(await api.routing()).toBe("round-robin");
  });

  it.each([
    [404, undefined],
    [404, { error: "management disabled" }],
    [404, { error: "not_found", message: "disabled" }],
    [401, { error: "not_found" }],
    [403, { error: "remote management disabled" }],
    [500, { error: "read_failed" }],
  ])(
    "does not hide management failures behind a routing default (%s)",
    async (status, data) => {
      if (data === undefined)
        fetchMock.mockResolvedValueOnce(new Response("", { status }));
      else respond(data, status);
      await expect(api.routing()).rejects.toThrow(`(${status})`);
    },
  );

  it.each([null, { strategy: "round-robin" }, "unknown", ""])(
    "rejects unexpected routing values: %j",
    async (data) => {
      respond(data);
      await expect(api.routing()).rejects.toThrow("invalid routing strategy");
    },
  );

  it.each([
    ["ff", "fill-first"],
    ["fillfirst", "fill-first"],
    ["wrr", "weighted-round-robin"],
    ["weightedroundrobin", "weighted-round-robin"],
    [" FILL-FIRST ", "fill-first"],
    [" Weighted-Round-Robin ", "weighted-round-robin"],
    [" ROUND-ROBIN ", "round-robin"],
  ])("reads runtime routing alias %s as %s", async (input, expected) => {
    respond(input);
    expect(await api.routing()).toBe(expected);
  });

  it("sends exact mutation bodies and requires confirmation", async () => {
    respond({ status: "ok" });
    await api.setRouting("fill-first");
    expect(call()).toMatchObject({
      url: "/v8/management/config/routing/strategy",
      options: { method: "PUT", body: '"fill-first"' },
    });
    respond({ status: "ok" });
    await api.setCredentialEnabled(account, false);
    expect(call(1)).toMatchObject({
      url: "/v8/management/credentials/status",
      options: {
        method: "PATCH",
        body: JSON.stringify({
          name: account.name,
          auth_index: account.auth_index,
          disabled: true,
        }),
      },
    });
    respond({ status: "ok" });
    await api.setCredentialEnabled(account, true);
    expect(JSON.parse(String(call(2).options.body)).disabled).toBe(false);
    respond({ status: "error" });
    await expect(api.setRouting("round-robin")).rejects.toThrow(
      "not confirmed",
    );
  });

  it("reports JSON HTTP errors while redacting the management key", async () => {
    respond({ error: "unauthorized", message: `Bad key: ${key}` }, 401);
    await expect(api.credentials()).rejects.toThrow(
      "Management key was not accepted. Check your key and reconnect. (401): unauthorized: Bad key: [redacted]",
    );
    fetchMock.mockRejectedValueOnce(new Error(`Failed ${key}`));
    await expect(api.credentials()).rejects.toThrow("Failed [redacted]");
  });

  it("explains how to resolve denied remote management access", async () => {
    respond({ error: "remote management disabled" }, 403);
    await expect(api.credentials()).rejects.toThrow(
      "Check whether remote management is enabled",
    );
  });

  it("reports non-JSON HTTP failures and rejects malformed successful JSON", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response("<html>bad gateway</html>", { status: 502 }),
    );
    await expect(api.credentials()).rejects.toThrow("(502)");
    fetchMock.mockResolvedValueOnce(new Response("not json"));
    await expect(api.credentials()).rejects.toThrow("invalid JSON");
  });

  it("passes AbortSignal through every read and preserves cancellation", async () => {
    const controller = new AbortController();
    const signal = controller.signal;
    const reads = [
      () => api.credentials(signal),
      () => api.models("name", signal),
      () => api.routing(signal),
      () => api.startLogin("codex", signal),
      () => api.loginStatus("state", signal),
    ];
    controller.abort();
    for (const read of reads) {
      fetchMock.mockRejectedValueOnce(
        new DOMException("Aborted", "AbortError"),
      );
      await expect(read()).rejects.toMatchObject({ name: "AbortError" });
      expect(call(fetchMock.mock.calls.length - 1).options.signal).toBe(signal);
    }
  });

  it("preserves cancellation while reading the response body", async () => {
    const response = new Response("{}");
    const aborted = new DOMException("Aborted", "AbortError");
    vi.spyOn(response, "text").mockRejectedValueOnce(aborted);
    fetchMock.mockResolvedValueOnce(response);
    await expect(api.credentials()).rejects.toBe(aborted);
  });

  it("redacts failures while reading the response body", async () => {
    const response = new Response("{}");
    vi.spyOn(response, "text").mockRejectedValueOnce(
      new Error(`Failed ${key}`),
    );
    fetchMock.mockResolvedValueOnce(response);
    await expect(api.credentials()).rejects.toThrow("Failed [redacted]");
  });

  it("validates login sessions and encodes provider and state parameters", async () => {
    respond({
      url: "https://login.example.com/oauth",
      state: "a&?b",
      status: "ok",
    });
    expect(await api.startLogin("a&b")).toEqual({
      url: "https://login.example.com/oauth",
      state: "a&?b",
    });
    expect(call().url).toBe("/v8/management/oauth/auth-url?provider=a%26b");
    respond({ status: "wait" });
    expect(await api.loginStatus("a&?b")).toEqual({ status: "wait" });
    expect(call(1).url).toBe("/v8/management/oauth/status?state=a%26%3Fb");
    respond({ status: "error", error: "Login failed" });
    expect(await api.loginStatus("s")).toEqual({
      status: "error",
      error: "Login failed",
    });
  });

  it.each([
    "javascript:alert(1)",
    "file:///tmp/login",
    "/relative",
    "invalid",
    "https://user:password@example.com",
  ])("rejects invalid login URLs: %s", async (url) => {
    respond({ url, state: "s" });
    await expect(api.startLogin("codex")).rejects.toThrow("invalid login URL");
  });

  it("retains device codes for verification URLs without a prefilled code", async () => {
    const session = {
      url: "https://example.invalid/activate",
      state: "generated-state",
      flow: "device",
      user_code: "TEST-1234",
    };
    respond({ ...session, device_code: "private-provider-token" });
    expect(await api.startLogin("xai")).toEqual(session);
  });

  it.each([
    { flow: 3 },
    { flow: "unexpected" },
    { user_code: 1234 },
    { user_code: "" },
  ])("rejects malformed device sign-in metadata: %j", async (metadata) => {
    respond({
      url: "https://example.invalid/activate",
      state: "s",
      ...metadata,
    });
    await expect(api.startLogin("xai")).rejects.toThrow(
      "Unexpected management response",
    );
  });

  it("rejects missing session fields and unexpected login states", async () => {
    respond({ url: "https://login.example.com" });
    await expect(api.startLogin("codex")).rejects.toThrow("missing string");
    respond({ status: "complete" });
    await expect(api.loginStatus("s")).rejects.toThrow("invalid login status");
    respond({ status: "error", error: 42 });
    await expect(api.loginStatus("s")).rejects.toThrow("invalid error");
  });

  it("cancels a specific session and submits callback fields", async () => {
    respond({ status: "ok", cancelled: true });
    await api.cancelLogin("state&value");
    expect(call()).toMatchObject({
      url: "/v8/management/oauth/session?state=state%26value",
      options: { method: "DELETE", body: undefined },
    });
    respond({ status: "ok" });
    await api.submitCallback(
      "codex",
      "http://localhost:1455/callback?code=test&state=value",
    );
    expect(call(1)).toMatchObject({
      url: "/v8/management/oauth/callback",
      options: {
        method: "POST",
        body: JSON.stringify({
          provider: "codex",
          redirect_url: "http://localhost:1455/callback?code=test&state=value",
        }),
      },
    });
  });
});

describe("routing customization", () => {
  it("loads full routing settings and preserves unknown fields", async () => {
    respond({
      strategy: "subscription-first",
      future: { enabled: true },
      retry: { extra: 9 },
    });
    const settings = await api.routingSettings();
    expect(settings.future).toEqual({ enabled: true });
    expect(settings.retry).toMatchObject({ extra: 9, "request-retry": 0 });
    expect(call().url).toBe("/v8/management/config/routing");
  });
  it.each([
    { "session-affinity": "true" },
    { retry: [] },
    { "session-affinity-ttl": "forever" },
    { retry: { "request-retry": -1 } },
  ])("rejects malformed known settings %j", async (value) => {
    respond(value);
    await expect(api.routingSettings()).rejects.toThrow();
  });
  it("patches only edits and rejects an unpersisted success", async () => {
    respond({ status: "ok" });
    respond({ strategy: "round-robin" });
    await expect(
      api.setRoutingSettings({ strategy: "subscription-first" }),
    ).rejects.toThrow("could not be confirmed");
    expect(call().options.method).toBe("PATCH");
    expect(JSON.parse(String(call().options.body))).toEqual({
      strategy: "subscription-first",
    });
  });
  it("accepts equivalent persisted duration formatting", async () => {
    respond({ status: "ok" });
    respond({ "session-affinity-ttl": "1h0m0s" });
    await expect(
      api.setRoutingSettings({ "session-affinity-ttl": "1h" }),
    ).resolves.toBeDefined();
  });
  it("sends changed account fields only and verifies persistence", async () => {
    respond({ status: "ok" });
    respond({ files: [{ ...account, weight: 4 }] });
    await api.setCredentialFields(account.name, { weight: 4 });
    expect(JSON.parse(String(call().options.body))).toEqual({
      name: account.name,
      weight: 4,
    });
  });
  it("rejects invalid account fields before requesting", async () => {
    await expect(
      api.setCredentialFields(account.name, { weight: -1 }),
    ).rejects.toThrow("Weight");
    await expect(
      api.setCredentialFields(account.name, { routing_tier: 1001 }),
    ).rejects.toThrow("Tier rank");
    await expect(
      api.setCredentialFields(account.name, {
        routing_weekly_reset_at: "2000-01-01T00:00:00Z",
      }),
    ).rejects.toThrow("future");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("Codex routing observations", () => {
  const profile = {
    tier_source: "plan",
    reset_source: "observed",
    banked_reset_expires_at: "2030-01-02T00:00:00Z",
    banked_reset_observed_at: "2030-01-01T00:00:00Z",
    quota_reserve_percent: 5,
    quota_reserve_blocked: false,
  };
  it("retains independent banked reset observations and a known unblocked cutoff", async () => {
    respond({
      files: [
        { ...account, routing_profile: { ...profile, access_token: "secret" } },
      ],
    });
    expect((await api.credentials())[0]?.routing_profile).toEqual(profile);
  });
  it("preserves missing observations as unknown", async () => {
    const unknown = {
      tier_source: "unknown",
      reset_source: "unknown",
      quota_reserve_percent: 5,
    };
    respond({ files: [{ ...account, routing_profile: unknown }] });
    expect((await api.credentials())[0]?.routing_profile).toEqual(unknown);
  });
  it.each([
    { banked_reset_expires_at: "invalid" },
    { banked_reset_observed_at: "invalid" },
    { banked_reset_expires_at: 42 },
    { quota_reserve_percent: -1 },
    { quota_reserve_percent: 101 },
    { quota_reserve_percent: "5" },
    { quota_reserve_blocked: "false" },
  ])("rejects malformed routing observations %j", async (invalid) => {
    respond({
      files: [{ ...account, routing_profile: { ...profile, ...invalid } }],
    });
    await expect(api.credentials()).rejects.toThrow();
  });
});
