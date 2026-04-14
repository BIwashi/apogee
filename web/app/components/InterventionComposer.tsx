"use client";

import {
  AlertTriangle,
  GitCommit,
  Layers,
  Octagon,
  Plus,
  RotateCcw,
  Send,
  Shuffle,
  X,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type ReactNode,
} from "react";

import type {
  Intervention,
  InterventionCreateRequest,
  InterventionMode,
  InterventionScope,
  InterventionUrgency,
} from "../lib/api-types";
import { apiUrl } from "../lib/api";

/**
 * InterventionComposer — keyboard-first form for pushing an operator
 * message into a live Claude Code session. Submits `POST /v1/interventions`
 * and hands the created row back through `onSubmitted`. The form sticks
 * mode/scope/urgency after a successful send so the operator can fire a
 * burst of messages without retyping their preferences.
 *
 * Visual envelope matches HITLPanel's rail-accent idiom: a coloured 3px
 * left border whose hue reflects the current urgency, and compact radio
 * groups driven entirely by lucide-react icons.
 */

const MAX_MESSAGE_CHARS = 4096;
const MIN_ROWS = 4;
const MAX_ROWS = 12;

export interface InterventionComposerProps {
  sessionId: string;
  turnId?: string | null;
  defaultMode?: InterventionMode;
  defaultScope?: InterventionScope;
  defaultUrgency?: InterventionUrgency;
  autoFocus?: boolean;
  onSubmitted?: (intervention: Intervention) => void;
  disabled?: boolean;
  disabledReason?: string;
  /** Optional notification hook for parents that want to react to scope flips. */
  onScopeChange?: (scope: InterventionScope) => void;
}

type ModeOption = {
  key: InterventionMode;
  label: string;
  help: string;
  icon: typeof Octagon;
};

type ScopeOption = {
  key: InterventionScope;
  label: string;
  help: string;
  icon: typeof GitCommit;
};

type UrgencyOption = {
  key: InterventionUrgency;
  label: string;
  help: string;
};

const MODE_OPTIONS: ModeOption[] = [
  {
    key: "interrupt",
    label: "interrupt",
    help: "Blocks the next tool call and shows the agent your message.",
    icon: Octagon,
  },
  {
    key: "context",
    label: "context",
    help: "Attaches the message as additional context to the agent's next prompt.",
    icon: Plus,
  },
  {
    key: "both",
    label: "both",
    help: "Tries interrupt first; falls back to context after 60s.",
    icon: Shuffle,
  },
];

const SCOPE_OPTIONS: ScopeOption[] = [
  {
    key: "this_turn",
    label: "this turn",
    help: "Message expires when this turn ends.",
    icon: GitCommit,
  },
  {
    key: "this_session",
    label: "this session",
    help: "Message stays queued until cancelled or delivered.",
    icon: Layers,
  },
];

const URGENCY_OPTIONS: UrgencyOption[] = [
  {
    key: "high",
    label: "high",
    help: "Turn is elevated to intervene_now while pending.",
  },
  {
    key: "normal",
    label: "normal",
    help: "Turn is elevated to watch while pending.",
  },
  {
    key: "low",
    label: "low",
    help: "No attention state change.",
  },
];

function urgencyBorder(urgency: InterventionUrgency): string {
  switch (urgency) {
    case "high":
      return "var(--status-critical)";
    case "normal":
      return "var(--status-warning)";
    case "low":
    default:
      return "var(--border-bright)";
  }
}

export default function InterventionComposer({
  sessionId,
  turnId,
  defaultMode = "interrupt",
  defaultScope,
  defaultUrgency = "normal",
  autoFocus = false,
  onSubmitted,
  disabled = false,
  disabledReason,
  onScopeChange,
}: InterventionComposerProps) {
  const textareaId = useId();
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  const initialScope: InterventionScope =
    defaultScope ?? (turnId ? "this_turn" : "this_session");

  const [message, setMessage] = useState("");
  const [mode, setMode] = useState<InterventionMode>(defaultMode);
  const [scope, setScope] = useState<InterventionScope>(initialScope);
  const [urgency, setUrgency] = useState<InterventionUrgency>(defaultUrgency);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // If the parent remounts the composer with a different turn, slide the
  // scope default back to `this_turn` so the operator's new context wins.
  useEffect(() => {
    setScope(turnId ? "this_turn" : "this_session");
  }, [turnId]);

  useEffect(() => {
    onScopeChange?.(scope);
  }, [scope, onScopeChange]);

  useEffect(() => {
    if (autoFocus && !disabled) {
      // next tick so the ref is attached after any parent layout shift.
      queueMicrotask(() => textareaRef.current?.focus());
    }
  }, [autoFocus, disabled]);

  const charCount = message.length;
  const overflow = charCount > MAX_MESSAGE_CHARS;

  const onTextChange = useCallback(
    (ev: ChangeEvent<HTMLTextAreaElement>) => {
      setMessage(ev.target.value);
      if (error) setError(null);
    },
    [error],
  );

  const buildRequest = useCallback((): InterventionCreateRequest => {
    const req: InterventionCreateRequest = {
      session_id: sessionId,
      message: message.trim(),
      delivery_mode: mode,
      scope,
      urgency,
    };
    if (scope === "this_turn" && turnId) {
      req.turn_id = turnId;
    }
    return req;
  }, [sessionId, turnId, message, mode, scope, urgency]);

  const canSubmit =
    !disabled && !overflow && message.trim().length > 0 && !submitting;

  const submit = useCallback(async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    try {
      const resp = await fetch(apiUrl("/v1/interventions"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(buildRequest()),
      });
      if (!resp.ok) {
        const text = await resp.text();
        throw new Error(`${resp.status}: ${text || resp.statusText}`);
      }
      const body = (await resp.json()) as
        | { intervention: Intervention }
        | Intervention;
      const intervention: Intervention =
        "intervention" in body ? body.intervention : body;
      setMessage("");
      onSubmitted?.(intervention);
      // Re-focus so bursts of interventions stay keyboard-driven.
      queueMicrotask(() => textareaRef.current?.focus());
    } catch (err) {
      const text = err instanceof Error ? err.message : "submit failed";
      setError(text);
    } finally {
      setSubmitting(false);
    }
  }, [canSubmit, buildRequest, onSubmitted]);

  const onKeyDown = useCallback(
    (ev: KeyboardEvent<HTMLTextAreaElement>) => {
      if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) {
        ev.preventDefault();
        void submit();
        return;
      }
      if (ev.key === "Escape") {
        ev.preventDefault();
        if (message.length > 0) {
          setMessage("");
          setError(null);
        }
      }
    },
    [submit, message.length],
  );

  const modeHelp = useMemo(
    () => MODE_OPTIONS.find((m) => m.key === mode)?.help ?? "",
    [mode],
  );
  const scopeHelp = useMemo(
    () => SCOPE_OPTIONS.find((s) => s.key === scope)?.help ?? "",
    [scope],
  );
  const urgencyHelp = useMemo(
    () => URGENCY_OPTIONS.find((u) => u.key === urgency)?.help ?? "",
    [urgency],
  );

  const borderColor = urgencyBorder(urgency);
  const placeholder = disabled
    ? disabledReason || "Composer disabled."
    : "Type a message for the agent. Ctrl/Cmd+Enter to send.";

  return (
    <div
      className="rounded-md border border-l-[3px] p-4"
      style={{
        background: "var(--bg-raised)",
        borderColor: "var(--border-bright)",
        borderLeftColor: borderColor,
      }}
      data-testid="intervention-composer"
    >
      <div className="flex items-center gap-2">
        <Send size={16} strokeWidth={1.5} color="var(--artemis-earth)" />
        <span
          className="font-display text-[10px] uppercase tracking-[0.16em]"
          style={{ color: "var(--artemis-earth)" }}
        >
          Send to agent
        </span>
        {disabled && (
          <span
            title={disabledReason}
            className="ml-auto inline-flex items-center gap-1 rounded border px-2 py-[2px] font-mono text-[10px]"
            style={{
              borderColor: "var(--border-bright)",
              color: "var(--text-muted)",
              background: "var(--bg-overlay)",
            }}
          >
            <AlertTriangle size={16} strokeWidth={1.5} />
            disabled
          </span>
        )}
      </div>

      <label htmlFor={textareaId} className="sr-only">
        Intervention message
      </label>
      <textarea
        id={textareaId}
        ref={textareaRef}
        rows={MIN_ROWS}
        value={message}
        onChange={onTextChange}
        onKeyDown={onKeyDown}
        disabled={disabled}
        placeholder={placeholder}
        title={disabled ? disabledReason : undefined}
        aria-label="Intervention message"
        className="mt-3 block w-full rounded border bg-transparent px-3 py-2 font-mono text-[12px] leading-snug text-gray-100 outline-none disabled:cursor-not-allowed disabled:opacity-60"
        style={{
          borderColor: overflow
            ? "var(--status-critical)"
            : "var(--border-bright)",
          resize: "none",
          minHeight: `${MIN_ROWS * 1.5}em`,
          maxHeight: `${MAX_ROWS * 1.5}em`,
          overflowY: "auto",
        }}
      />

      <div className="mt-1 flex items-center justify-between font-mono text-[10px] text-[var(--text-muted)]">
        <span>Ctrl/Cmd+Enter sends · Esc clears</span>
        <span
          style={{
            color: overflow ? "var(--status-critical)" : "var(--text-muted)",
          }}
        >
          {charCount} / {MAX_MESSAGE_CHARS}
        </span>
      </div>

      <div className="mt-4 grid gap-3 md:grid-cols-3">
        <RadioGroup<InterventionMode>
          label="Delivery mode"
          name="intervention-mode"
          value={mode}
          onChange={setMode}
          disabled={disabled}
          options={MODE_OPTIONS.map((opt) => ({
            value: opt.key,
            label: opt.label,
            icon: <opt.icon size={16} strokeWidth={1.5} />,
          }))}
          help={modeHelp}
        />
        <RadioGroup<InterventionScope>
          label="Scope"
          name="intervention-scope"
          value={scope}
          onChange={setScope}
          disabled={disabled}
          options={SCOPE_OPTIONS.map((opt) => ({
            value: opt.key,
            label: opt.label,
            icon: <opt.icon size={16} strokeWidth={1.5} />,
          }))}
          help={scopeHelp}
        />
        <RadioGroup<InterventionUrgency>
          label="Urgency"
          name="intervention-urgency"
          value={urgency}
          onChange={setUrgency}
          disabled={disabled}
          options={URGENCY_OPTIONS.map((opt) => ({
            value: opt.key,
            label: opt.label,
          }))}
          help={urgencyHelp}
        />
      </div>

      {error && (
        <div
          className="mt-3 flex items-center justify-between gap-2 rounded border px-3 py-2 font-mono text-[11px]"
          style={{
            borderColor: "var(--status-critical)",
            background: "rgb(252 61 33 / 0.08)",
            color: "var(--status-critical)",
          }}
        >
          <span className="flex items-center gap-2">
            <AlertTriangle size={16} strokeWidth={1.5} />
            {error}
          </span>
          <button
            type="button"
            onClick={() => void submit()}
            className="inline-flex items-center gap-1 rounded border px-2 py-[2px] text-[10px]"
            style={{
              borderColor: "var(--status-critical)",
              color: "var(--status-critical)",
            }}
          >
            <RotateCcw size={16} strokeWidth={1.5} /> Retry
          </button>
        </div>
      )}

      {!disabled && (
        <div className="mt-4 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={() => {
              setMessage("");
              setError(null);
            }}
            disabled={submitting || message.length === 0}
            className="inline-flex items-center gap-1 rounded border px-3 py-1 font-mono text-[11px] text-[var(--text-muted)] disabled:opacity-40"
            style={{
              borderColor: "var(--border-bright)",
              background: "var(--bg-overlay)",
            }}
          >
            <X size={16} strokeWidth={1.5} /> Cancel
          </button>
          <button
            type="button"
            onClick={() => void submit()}
            disabled={!canSubmit}
            className="inline-flex items-center gap-1 rounded px-3 py-1 font-mono text-[11px] font-medium text-black disabled:opacity-40"
            style={{ background: "var(--status-info)" }}
          >
            {submitting ? (
              <span
                aria-label="submitting"
                className="inline-block h-[10px] w-[10px] animate-spin rounded-full border border-black border-t-transparent"
              />
            ) : (
              <Send size={16} strokeWidth={1.5} />
            )}
            Send
          </button>
        </div>
      )}
    </div>
  );
}

interface RadioOption<T extends string> {
  value: T;
  label: string;
  icon?: ReactNode;
}

interface RadioGroupProps<T extends string> {
  label: string;
  name: string;
  value: T;
  onChange: (next: T) => void;
  options: RadioOption<T>[];
  help?: string;
  disabled?: boolean;
}

function RadioGroup<T extends string>({
  label,
  name,
  value,
  onChange,
  options,
  help,
  disabled = false,
}: RadioGroupProps<T>) {
  const selectedIndex = Math.max(
    0,
    options.findIndex((o) => o.value === value),
  );

  const onKeyDown = (ev: KeyboardEvent<HTMLDivElement>) => {
    if (disabled) return;
    if (ev.key === "ArrowRight" || ev.key === "ArrowDown") {
      ev.preventDefault();
      const next = options[(selectedIndex + 1) % options.length];
      if (next) onChange(next.value);
      return;
    }
    if (ev.key === "ArrowLeft" || ev.key === "ArrowUp") {
      ev.preventDefault();
      const next =
        options[(selectedIndex - 1 + options.length) % options.length];
      if (next) onChange(next.value);
    }
  };

  return (
    <div
      className="flex flex-col gap-1 rounded border p-2"
      style={{
        borderColor: "var(--border-bright)",
        background: "var(--bg-overlay)",
      }}
    >
      <span
        className="font-display text-[10px] uppercase tracking-[0.16em]"
        style={{ color: "var(--text-muted)" }}
      >
        {label}
      </span>
      <div
        role="radiogroup"
        aria-label={label}
        onKeyDown={onKeyDown}
        className="flex flex-col gap-1"
      >
        {options.map((opt) => {
          const selected = opt.value === value;
          return (
            <button
              key={opt.value}
              type="button"
              role="radio"
              aria-checked={selected}
              tabIndex={selected ? 0 : -1}
              disabled={disabled}
              onClick={() => onChange(opt.value)}
              className="flex items-center gap-2 rounded border px-2 py-[3px] text-left font-mono text-[11px] disabled:cursor-not-allowed disabled:opacity-50"
              style={{
                background: selected
                  ? "var(--bg-raised)"
                  : "transparent",
                borderColor: selected
                  ? "var(--artemis-earth)"
                  : "var(--border)",
                color: selected ? "#fff" : "var(--text-muted)",
              }}
            >
              <span
                aria-hidden
                className="inline-block h-[8px] w-[8px] rounded-full"
                style={{
                  background: selected
                    ? "var(--artemis-earth)"
                    : "transparent",
                  border: `1px solid ${
                    selected ? "var(--artemis-earth)" : "var(--border-bright)"
                  }`,
                }}
              />
              {opt.icon}
              {opt.label}
            </button>
          );
        })}
      </div>
      {help && (
        <p className="mt-1 font-mono text-[10px] leading-snug text-[var(--text-muted)]">
          {help}
        </p>
      )}
    </div>
  );
}
