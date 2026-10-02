import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { Accounts } from "./Accounts";
import { ConnectAccount } from "./ConnectAccount";
import { Models } from "./Models";
import { parseRouting } from "./routing";
import { Settings } from "./Settings";
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
    await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your draft is retained",
    );
    expect(notify).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
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
    expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
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
  expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
});
