"use client";

import { Check, Clock, ShieldAlert, Slash, X } from "lucide-react";
import {
  useCallback,
  useMemo,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";

import type {
  HITLDecision,
  HITLEvent,
  HITLReason,
  HITLResponseInput,
  HITLResumeMode,
} from "../lib/api-types";
import { HITL_REASONS, HITL_RESUME_MODES } from "../lib/api-types";
import { apiUrl } from "../lib/api";
import { timeAgo } from "../lib/time";
import Card from "./Card";

/**
 * HITLPanel — the always-visible top-right card on the turn detail page
 * that surfaces every pending Human-In-The-Loop request and lets the
 * operator respond inline. Each pending row gets its own form so the
 * operator can choose Allow/Deny, pick a reason category, attach a free
 * text note, and pick how the agent should resume after the response.
 *
 * Submits via POST /v1/hitl/:hitl_id/respond. SSE updates
 * (hitl.responded / hitl.expired) automatically remove rows from the
 * pending list — this component just renders whatever the parent passes
 * in via `events`.
 */

interface HITLPanelProps {
  events: HITLEvent[];
  onResponded?: (hitlID: string) => void;
}

const KIND_LABEL: Record<string, string> = {
  permission: "permission",
  tool_approval: "tool approval",
  prompt: "prompt",
  choice: "choice",
};

interface PendingFormState {
  reason: HITLReason;
  note: string;
  resume: HITLResumeMode;
  submitting: boolean;
  error: string | null;
}

const INITIAL_FORM: PendingFormState = {
  reason: "scope",
  note: "",
  resume: "continue",
  submitting: false,
  error: null,
};

function HITLEntry({
  event,
  onResponded,
}: {
  event: HITLEvent;
  onResponded?: (hitlID: string) => void;
}) {
  const [form, setForm] = useState<PendingFormState>(INITIAL_FORM);

  const submit = useCallback(
    async (decision: HITLDecision, note?: string) => {
      if (form.submitting) return;
      setForm((prev) => ({ ...prev, submitting: true, error: null }));
      const body: HITLResponseInput = {
        decision,
        reason_category: form.reason,
        operator_note: note ?? form.note,
        resume_mode: form.resume,
      };
      try {
        const resp = await fetch(apiUrl(`/v1/hitl/${event.hitl_id}/respond`), {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        if (!resp.ok) {
          const text = await resp.text();
          throw new Error(`${resp.status}: ${text || resp.statusText}`);
        }
        onResponded?.(event.hitl_id);
      } catch (err) {
        const message = err instanceof Error ? err.message : "submit failed";
        setForm((prev) => ({ ...prev, submitting: false, error: message }));
      }
    },
    [event.hitl_id, form.submitting, form.reason, form.note, form.resume, onResponded],
  );

  const onSubmitForm = useCallback(
    (ev: FormEvent<HTMLFormElement>) => {
      ev.preventDefault();
      void submit("allow");
    },
    [submit],
  );

  // Ctrl+Enter inside the textarea submits as Allow. Plain Enter inserts a
  // newline so multi-line notes stay editable.
  const onNoteKey = useCallback(
    (ev: KeyboardEvent<HTMLTextAreaElement>) => {
      if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) {
        ev.preventDefault();
        void submit("allow");
      }
    },
    [submit],
  );

  const ageLabel = useMemo(() => `${timeAgo(event.requested_at)} ago`, [event.requested_at]);

  return (
    <li
      className="rounded-md border-l-2 p-3"
      style={{
        background: "var(--bg-raised)",
        borderLeftColor: "var(--status-warning)",
      }}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <ShieldAlert
            size={14}
            strokeWidth={1.5}
            color="var(--artemis-earth)"
          />
          <span
            className="font-display text-[10px] tracking-[0.16em] uppercase"
            style={{ color: "var(--artemis-earth)" }}
          >
            {KIND_LABEL[event.kind] ?? event.kind}
          </span>
        </div>
        <span className="font-mono text-[10px] text-[var(--text-muted)]">
          {ageLabel}
        </span>
      </div>

      <p className="mt-2 font-mono text-[11px] leading-snug text-gray-200">
        {event.question}
      </p>

      <div className="mt-2 flex flex-wrap gap-1">
        {event.context.tool_name && (
          <ContextChip label="tool" value={event.context.tool_name} />
        )}
        {event.context.target_file && (
          <ContextChip label="file" value={event.context.target_file} />
        )}
        {event.context.command_preview && (
          <ContextChip
            label="cmd"
            value={event.context.command_preview}
            mono
          />
        )}
      </div>

      {event.suggestions.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1">
          {event.suggestions.map((sugg) => (
            <button
              key={sugg}
              type="button"
              onClick={() => void submit("custom", sugg)}
              className="rounded border px-2 py-[2px] font-mono text-[10px] text-[var(--text-muted)] hover:text-white"
              style={{ borderColor: "var(--border-bright)", background: "var(--bg-overlay)" }}
            >
              {sugg}
            </button>
          ))}
        </div>
      )}

      <form className="mt-3 flex flex-col gap-2" onSubmit={onSubmitForm}>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => void submit("allow")}
            disabled={form.submitting}
            className="flex flex-1 items-center justify-center gap-1 rounded px-3 py-1 text-[11px] font-medium text-black disabled:opacity-50"
            style={{ background: "var(--status-success)" }}
          >
            <Check size={12} strokeWidth={2.5} /> Allow
          </button>
          <button
            type="button"
            onClick={() => void submit("deny")}
            disabled={form.submitting}
            className="flex flex-1 items-center justify-center gap-1 rounded px-3 py-1 text-[11px] font-medium text-white disabled:opacity-50"
            style={{ background: "var(--status-critical)" }}
          >
            <X size={12} strokeWidth={2.5} /> Deny
          </button>
        </div>

        <div className="flex gap-2">
          <label className="flex flex-1 flex-col gap-1 text-[10px] text-[var(--text-muted)]">
            Reason
            <select
              value={form.reason}
              onChange={(ev) =>
                setForm((prev) => ({ ...prev, reason: ev.target.value as HITLReason }))
              }
              className="rounded border bg-transparent px-2 py-1 font-mono text-[11px] text-gray-200"
              style={{ borderColor: "var(--border-bright)" }}
            >
              {HITL_REASONS.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-1 flex-col gap-1 text-[10px] text-[var(--text-muted)]">
            Resume
            <select
              value={form.resume}
              onChange={(ev) =>
                setForm((prev) => ({
                  ...prev,
                  resume: ev.target.value as HITLResumeMode,
                }))
              }
              className="rounded border bg-transparent px-2 py-1 font-mono text-[11px] text-gray-200"
              style={{ borderColor: "var(--border-bright)" }}
            >
              {HITL_RESUME_MODES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </label>
        </div>

        <label className="flex flex-col gap-1 text-[10px] text-[var(--text-muted)]">
          Note (Ctrl+Enter to allow)
          <textarea
            rows={2}
            value={form.note}
            onKeyDown={onNoteKey}
            onChange={(ev) =>
              setForm((prev) => ({ ...prev, note: ev.target.value }))
            }
            className="resize-y rounded border bg-transparent px-2 py-1 font-mono text-[11px] text-gray-200"
            style={{ borderColor: "var(--border-bright)" }}
            placeholder="optional context"
          />
        </label>

        {form.error && (
          <p className="font-mono text-[10px] text-[var(--status-critical)]">
            {form.error}
          </p>
        )}
      </form>
    </li>
  );
}

function ContextChip({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <span
      className="inline-flex items-center gap-1 rounded border px-2 py-[2px] text-[10px]"
      style={{
        background: "var(--bg-overlay)",
        borderColor: "var(--border-bright)",
        color: "var(--text-muted)",
      }}
    >
      <span className="uppercase tracking-wider">{label}</span>
      <span
        className={mono ? "font-mono text-gray-200" : "text-gray-200"}
        style={{ maxWidth: 180, overflow: "hidden", textOverflow: "ellipsis" }}
      >
        {value}
      </span>
    </span>
  );
}

export default function HITLPanel({ events, onResponded }: HITLPanelProps) {
  // The collector returns pending HITL rows ordered by requested_at ASC,
  // which is a stable, append-only ordering as new requests come in. We
  // sort defensively so a re-fetch with the same set never mutates the
  // visible order while the operator is typing.
  const ordered = useMemo(() => {
    return [...events].sort((a, b) =>
      a.requested_at.localeCompare(b.requested_at),
    );
  }, [events]);

  return (
    <Card raised className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Clock size={12} strokeWidth={1.5} color="var(--artemis-earth)" />
        <span
          className="font-display text-[10px] uppercase tracking-[0.16em]"
          style={{ color: "var(--artemis-earth)" }}
        >
          Human in the loop
        </span>
        <span className="ml-auto font-mono text-[10px] text-[var(--text-muted)]">
          {ordered.length} pending
        </span>
      </div>
      {ordered.length === 0 ? (
        <div className="flex items-center gap-2 text-[12px] text-[var(--text-muted)]">
          <Slash size={12} strokeWidth={1.5} />
          No human intervention requested
        </div>
      ) : (
        <ul className="flex flex-col gap-2">
          {ordered.map((event) => (
            <HITLEntry
              key={event.hitl_id}
              event={event}
              onResponded={onResponded}
            />
          ))}
        </ul>
      )}
    </Card>
  );
}
