import { useEffect, useRef, useState, type ReactNode } from "react";
import { motion } from "motion/react";
import { ArrowUpRight, Check, Copy, Network, X } from "lucide-react";
import type { Credential } from "./api";

export const providers: Record<
  string,
  { name: string; color: string; logo?: string }
> = {
  codex: { name: "OpenAI", color: "mint", logo: "openai" },
  openai: { name: "OpenAI", color: "mint", logo: "openai" },
  claude: { name: "Claude", color: "peach", logo: "claude" },
  antigravity: { name: "Antigravity", color: "blue", logo: "antigravity" },
  gemini: { name: "Gemini", color: "blue" },
  "gemini-cli": { name: "Gemini", color: "blue" },
  xai: { name: "Grok", color: "gray", logo: "xai" },
  grok: { name: "Grok", color: "gray", logo: "xai" },
  kimi: { name: "Kimi", color: "violet", logo: "kimi" },
  "kimi-ai": { name: "Kimi", color: "violet", logo: "kimi" },
  meta: { name: "Meta", color: "blue", logo: "meta" },
  devin: { name: "Devin", color: "blue" },
};

export function providerName(id: string) {
  return providers[id]?.name ?? id;
}

export function ProviderMark({
  provider,
  small = false,
}: {
  provider: string;
  small?: boolean;
}) {
  const info = providers[provider];
  return (
    <span
      className={`provider-mark ${info?.color ?? "gray"} ${small ? "small" : ""}`}
      aria-hidden="true"
    >
      {info?.logo ? (
        <img
          src={`${import.meta.env.BASE_URL}providers/${info.logo}.svg`}
          alt=""
        />
      ) : (
        <Network size={small ? 16 : 22} />
      )}
    </span>
  );
}

export function credentialState(item: Credential) {
  if (item.disabled) return { label: "Paused", tone: "muted" };
  if (item.unavailable || item.status === "error")
    return { label: "Needs attention", tone: "amber" };
  if (item.status === "active") return { label: "Available", tone: "green" };
  return { label: "Pending", tone: "muted" };
}

export function Status({
  label,
  tone = "green",
}: {
  label: string;
  tone?: string;
}) {
  return (
    <span className={`status ${tone}`}>
      <i />
      {label}
    </span>
  );
}

export function CopyButton({
  value,
  label = "Copy",
  compact = false,
}: {
  value: string;
  label?: string;
  compact?: boolean;
}) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setState("copied");
    } catch {
      setState("failed");
    }
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setState("idle"), 2500);
  }
  const text =
    state === "copied"
      ? "Copied"
      : state === "failed"
        ? "Select and copy manually"
        : label;
  return (
    <button
      className={`button ${compact ? "icon-button" : "secondary"}`}
      onClick={() => void copy()}
      aria-label={text}
      title={text}
    >
      {state === "copied" ? <Check size={16} /> : <Copy size={16} />}
      {!compact && <span>{text}</span>}
      <span className="sr-only" role="status">
        {state !== "idle" ? text : ""}
      </span>
    </button>
  );
}

export function Dialog({
  title,
  children,
  onClose,
  wide = false,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current!;
    const previous = document.activeElement as HTMLElement | null;
    dialog.showModal();
    return () => {
      dialog.close();
      previous?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className={`dialog ${wide ? "wide" : ""}`}
      aria-labelledby="dialog-title"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <motion.div
        className="dialog-surface"
        initial={{ opacity: 0, scale: 0.97, y: 8 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
      >
        <div className="dialog-heading">
          <h2 id="dialog-title">{title}</h2>
          <button
            className="button icon-button"
            aria-label="Close dialog"
            onClick={onClose}
          >
            <X size={19} />
          </button>
        </div>
        {children}
      </motion.div>
    </dialog>
  );
}

export function EmptyState({
  icon,
  title,
  children,
  action,
}: {
  icon: ReactNode;
  title: string;
  children: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <span className="empty-icon">{icon}</span>
      <h3>{title}</h3>
      <p>{children}</p>
      {action}
    </div>
  );
}

export function SectionHeading({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle?: string;
  action?: ReactNode;
}) {
  return (
    <div className="section-heading">
      <div>
        <h2>{title}</h2>
        {subtitle && <p>{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}

export function ExternalDocs() {
  return (
    <a
      className="text-link"
      href="https://help.router-for.me/"
      target="_blank"
      rel="noreferrer"
    >
      Documentation
      <ArrowUpRight size={15} />
    </a>
  );
}
