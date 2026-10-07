import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { Accounts } from "./Accounts";
import { ConnectAccount } from "./ConnectAccount";
import { Models } from "./Models";
import { parseRouting } from "./routing";
import { Settings } from "./Settings";
import { Usage } from "./Usage";
import { ManagementApi, type Credential } from "./api";

const account: Credential = {
  id: "generated-account",
  name: "generated.json",
  provider: "claude",
  status: "active",
  disabled: false,
  unavailable: false,
  label: "Test account",
};

afterEach(() => vi.unstubAllGlobals());

it("connects a gateway whose persisted routing uses a supported alias", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.endsWith("/credentials")
              ? { files: [account] }
              : url.endsWith("/config/routing")
                ? { strategy: "ff" }
                : "ff",
          ),
        ),
    ),
  );
  render(<App />);
  await userEvent.click(
    screen.getAllByRole("button", { name: "Connect gateway" })[0],
  );
  await userEvent.type(
    screen.getByLabelText("Management key", { exact: true }),
    "test-key",
  );
  await userEvent.click(screen.getByRole("button", { name: "Connect" }));
  expect(
    await screen.findByText("Gateway connected", { selector: "strong" }),
  ).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Settings" }));
  expect(
    await screen.findByRole("radio", { name: /One account at a time/ }),
  ).toBeChecked();
});

describe("remembered sign-in and password changes", () => {
  const storageKey = "relay.managementKey";
  type Reply = { status?: number; body: unknown };
  function stubGateway(
    route: (url: string, init: RequestInit) => Reply | undefined = () =>
      undefined,
  ) {
    const fetchMock = vi.fn(async (url: string, init: RequestInit) => {
      const reply = route(url, init) ?? {
        body: url.endsWith("/credentials")
          ? { files: [account] }
          : url.endsWith("/config/routing")
            ? { strategy: "round-robin" }
            : "round-robin",
      };
      return new Response(JSON.stringify(reply.body), {
        status: reply.status ?? 200,
      });
    });
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }
  function bearer(init: RequestInit): string {
    return (init.headers as Record<string, string>).Authorization;
  }
  afterEach(() => window.localStorage.clear());

  it("remembers the key, signs in on reload, and forgets it on disconnect", async () => {
    const fetchMock = stubGateway();
    const first = render(<App />);
    await userEvent.click(
      screen.getAllByRole("button", { name: "Connect gateway" })[0],
    );
    await userEvent.type(
      screen.getByLabelText("Management key", { exact: true }),
      "saved-key",
    );
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Remember me on this device" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Connect" }));
    await screen.findByText("Gateway connected", { selector: "strong" });
    expect(window.localStorage.getItem(storageKey)).toBe("saved-key");
    first.unmount();

    fetchMock.mockClear();
    render(<App />);
    expect(
      await screen.findByText("Gateway connected", { selector: "strong" }),
    ).toBeInTheDocument();
    expect(bearer(fetchMock.mock.calls[0]![1])).toBe("Bearer saved-key");

    await userEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(await screen.findByText(/Saved on this device/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(window.localStorage.getItem(storageKey)).toBeNull();
    expect(
      screen.getByText("Gateway locked", { selector: "strong" }),
    ).toBeInTheDocument();
  });

  it("keeps an unremembered key out of storage", async () => {
    stubGateway();
    render(<App />);
    await userEvent.click(
      screen.getAllByRole("button", { name: "Connect gateway" })[0],
    );
    await userEvent.type(
      screen.getByLabelText("Management key", { exact: true }),
      "memory-key",
    );
    await userEvent.click(screen.getByRole("button", { name: "Connect" }));
    await screen.findByText("Gateway connected", { selector: "strong" });
    expect(window.localStorage.getItem(storageKey)).toBeNull();
  });

  it("clears a saved key the server rejects and asks to sign in again", async () => {
    window.localStorage.setItem(storageKey, "stale-key");
    stubGateway(() => ({
      status: 401,
      body: { error: "invalid management key" },
    }));
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your saved password was not accepted",
    );
    expect(window.localStorage.getItem(storageKey)).toBeNull();
    expect(
      screen.getByLabelText("Management key", { exact: true }),
    ).toHaveValue("");
  });

  async function openPasswordForm() {
    window.localStorage.setItem(storageKey, "old-password");
    render(<App />);
    await screen.findByText("Gateway connected", { selector: "strong" });
    await userEvent.click(screen.getByRole("button", { name: "Settings" }));
    await userEvent.type(
      await screen.findByLabelText("Current password"),
      "old-password",
    );
    await userEvent.type(screen.getByLabelText("New password"), "new-password");
  }

  it("changes the password and keeps the session on the new key", async () => {
    const fetchMock = stubGateway((url) =>
      url.endsWith("/password") ? { body: { status: "ok" } } : undefined,
    );
    await openPasswordForm();
    await userEvent.type(
      screen.getByLabelText("Confirm new password"),
      "new-passwrd",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Change password" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("do not match");
    expect(
      fetchMock.mock.calls.some(([url]) => url.endsWith("/password")),
    ).toBe(false);

    await userEvent.clear(screen.getByLabelText("Confirm new password"));
    await userEvent.type(
      screen.getByLabelText("Confirm new password"),
      "new-password",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Change password" }),
    );
    expect(
      await screen.findByText("Password changed", { selector: "span" }),
    ).toBeInTheDocument();
    const change = fetchMock.mock.calls.find(([url]) =>
      url.endsWith("/password"),
    )!;
    expect(change[1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(String(change[1].body))).toEqual({
      current_password: "old-password",
      new_password: "new-password",
    });
    expect(window.localStorage.getItem(storageKey)).toBe("new-password");
    expect(screen.getByLabelText("Current password")).toHaveValue("");

    fetchMock.mockClear();
    await userEvent.click(
      screen.getByRole("button", { name: "Refresh gateway" }),
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(bearer(fetchMock.mock.calls[0]![1])).toBe("Bearer new-password");
  });

  it("keeps the new key when an old-key refresh finishes after the change", async () => {
    let holdOldKey = false;
    const held: Array<() => void> = [];
    const fetchMock = vi.fn(async (url: string, init: RequestInit) => {
      const body = url.endsWith("/password")
        ? { status: "ok" }
        : url.endsWith("/credentials")
          ? { files: [account] }
          : url.endsWith("/config/routing")
            ? { strategy: "round-robin" }
            : "round-robin";
      if (
        holdOldKey &&
        !url.endsWith("/password") &&
        bearer(init) === "Bearer old-password"
      )
        await new Promise<void>((release) => held.push(release));
      return new Response(JSON.stringify(body));
    });
    vi.stubGlobal("fetch", fetchMock);
    await openPasswordForm();
    await userEvent.type(
      screen.getByLabelText("Confirm new password"),
      "new-password",
    );

    holdOldKey = true;
    await userEvent.click(
      screen.getByRole("button", { name: "Refresh gateway" }),
    );
    await waitFor(() => expect(held.length).toBeGreaterThan(0));
    await userEvent.click(
      screen.getByRole("button", { name: "Change password" }),
    );
    await screen.findByText("Password changed", { selector: "span" });
    held.splice(0).forEach((release) => release());
    await new Promise((resolve) => setTimeout(resolve, 0));

    holdOldKey = false;
    fetchMock.mockClear();
    await userEvent.click(
      screen.getByRole("button", { name: "Refresh gateway" }),
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(
      fetchMock.mock.calls.every(
        ([, init]) => bearer(init) === "Bearer new-password",
      ),
    ).toBe(true);
  });

  it("shows the server's reason when the password change is refused", async () => {
    stubGateway((url) =>
      url.endsWith("/password")
        ? { status: 403, body: { error: "Current password is incorrect." } }
        : undefined,
    );
    await openPasswordForm();
    await userEvent.type(
      screen.getByLabelText("Confirm new password"),
      "new-password",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Change password" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Current password is incorrect.",
    );
    expect(window.localStorage.getItem(storageKey)).toBe("old-password");
    expect(screen.getByLabelText("Current password")).toHaveValue(
      "old-password",
    );
  });
});

describe("account and routing controls", () => {
  it("shows a rejected pause without claiming the account is paused", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "setCredentialEnabled").mockRejectedValue(
      new Error("Account could not be saved"),
    );
    const notify = vi.fn();
    render(
      <Accounts
        api={api}
        credentials={[account]}
        onAdd={vi.fn()}
        onRefresh={vi.fn()}
        notify={notify}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Pause Test account" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Account could not be saved",
    );
    expect(notify).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Pause Test account" }),
    ).toBeEnabled();
  });

  it("refreshes persisted account state before reporting success", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "setCredentialEnabled").mockResolvedValue();
    const notify = vi.fn();
    let resolveRefresh: () => void = () => {};
    const refresh = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveRefresh = resolve;
        }),
    );
    render(
      <Accounts
        api={api}
        credentials={[account]}
        onAdd={vi.fn()}
        onRefresh={refresh}
        notify={notify}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Pause Test account" }),
    );
    expect(api.setCredentialEnabled).toHaveBeenCalledWith(account, false);
    expect(notify).not.toHaveBeenCalled();
    resolveRefresh();
    await waitFor(() => expect(notify).toHaveBeenCalledWith("Account paused"));
  });

  it("keeps routing edits unsaved when the server rejects a write", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "routingSettings").mockResolvedValue(parseRouting({}));
    vi.spyOn(api, "setRoutingSettings").mockRejectedValue(
      new Error("Could not persist configuration"),
    );
    const notify = vi.fn();
    render(
      <Settings
        api={api}
        strategy="round-robin"
        onRefresh={vi.fn()}
        onDisconnect={vi.fn()}
        notify={notify}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("radio", { name: /One account at a time/ }),
      ).toBeEnabled(),
    );
    await userEvent.click(
      screen.getByRole("radio", { name: /One account at a time/ }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save Relay settings" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your draft is retained",
    );
    expect(notify).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Save Relay settings" }),
    ).toBeEnabled();
  });
});

describe("model library", () => {
  it("combines shared models, excludes paused accounts, and filters by name", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "models").mockResolvedValue([
      { id: "shared-model" },
      { id: "other-model" },
    ]);
    render(
      <Models
        api={api}
        credentials={[
          account,
          { ...account, id: "second", name: "second.json", provider: "codex" },
          { ...account, id: "paused", name: "paused.json", disabled: true },
        ]}
      />,
    );
    expect(await screen.findByText("shared-model")).toBeInTheDocument();
    expect(api.models).toHaveBeenCalledTimes(2);
    expect(screen.getAllByText("Claude, OpenAI · 2 accounts")).toHaveLength(2);
    await userEvent.type(
      screen.getByRole("textbox", { name: "Search models" }),
      "shared",
    );
    expect(screen.queryByText("other-model")).not.toBeInTheDocument();
  });
  it("does not display a partial library as complete after a failed account lookup", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "models").mockRejectedValue(new Error("Gateway unavailable"));
    render(<Models api={api} credentials={[account]} />);
    expect(
      await screen.findByText("Models could not be loaded"),
    ).toBeInTheDocument();
    expect(screen.getByText("Gateway unavailable")).toBeInTheDocument();
    expect(screen.queryByText("0 models")).not.toBeInTheDocument();
  });
});

describe("provider sign-in", () => {
  it("displays and copies a device code while waiting for provider confirmation", async () => {
    const user = userEvent.setup();
    const copy = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async (url: string) =>
          new Response(
            JSON.stringify(
              url.includes("/oauth/auth-url")
                ? {
                    url: "https://example.invalid/activate",
                    state: "generated-state",
                    flow: "device",
                    user_code: "TEST-1234",
                  }
                : url.includes("/oauth/status")
                  ? { status: "wait" }
                  : { status: "ok" },
            ),
          ),
      ),
    );
    const connected = vi.fn();
    render(
      <ConnectAccount
        api={new ManagementApi("test-key")}
        onClose={vi.fn()}
        onConnected={connected}
        notify={vi.fn()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Grok" }));
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(await screen.findByText("TEST-1234")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open provider sign-in" }),
    ).toHaveAttribute("href", "https://example.invalid/activate");
    await user.click(screen.getByRole("button", { name: "Copy sign-in code" }));
    expect(copy).toHaveBeenCalledWith("TEST-1234");
    expect(connected).not.toHaveBeenCalled();
    expect(screen.queryByText("You're connected")).not.toBeInTheDocument();
    expect(
      screen.queryByText("Signing in from another computer?"),
    ).not.toBeInTheDocument();
  });

  it("cancels a pending login when the dialog is dismissed", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "startLogin").mockResolvedValue({
      state: "generated-state",
      url: "https://example.com/login",
    });
    vi.spyOn(api, "loginStatus").mockResolvedValue({ status: "wait" });
    vi.spyOn(api, "cancelLogin").mockResolvedValue();
    const { unmount } = render(
      <ConnectAccount
        api={api}
        onClose={vi.fn()}
        onConnected={vi.fn()}
        notify={vi.fn()}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(
      await screen.findByRole("link", { name: "Open provider sign-in" }),
    ).toHaveAttribute("href", "https://example.com/login");
    unmount();
    expect(api.cancelLogin).toHaveBeenCalledWith("generated-state");
  });
  it("reports success only after the provider confirms credential completion", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "startLogin").mockResolvedValue({
      state: "generated-state",
      url: "https://example.com/login",
    });
    vi.spyOn(api, "loginStatus").mockResolvedValue({ status: "ok" });
    vi.spyOn(api, "cancelLogin").mockResolvedValue();
    const connected = vi.fn().mockResolvedValue(undefined);
    const { unmount } = render(
      <ConnectAccount
        api={api}
        onClose={vi.fn()}
        onConnected={connected}
        notify={vi.fn()}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(await screen.findByText("You're connected")).toBeInTheDocument();
    expect(connected).toHaveBeenCalledOnce();
    unmount();
    expect(api.cancelLogin).not.toHaveBeenCalled();
  });
});

describe("account routing drafts", () => {
  it("retains a failed account draft independently of another saved account", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "routingSettings").mockResolvedValue(
      parseRouting({ strategy: "weighted-round-robin" }),
    );
    vi.spyOn(api, "setCredentialFields")
      .mockRejectedValueOnce(new Error("write failed"))
      .mockResolvedValueOnce({
        ...account,
        name: "second.json",
        id: "second",
        weight: 3,
      });
    const notify = vi.fn();
    render(
      <Settings
        api={api}
        strategy="weighted-round-robin"
        credentials={[
          account,
          {
            ...account,
            id: "second",
            name: "second.json",
            label: "Second account",
          },
        ]}
        onRefresh={vi.fn().mockResolvedValue(undefined)}
        onDisconnect={vi.fn()}
        notify={notify}
      />,
    );
    await userEvent.click(screen.getByRole("tab", { name: /Claude/ }));
    const weights = await screen.findAllByLabelText(
      "Weight (zero skips this account)",
    );
    await userEvent.clear(weights[0]!);
    await userEvent.type(weights[0]!, "2");
    await userEvent.click(
      screen.getAllByRole("button", { name: "Save account" })[0]!,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your draft is retained",
    );
    await userEvent.clear(weights[1]!);
    await userEvent.type(weights[1]!, "3");
    await userEvent.click(
      screen.getAllByRole("button", { name: "Save account" })[1]!,
    );
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith("Account routing saved"),
    );
    expect(weights[0]).toHaveValue(2);
    expect(
      screen.getAllByRole("button", { name: "Save account" })[0],
    ).toBeEnabled();
  });
  it("keeps unsaved global edits across a credential refresh", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "routingSettings").mockResolvedValue(parseRouting({}));
    const props = {
      api,
      strategy: "round-robin" as const,
      credentials: [account],
      onRefresh: vi.fn(),
      onDisconnect: vi.fn(),
      notify: vi.fn(),
    };
    const view = render(<Settings {...props} />);
    await waitFor(() =>
      expect(
        screen.getByRole("radio", { name: /Use subscription order/ }),
      ).toBeEnabled(),
    );
    await userEvent.click(
      screen.getByRole("radio", { name: /Use subscription order/ }),
    );
    view.rerender(
      <Settings
        {...props}
        strategy="fill-first"
        credentials={[{ ...account, weight: 8 }]}
      />,
    );
    expect(
      screen.getByRole("radio", { name: /Use subscription order/ }),
    ).toBeChecked();
    expect(
      screen.getByRole("button", { name: "Save Relay settings" }),
    ).toBeEnabled();
  });
});

it("refreshes global routing settings when there are no unsaved edits", async () => {
  const api = new ManagementApi("test-key");
  vi.spyOn(api, "routingSettings")
    .mockResolvedValueOnce(parseRouting({}))
    .mockResolvedValueOnce(parseRouting({ strategy: "subscription-first" }));
  const props = {
    api,
    strategy: "round-robin" as const,
    credentials: [account],
    onRefresh: vi.fn(),
    onDisconnect: vi.fn(),
    notify: vi.fn(),
  };
  const view = render(<Settings {...props} />);
  await waitFor(() =>
    expect(screen.getByRole("radio", { name: /Share the work/ })).toBeEnabled(),
  );
  view.rerender(<Settings {...props} credentials={[{ ...account }]} />);
  await waitFor(() =>
    expect(
      screen.getByRole("radio", { name: /Use subscription order/ }),
    ).toBeChecked(),
  );
  expect(
    screen.getByRole("button", { name: "Save Relay settings" }),
  ).toBeDisabled();
});

it("displays omitted retry values as zero and saves explicit rounds and wait", async () => {
  const api = new ManagementApi("test-key");
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(JSON.stringify({}))),
  );
  const saved = parseRouting({
    retry: { "request-retry": 3, "max-retry-interval": 30 },
  });
  vi.spyOn(api, "setRoutingSettings").mockResolvedValue(saved);
  render(
    <Settings
      api={api}
      strategy="round-robin"
      onRefresh={vi.fn().mockResolvedValue(undefined)}
      onDisconnect={vi.fn()}
      notify={vi.fn()}
    />,
  );
  await waitFor(() =>
    expect(screen.getByRole("radio", { name: /Share the work/ })).toBeEnabled(),
  );
  await userEvent.click(
    screen.getByText("Retry options", { selector: "summary" }),
  );
  const rounds = screen.getByLabelText("Retry rounds");
  const interval = screen.getByLabelText("Maximum retry wait (seconds)");
  expect(rounds).toHaveValue(0);
  expect(interval).toHaveValue(0);
  await userEvent.clear(rounds);
  await userEvent.type(rounds, "3");
  await userEvent.clear(interval);
  await userEvent.type(interval, "30");
  await userEvent.click(
    screen.getByRole("button", { name: "Save Relay settings" }),
  );
  await waitFor(() =>
    expect(api.setRoutingSettings).toHaveBeenCalledWith({
      retry: { "request-retry": 3, "max-retry-interval": 30 },
    }),
  );
});

it("separates providers, supports tab keys and retains drafts and scoped errors", async () => {
  const api = new ManagementApi("test-key");
  vi.spyOn(api, "routingSettings").mockResolvedValue(parseRouting({}));
  const saveAccount = vi
    .spyOn(api, "setCredentialFields")
    .mockRejectedValue(new Error("account write failed"));
  const saveRelay = vi.spyOn(api, "setRoutingSettings");
  const codex = {
    ...account,
    name: "codex.json",
    id: "codex",
    provider: "codex",
    label: "Codex fixture",
  };
  const props = {
    api,
    strategy: "subscription-first" as const,
    credentials: [account, codex],
    onRefresh: vi.fn(),
    onDisconnect: vi.fn(),
    notify: vi.fn(),
  };
  const view = render(<Settings {...props} />);
  const relayTab = screen.getByRole("tab", { name: "Relay" });
  await waitFor(() =>
    expect(
      screen.getByRole("radio", { name: /Use subscription order/ }),
    ).toBeEnabled(),
  );
  await userEvent.click(
    screen.getByRole("radio", { name: /Use subscription order/ }),
  );
  await userEvent.click(
    screen.getByLabelText("Keep a conversation on the same account", {
      exact: true,
    }),
  );
  relayTab.focus();
  await userEvent.keyboard("{ArrowRight}");
  expect(screen.getByRole("tab", { name: /Codex/ })).toHaveFocus();
  expect(screen.getByRole("tab", { name: /Codex/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  expect(screen.getByRole("heading", { name: "Codex fixture" })).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "Test account" }),
  ).not.toBeInTheDocument();
  expect(
    within(screen.getByRole("tabpanel", { name: /Codex/ })).getByText(
      /unsaved strategy/,
    ),
  ).toBeVisible();
  const rank = within(
    screen.getByRole("tabpanel", { name: /Codex/ }),
  ).getByLabelText("Tier rank override");
  await userEvent.type(rank, "2");
  await userEvent.click(screen.getByRole("button", { name: "Save account" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "account write failed",
  );
  expect(saveAccount).toHaveBeenCalledWith("codex.json", { routing_tier: 2 });
  expect(saveRelay).not.toHaveBeenCalled();
  screen.getByRole("tab", { name: /Codex/ }).focus();
  await userEvent.keyboard("{End}");
  expect(screen.getByRole("tab", { name: /Claude/ })).toHaveFocus();
  expect(
    within(screen.getByRole("tabpanel")).getByLabelText("Tier rank override"),
  ).toHaveValue(null);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(
    within(screen.getByRole("tabpanel", { name: /Claude/ })).getByText(
      "Unknown, used last",
      { exact: false },
    ),
  ).toBeVisible();
  view.rerender(
    <Settings
      {...props}
      credentials={[{ ...account }, { ...codex, routing_tier: 9 }]}
    />,
  );
  await userEvent.click(screen.getByRole("tab", { name: /Codex/ }));
  expect(rank).toHaveValue(2);
  expect(screen.getByRole("alert")).toHaveTextContent("Your draft is retained");
  screen.getByRole("tab", { name: /Codex/ }).focus();
  await userEvent.keyboard("{Home}");
  expect(relayTab).toHaveFocus();
  expect(
    screen.getByRole("button", { name: "Save Relay settings" }),
  ).toBeEnabled();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await userEvent.keyboard("{ArrowLeft}");
  expect(screen.getByRole("tab", { name: /Claude/ })).toHaveFocus();
});

it("keeps other providers reachable and runtime-only account fields disabled", async () => {
  const api = new ManagementApi("test-key");
  vi.spyOn(api, "routingSettings").mockResolvedValue(
    parseRouting({ strategy: "subscription-first" }),
  );
  render(
    <Settings
      api={api}
      strategy="subscription-first"
      credentials={[
        {
          ...account,
          provider: "gemini",
          runtime_only: true,
          routing_profile: {
            tier_source: "unknown",
            reset_source: "observed",
            weekly_reset_at: "2000-01-01T00:00:00Z",
            observed_at: "2000-01-01T00:00:00Z",
          },
        },
      ]}
      onRefresh={vi.fn()}
      onDisconnect={vi.fn()}
      notify={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("tab", { name: /Other accounts/ }));
  expect(
    within(screen.getByRole("tabpanel")).getByLabelText("Tier rank override"),
  ).toBeDisabled();
  expect(
    within(screen.getByRole("tabpanel")).getByLabelText(
      "Weekly reset override",
    ),
  ).toBeDisabled();
  expect(screen.getByRole("button", { name: "Save account" })).toBeDisabled();
  expect(screen.getByText(/No usable future reset/)).toBeVisible();
});

it("distinguishes stale provider resets from usable manual resets", async () => {
  vi.spyOn(Date, "now").mockReturnValue(Date.parse("2030-01-01T00:00:00Z"));
  const api = new ManagementApi("test-key");
  vi.spyOn(api, "routingSettings").mockResolvedValue(
    parseRouting({ strategy: "subscription-first" }),
  );
  render(
    <Settings
      api={api}
      strategy="subscription-first"
      credentials={[
        {
          ...account,
          routing_profile: {
            tier: 2,
            tier_source: "manual",
            reset_source: "observed",
            weekly_reset_at: "2030-01-02T00:00:00Z",
            observed_at: "2029-01-01T00:00:00Z",
          },
        },
        {
          ...account,
          name: "manual.json",
          id: "manual",
          label: "Manual reset fixture",
          routing_profile: {
            tier: 2,
            tier_source: "manual",
            reset_source: "manual",
            weekly_reset_at: "2030-01-02T00:00:00Z",
          },
        },
      ]}
      onRefresh={vi.fn()}
      onDisconnect={vi.fn()}
      notify={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("tab", { name: /Claude/ }));
  expect(
    within(
      screen.getByRole("region", { name: "Test account routing" }),
    ).getByText(/Stale, ignored/),
  ).toBeVisible();
  expect(
    within(
      screen.getByRole("region", { name: "Manual reset fixture routing" }),
    ).getByText("Usable for ordering"),
  ).toBeVisible();
});

it("uses banked reset freshness independently and distinguishes cutoff states", async () => {
  vi.spyOn(Date, "now").mockReturnValue(Date.parse("2030-01-01T00:00:00Z"));
  const api = new ManagementApi("test-key");
  vi.spyOn(api, "routingSettings").mockResolvedValue(
    parseRouting({ strategy: "subscription-first" }),
  );
  const common = {
    ...account,
    provider: "codex",
    routing_profile: {
      tier_source: "plan" as const,
      reset_source: "manual" as const,
      weekly_reset_at: "2030-01-02T00:00:00Z",
      banked_reset_expires_at: "2030-01-02T00:00:00Z",
      banked_reset_observed_at: "2029-01-01T00:00:00Z",
      quota_reserve_percent: 5,
      quota_reserve_blocked: true,
    },
  };
  render(
    <Settings
      api={api}
      strategy="subscription-first"
      credentials={[
        {
          ...common,
          id: "blocked",
          name: "blocked.json",
          label: "Blocked fixture",
        },
        {
          ...common,
          id: "available",
          name: "available.json",
          label: "Available fixture",
          routing_profile: {
            ...common.routing_profile,
            banked_reset_observed_at: "2029-12-31T23:59:00Z",
            quota_reserve_blocked: false,
          },
        },
        {
          ...common,
          id: "unknown",
          name: "unknown.json",
          label: "Unknown fixture",
          routing_profile: {
            tier_source: "unknown",
            reset_source: "unknown",
            quota_reserve_percent: 5,
          },
        },
      ]}
      onRefresh={vi.fn()}
      onDisconnect={vi.fn()}
      notify={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("tab", { name: /Codex/ }));
  const blocked = within(
    screen.getByRole("region", { name: "Blocked fixture routing" }),
  );
  expect(blocked.getByText("Blocked at 5% remaining or less")).toBeVisible();
  expect(blocked.getByText("Stale, ignored")).toBeVisible();
  expect(blocked.getByText("Usable for ordering")).toBeVisible();
  const available = within(
    screen.getByRole("region", { name: "Available fixture routing" }),
  );
  expect(available.getByText("Above the 5% cutoff")).toBeVisible();
  expect(available.getAllByText("Usable for ordering")).toHaveLength(2);
  const unknown = within(
    screen.getByRole("region", { name: "Unknown fixture routing" }),
  );
  expect(unknown.getByText("Quota state unknown")).toBeVisible();
  expect(unknown.getByText("No usable banked reset")).toBeVisible();
  expect(screen.getAllByLabelText("Tier rank override")).toHaveLength(3);
  expect(screen.getAllByLabelText("Weekly reset override")).toHaveLength(3);
  expect(
    screen.queryByRole("spinbutton", { name: /cutoff/i }),
  ).not.toBeInTheDocument();
});

describe("account removal", () => {
  function setup(
    api: ManagementApi,
    refresh = vi.fn().mockResolvedValue(undefined),
  ) {
    const removed = vi.fn();
    const notify = vi.fn();
    render(
      <Accounts
        api={api}
        credentials={[account]}
        onAdd={vi.fn()}
        onRefresh={refresh}
        onRemoved={removed}
        notify={notify}
      />,
    );
    return { removed, notify };
  }
  it("names the account and cancels without deleting", async () => {
    const api = new ManagementApi("test-key");
    const remove = vi.spyOn(api, "removeCredential");
    setup(api);
    await userEvent.click(
      screen.getByRole("button", { name: "Remove Test account" }),
    );
    const dialog = screen.getByRole("dialog", { name: "Remove account?" });
    expect(dialog).toHaveTextContent("Test account");
    expect(dialog).toHaveTextContent("sign in again");
    expect(
      within(dialog).getByRole("button", { name: "Cancel" }),
    ).toHaveFocus();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Cancel" }),
    );
    expect(remove).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("keeps a failed removal visible and allows retry", async () => {
    const api = new ManagementApi("test-key");
    vi.spyOn(api, "removeCredential").mockRejectedValue(
      new Error("Delete failed"),
    );
    const { removed, notify } = setup(api);
    await userEvent.click(
      screen.getByRole("button", { name: "Remove Test account" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Remove account" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Delete failed");
    expect(removed).not.toHaveBeenCalled();
    expect(notify).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Remove account" }),
    ).toBeEnabled();
  });
  it("blocks duplicate deletion and distinguishes refresh failure", async () => {
    const api = new ManagementApi("test-key");
    let finish: () => void = () => {};
    vi.spyOn(api, "removeCredential").mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const { removed, notify } = setup(
      api,
      vi.fn().mockRejectedValue(new Error("Offline")),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Remove Test account" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Remove account" }),
    );
    expect(screen.getByRole("button", { name: /Removing/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(removed).not.toHaveBeenCalled();
    finish();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Account removed, but the gateway could not refresh",
    );
    expect(api.removeCredential).toHaveBeenCalledTimes(1);
    expect(removed).toHaveBeenCalledWith(account.name);
    expect(notify).toHaveBeenCalledWith("Account removed");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});

it("does not remove siblings that share a saved connection even when search hides one", async () => {
  const api = new ManagementApi("test-key");
  const remove = vi.spyOn(api, "removeCredential");
  render(
    <Accounts
      api={api}
      credentials={[
        account,
        { ...account, id: "sibling", label: "Other account" },
      ]}
      onAdd={vi.fn()}
      onRefresh={vi.fn()}
      notify={vi.fn()}
    />,
  );
  await userEvent.type(
    screen.getByLabelText("Search accounts"),
    "Test account",
  );
  expect(
    screen.getByRole("button", { name: "Remove Test account" }),
  ).toBeDisabled();
  expect(screen.getByText(/cannot be removed separately/)).toBeInTheDocument();
  expect(remove).not.toHaveBeenCalled();
});

it("does not offer removal for a live-session account without saved credentials", async () => {
  const api = new ManagementApi("test-key");
  const remove = vi.spyOn(api, "removeCredential");
  render(
    <Accounts
      api={api}
      credentials={[{ ...account, runtime_only: true }]}
      onAdd={vi.fn()}
      onRefresh={vi.fn()}
      notify={vi.fn()}
    />,
  );
  const button = screen.getByRole("button", { name: "Remove Test account" });
  expect(button).toBeDisabled();
  expect(
    screen.getByText("This account has no saved connection to remove."),
  ).toBeInTheDocument();
  await userEvent.click(button);
  expect(remove).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("shows usage meters and confirms reset redemption before calling the API", async () => {
  const api = new ManagementApi("test-key");
  const fetchUsage = vi.spyOn(api, "fetchUsage").mockResolvedValue({
    plan: "pro",
    banked_reset_count: 1,
    primary: {
      used_percent: 40,
      remaining_percent: 60,
      window_minutes: 300,
      resets_at: "2030-01-01T05:00:00Z",
      observed_at: "2030-01-01T00:00:00Z",
    },
  });
  const redeemReset = vi.spyOn(api, "redeemReset").mockResolvedValue({
    banked_reset_count: 0,
  });
  const notify = vi.fn();
  render(
    <Usage
      api={api}
      credentials={[
        {
          ...account,
          provider: "codex",
          supports_reset: true,
          usage_limits: {
            plan: "pro",
            banked_reset_count: 1,
            primary: {
              used_percent: 40,
              remaining_percent: 60,
              window_minutes: 300,
              resets_at: "2030-01-01T05:00:00Z",
              observed_at: "2030-01-01T00:00:00Z",
            },
          },
        },
      ]}
      onRefresh={vi.fn().mockResolvedValue(undefined)}
      notify={notify}
    />,
  );
  expect(screen.getByText("40% used")).toBeInTheDocument();
  expect(screen.getByText("1 available")).toBeInTheDocument();
  await userEvent.click(
    screen.getByRole("button", { name: "Fetch usage for Test account" }),
  );
  await waitFor(() =>
    expect(fetchUsage).toHaveBeenCalledWith(
      expect.objectContaining({ provider: "codex" }),
    ),
  );
  await userEvent.click(screen.getByRole("button", { name: "Redeem reset" }));
  expect(screen.getByRole("dialog")).toHaveTextContent(
    "Redeem a banked reset?",
  );
  expect(redeemReset).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Redeem reset" }));
  await userEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Redeem reset",
    }),
  );
  await waitFor(() => expect(redeemReset).toHaveBeenCalled());
  expect(notify).toHaveBeenCalledWith("Reset redeemed for Test account");
});

it("keeps a completed redemption successful when the inventory refresh fails", async () => {
  const api = new ManagementApi("test-key");
  vi.spyOn(api, "redeemReset").mockResolvedValue({ banked_reset_count: 0 });
  const notify = vi.fn();
  render(
    <Usage
      api={api}
      credentials={[
        {
          ...account,
          provider: "codex",
          supports_reset: true,
          usage_limits: { banked_reset_count: 1 },
        },
      ]}
      onRefresh={vi.fn().mockRejectedValue(new Error("gateway unavailable"))}
      notify={notify}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Redeem reset" }));
  await userEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Redeem reset",
    }),
  );
  await waitFor(() =>
    expect(notify).toHaveBeenCalledWith("Reset redeemed for Test account"),
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByRole("alert")).toHaveTextContent("could not be refreshed");
});

it("rechecks runtime-only status when the account inventory changes during confirmation", async () => {
  const api = new ManagementApi("test-key");
  const remove = vi.spyOn(api, "removeCredential");
  const props = { api, onAdd: vi.fn(), onRefresh: vi.fn(), notify: vi.fn() };
  const view = render(<Accounts {...props} credentials={[account]} />);
  await userEvent.click(
    screen.getByRole("button", { name: "Remove Test account" }),
  );
  view.rerender(
    <Accounts {...props} credentials={[{ ...account, runtime_only: true }]} />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Remove account" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This account has no saved connection to remove.",
  );
  expect(remove).not.toHaveBeenCalled();
});
