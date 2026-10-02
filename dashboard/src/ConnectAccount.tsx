import { useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  ExternalLink,
  LoaderCircle,
} from "lucide-react";
import { ManagementApi, type LoginSession } from "./api";
import { Dialog, ProviderMark, providerName } from "./ui";

const loginProviders = [
  "codex",
  "claude",
  "antigravity",
  "xai",
  "kimi",
  "meta",
  "devin",
];

export function ConnectAccount({
  api,
  onClose,
  onConnected,
  notify,
}: {
  api: ManagementApi;
  onClose: () => void;
  onConnected: () => Promise<void>;
  notify: (message: string) => void;
}) {
  const [provider, setProvider] = useState("codex");
  const [session, setSession] = useState<LoginSession | null>(null);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");
  const [callback, setCallback] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [callbackSubmitted, setCallbackSubmitted] = useState(false);
  const [done, setDone] = useState(false);
  const [pollRevision, setPollRevision] = useState(0);
  const pending = useRef<string | null>(null);
  const startingController = useRef<AbortController | null>(null);
  const onConnectedRef = useRef(onConnected);
  const notifyRef = useRef(notify);
  onConnectedRef.current = onConnected;
  notifyRef.current = notify;

  useEffect(
    () => () => {
      startingController.current?.abort();
      if (pending.current)
        void api
          .cancelLogin(pending.current)
          .catch(() =>
            notifyRef.current(
              "Sign-in could not be cancelled on the server. Its pending session will expire.",
            ),
          );
    },
    [api],
  );

  useEffect(() => {
    if (!session) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const deadline = Date.now() + 5 * 60 * 1000;
    async function poll() {
      try {
        const result = await api.loginStatus(session!.state, controller.signal);
        if (controller.signal.aborted) return;
        if (result.status === "ok") {
          pending.current = null;
          setDone(true);
          try {
            await onConnectedRef.current();
          } catch {
            notifyRef.current(
              "Account connected. Refresh the account list to see it.",
            );
          }
          return;
        }
        if (result.status === "error") {
          setError(
            result.error ||
              "Sign-in was not completed. Close this window and try again.",
          );
          return;
        }
        if (Date.now() >= deadline) {
          setError(
            "Still waiting for sign-in. You can check again, or close this window to cancel.",
          );
          return;
        }
        timer = setTimeout(() => void poll(), 1800);
      } catch (reason) {
        if (!controller.signal.aborted)
          setError(
            reason instanceof Error
              ? reason.message
              : "Could not check sign-in status.",
          );
      }
    }
    void poll();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [api, session, pollRevision]);

  async function start() {
    setStarting(true);
    setError("");
    const controller = new AbortController();
    startingController.current = controller;
    try {
      const next = await api.startLogin(provider, controller.signal);
      if (controller.signal.aborted) {
        void api.cancelLogin(next.state);
        return;
      }
      pending.current = next.state;
      setSession(next);
    } catch (reason) {
      if (!controller.signal.aborted)
        setError(
          reason instanceof Error ? reason.message : "Could not start sign-in.",
        );
    } finally {
      if (!controller.signal.aborted) setStarting(false);
    }
  }

  async function submitCallback() {
    setSubmitting(true);
    setError("");
    try {
      await api.submitCallback(provider, callback.trim());
      setCallback("");
      setCallbackSubmitted(true);
      setPollRevision((value) => value + 1);
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : "Could not submit the callback.",
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog
      title={
        done
          ? "You're connected"
          : session
            ? `Sign in to ${providerName(provider)}`
            : "Connect an account"
      }
      onClose={onClose}
    >
      {done ? (
        <div className="login-success">
          <span>
            <Check size={28} />
          </span>
          <h3>One more connection. More possibilities.</h3>
          <p>
            Your account is connected to the gateway. Its available models will
            appear in your library.
          </p>
          <button className="button primary" onClick={onClose}>
            Done
            <Check size={16} />
          </button>
        </div>
      ) : session ? (
        <div className="login-session">
          <ProviderMark provider={provider} />
          <p>Continue in a new tab to securely sign in with your provider.</p>
          <a
            className="button primary"
            href={session.url}
            target="_blank"
            rel="noreferrer"
          >
            Open provider sign-in
            <ExternalLink size={16} />
          </a>
          <div className="waiting-status" role="status">
            <LoaderCircle className={error ? "" : "spinning"} size={17} />
            {error ? "Sign-in needs attention" : "Waiting for your provider…"}
          </div>
          <details>
            <summary>Signing in from another computer?</summary>
            <p>
              If the final callback page does not load, copy its full URL from
              the address bar and paste it here.
            </p>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void submitCallback();
              }}
            >
              <label htmlFor="callback-url">Callback URL</label>
              <input
                id="callback-url"
                type="url"
                required
                value={callback}
                onChange={(event) => setCallback(event.target.value)}
                placeholder="http://localhost:…"
                autoComplete="off"
              />
              <button
                className="button secondary"
                disabled={submitting || !callback.trim()}
              >
                {submitting ? "Submitting…" : "Complete sign-in"}
              </button>
            </form>
            {callbackSubmitted && (
              <p role="status">
                Callback received. Waiting for the account to finish connecting.
              </p>
            )}
          </details>
          {error && (
            <div>
              <p role="alert" className="inline-error">
                {error}
              </p>
              <button
                className="text-button"
                onClick={() => {
                  setError("");
                  setPollRevision((value) => value + 1);
                }}
              >
                Check again
                <ArrowRight size={14} />
              </button>
            </div>
          )}
        </div>
      ) : (
        <>
          <p className="dialog-intro">
            Choose a provider. You'll sign in with your existing account.
          </p>
          <div
            className="provider-choices"
            role="group"
            aria-label="Choose a provider"
          >
            {loginProviders.map((id) => (
              <button
                key={id}
                className={`provider-choice ${provider === id ? "selected" : ""}`}
                aria-pressed={provider === id}
                disabled={starting}
                onClick={() => setProvider(id)}
              >
                <ProviderMark provider={id} small />
                <span>{providerName(id)}</span>
                <span className="radio-visual">
                  {provider === id && <Check size={12} />}
                </span>
              </button>
            ))}
          </div>
          {error && (
            <p role="alert" className="inline-error">
              {error}
            </p>
          )}
          <p className="dialog-footnote">
            Model access depends on your provider and plan.
          </p>
          <div className="dialog-actions">
            <button className="button secondary" onClick={onClose}>
              <ArrowLeft size={15} />
              Cancel
            </button>
            <button
              className="button primary"
              onClick={() => void start()}
              disabled={starting}
            >
              {starting ? "Preparing sign-in…" : "Continue"}
              <ArrowRight size={16} />
            </button>
          </div>
        </>
      )}
    </Dialog>
  );
}
