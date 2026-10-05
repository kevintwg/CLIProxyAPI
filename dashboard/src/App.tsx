import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { AnimatePresence, motion } from "motion/react";
import {
  ArrowRight,
  Boxes,
  Check,
  ChevronRight,
  Command,
  Eye,
  EyeOff,
  KeyRound,
  LayoutDashboard,
  Menu,
  Plus,
  RefreshCw,
  Settings2,
  ShieldCheck,
  Users,
  X,
} from "lucide-react";
import { ManagementApi, type Credential, type RoutingStrategy } from "./api";
import { Accounts } from "./Accounts";
import { ConnectAccount } from "./ConnectAccount";
import { Models } from "./Models";
import { Overview } from "./Overview";
import { Settings } from "./Settings";
import { Dialog, EmptyState, ExternalDocs, Status } from "./ui";

const pages = {
  overview: {
    title: "Overview",
    description: "Your AI connections, at a glance.",
    icon: LayoutDashboard,
  },
  accounts: {
    title: "Accounts",
    description: "A place for every provider. Control for every account.",
    icon: Users,
  },
  models: {
    title: "Models",
    description: "Find the right model for whatever comes next.",
    icon: Boxes,
  },
  settings: {
    title: "Settings",
    description: "Shared Relay preferences and provider account settings.",
    icon: Settings2,
  },
};
type Page = keyof typeof pages;
type Gateway = {
  api: ManagementApi;
  credentials: Credential[];
  strategy: RoutingStrategy;
};

export function App() {
  const [page, setPage] = useState<Page>("overview");
  const [gateway, setGateway] = useState<Gateway | null>(null);
  const [dialog, setDialog] = useState<"gateway" | "account" | null>(null);
  const [mobileNav, setMobileNav] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [toast, setToast] = useState("");
  const [updated, setUpdated] = useState<Date | null>(null);
  const refreshController = useRef<AbortController | null>(null);
  const refreshRevision = useRef(0);
  const toastTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const gatewayRef = useRef<Gateway | null>(gateway);
  gatewayRef.current = gateway;

  useEffect(
    () => () => {
      refreshController.current?.abort();
      clearTimeout(toastTimer.current);
    },
    [],
  );
  const notify = useCallback((message: string) => {
    setToast(message);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(""), 4500);
  }, []);

  function navigate(next: Page) {
    setPage(next);
    setMobileNav(false);
  }
  function disconnect() {
    refreshRevision.current++;
    refreshController.current?.abort();
    gatewayRef.current = null;
    setGateway(null);
    setLoading(false);
    setError("");
    setDialog(null);
    setUpdated(null);
    notify("Disconnected from the gateway");
  }
  async function connect(key: string, signal: AbortSignal) {
    const api = new ManagementApi(key.trim());
    const [credentials, strategy] = await Promise.all([
      api.credentials(signal),
      api.routing(signal),
    ]);
    if (signal.aborted) return;
    setGateway({ api, credentials, strategy });
    setError("");
    setDialog(null);
    setUpdated(new Date());
    notify("Gateway connected");
  }
  const refresh = useCallback(async () => {
    const current = gatewayRef.current;
    if (!current) return;
    refreshController.current?.abort();
    const controller = new AbortController();
    refreshController.current = controller;
    const revision = ++refreshRevision.current;
    setLoading(true);
    setError("");
    try {
      const [credentials, strategy] = await Promise.all([
        current.api.credentials(controller.signal),
        current.api.routing(controller.signal),
      ]);
      if (controller.signal.aborted || revision !== refreshRevision.current)
        return;
      setGateway({ api: current.api, credentials, strategy });
      setUpdated(new Date());
    } catch (reason) {
      if (controller.signal.aborted) return;
      const message =
        reason instanceof Error
          ? reason.message
          : "Could not refresh the gateway.";
      setError(message);
      throw reason;
    } finally {
      if (revision === refreshRevision.current) setLoading(false);
    }
  }, []);
  const info = pages[page];

  return (
    <div className="app-shell">
      <a href="#main" className="skip-link">
        Skip to content
      </a>
      {mobileNav && (
        <button
          className="nav-scrim"
          aria-label="Close navigation"
          onClick={() => setMobileNav(false)}
        />
      )}
      <aside className={`sidebar ${mobileNav ? "open" : ""}`}>
        <a
          className="brand"
          href="#"
          onClick={(event) => {
            event.preventDefault();
            navigate("overview");
          }}
          aria-label="Relay overview"
        >
          <span className="brand-mark">
            <Command size={23} strokeWidth={1.8} />
          </span>
          <span>
            Relay<small>CLIProxyAPI</small>
          </span>
        </a>
        <div className="workspace-label">
          <span className="workspace-orb">
            <ShieldCheck size={16} />
          </span>
          <span>Your workspace</span>
          <ChevronRight size={14} />
        </div>
        <nav aria-label="Main navigation">
          {Object.entries(pages).map(([id, value]) => {
            const Icon = value.icon;
            return (
              <button
                key={id}
                className={`nav-item ${page === id ? "active" : ""}`}
                aria-current={page === id ? "page" : undefined}
                onClick={() => navigate(id as Page)}
              >
                {page === id && (
                  <motion.span
                    className="nav-selection"
                    layoutId="nav-selection"
                  />
                )}
                <Icon size={19} />
                <span>{value.title}</span>
                {id === "accounts" && gateway && (
                  <small>{gateway.credentials.length}</small>
                )}
              </button>
            );
          })}
        </nav>
        <div className="sidebar-bottom">
          <ExternalDocs />
          <div className="gateway-status">
            <span className={`gateway-light ${gateway ? "connected" : ""}`} />
            <div>
              <strong>
                {gateway ? "Gateway connected" : "Gateway locked"}
              </strong>
              <small>{window.location.host}</small>
            </div>
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumb">
            <button
              className="button icon-button mobile-menu"
              aria-label="Open navigation"
              aria-expanded={mobileNav}
              onClick={() => setMobileNav((value) => !value)}
            >
              <Menu size={20} />
            </button>
            <span>Workspace</span>
            <ChevronRight size={13} />
            <strong>{info.title}</strong>
          </div>
          <div className="topbar-status">
            {gateway ? (
              <Status
                label={error ? "Refresh needed" : "Connected"}
                tone={error ? "amber" : "green"}
              />
            ) : (
              <span className="local-label">
                <span />
                Self-hosted gateway
              </span>
            )}
            <span className="avatar" aria-label="Your workspace">
              R
            </span>
          </div>
        </header>
        <main id="main" tabIndex={-1}>
          <div className="page-heading">
            <div>
              <h1>{info.title}</h1>
              <p>{info.description}</p>
            </div>
            <div className="page-actions">
              {gateway && (
                <button
                  className="button icon-button refresh-button"
                  aria-label="Refresh gateway"
                  title="Refresh gateway"
                  disabled={loading}
                  onClick={() => {
                    void refresh().catch(() => {});
                  }}
                >
                  <RefreshCw size={18} className={loading ? "spinning" : ""} />
                </button>
              )}
              <button
                className="button primary"
                onClick={() => setDialog(gateway ? "account" : "gateway")}
              >
                {gateway ? <Plus size={17} /> : <KeyRound size={16} />}
                {gateway ? "Connect account" : "Connect gateway"}
              </button>
            </div>
          </div>
          {error && (
            <div role="alert" className="error-banner">
              <strong>Could not refresh.</strong>
              <span>{error} Showing the last loaded data.</span>
              <button
                className="text-button"
                onClick={() => {
                  void refresh().catch(() => {});
                }}
              >
                Try again
              </button>
            </div>
          )}
          <div className="page-content" aria-busy={loading}>
            {page === "overview" ? (
              <Overview
                credentials={gateway?.credentials ?? []}
                strategy={gateway?.strategy ?? "round-robin"}
                connected={!!gateway}
                onConnect={() => setDialog("gateway")}
                onAdd={() => setDialog("account")}
                onAccounts={() => navigate("accounts")}
                onSettings={() => navigate("settings")}
              />
            ) : !gateway ? (
              <section className="panel">
                <EmptyState
                  icon={<KeyRound size={28} />}
                  title="Connect your gateway first"
                  action={
                    <button
                      className="button primary"
                      onClick={() => setDialog("gateway")}
                    >
                      Connect gateway
                      <ArrowRight size={16} />
                    </button>
                  }
                >
                  Enter your management key to view and manage{" "}
                  {page === "settings" ? "your preferences" : `your ${page}`}.
                </EmptyState>
              </section>
            ) : page === "accounts" ? (
              <Accounts
                credentials={gateway.credentials}
                api={gateway.api}
                onAdd={() => setDialog("account")}
                onRefresh={refresh}
                onRemoved={(name) => {
                  const current = gatewayRef.current;
                  if (!current || current.api !== gateway.api) return;
                  const next = {
                    ...current,
                    credentials: current.credentials.filter(
                      (item) => item.name !== name,
                    ),
                  };
                  gatewayRef.current = next;
                  setGateway(next);
                }}
                notify={notify}
              />
            ) : page === "models" ? (
              <Models credentials={gateway.credentials} api={gateway.api} />
            ) : (
              <Settings
                api={gateway.api}
                strategy={gateway.strategy}
                credentials={gateway.credentials}
                onRefresh={refresh}
                onDisconnect={disconnect}
                notify={notify}
              />
            )}
          </div>
          <footer className="page-footer">
            <span>
              <Command size={13} />
              Relay<span className="middot">·</span>Powered by CLIProxyAPI
            </span>
            <span>
              {loading
                ? "Refreshing…"
                : updated
                  ? `Updated at ${updated.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
                  : "Your providers. Your gateway."}
            </span>
          </footer>
        </main>
      </div>
      {dialog === "gateway" && (
        <GatewayDialog onConnect={connect} onClose={() => setDialog(null)} />
      )}
      {dialog === "account" && gateway && (
        <ConnectAccount
          api={gateway.api}
          onClose={() => setDialog(null)}
          onConnected={refresh}
          notify={notify}
        />
      )}
      <AnimatePresence>
        {toast && (
          <motion.div
            className="toast"
            role="status"
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 8 }}
          >
            <Check size={17} />
            <span>{toast}</span>
            <button
              aria-label="Dismiss notification"
              onClick={() => setToast("")}
            >
              <X size={14} />
            </button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function GatewayDialog({
  onConnect,
  onClose,
}: {
  onConnect: (key: string, signal: AbortSignal) => Promise<void>;
  onClose: () => void;
}) {
  const [key, setKey] = useState("");
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    const request = new AbortController();
    controller.current = request;
    try {
      await onConnect(key, request.signal);
    } catch (reason) {
      if (!request.signal.aborted)
        setError(
          reason instanceof Error
            ? reason.message
            : "Could not connect to the gateway.",
        );
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  }
  return (
    <Dialog title="Connect your gateway" onClose={onClose}>
      <p className="dialog-intro">
        Use the management key from your proxy configuration to open your
        workspace.
      </p>
      <form onSubmit={(event) => void submit(event)}>
        <label htmlFor="management-key">Management key</label>
        <div className="password-field">
          <input
            autoFocus
            id="management-key"
            type={visible ? "text" : "password"}
            value={key}
            onChange={(event) => setKey(event.target.value)}
            autoComplete="off"
            placeholder="Enter your management key"
            required
            disabled={busy}
          />
          <button
            type="button"
            className="button icon-button"
            aria-label={visible ? "Hide management key" : "Show management key"}
            onClick={() => setVisible((value) => !value)}
          >
            {visible ? <EyeOff size={18} /> : <Eye size={18} />}
          </button>
        </div>
        {error && (
          <p className="inline-error" role="alert">
            {error}
          </p>
        )}
        <div className="key-note">
          <ShieldCheck size={18} />
          <p>
            Your key stays in this tab's memory and is cleared when you reload
            or disconnect.
          </p>
        </div>
        <div className="dialog-actions">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className="button primary"
            disabled={busy || !key.trim()}
          >
            {busy ? "Connecting…" : "Connect"}
            <ArrowRight size={16} />
          </button>
        </div>
      </form>
    </Dialog>
  );
}
