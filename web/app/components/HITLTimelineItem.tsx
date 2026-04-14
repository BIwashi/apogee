"use client";

import { Check, Clock, ShieldAlert, X } from "lucide-react";

import type { HITLEvent } from "../lib/api-types";

/**
 * HITLTimelineItem — compact card representation of a finalised HITL event
 * for inline rendering inside the span tree / past-event list. The
 * decision drives the accent color: allow=success, deny=critical,
 * timeout/expired=muted. The reason category and operator note are shown
 * as chips when set so the audit trail stays self-describing.
 */

interface HITLTimelineItemProps {
  event: HITLEvent;
}

function decisionTone(event: HITLEvent): {
  bg: string;
  fg: string;
  label: string;
  icon: typeof Check;
} {
  if (event.status === "expired" || event.status === "timeout") {
    return {
      bg: "var(--status-muted)",
      fg: "var(--text-primary)",
      label: event.status,
      icon: Clock,
    };
  }
  switch (event.decision) {
    case "allow":
      return {
        bg: "var(--status-success)",
        fg: "#06080f",
        label: "allow",
        icon: Check,
      };
    case "deny":
      return {
        bg: "var(--status-critical)",
        fg: "#ffffff",
        label: "deny",
        icon: X,
      };
    case "custom":
      return {
        bg: "var(--artemis-earth)",
        fg: "#06080f",
        label: "custom",
        icon: Check,
      };
    default:
      return {
        bg: "var(--status-muted)",
        fg: "var(--text-primary)",
        label: event.status,
        icon: Clock,
      };
  }
}

export default function HITLTimelineItem({ event }: HITLTimelineItemProps) {
  const tone = decisionTone(event);
  const Icon = tone.icon;
  return (
    <div
      className="my-1 ml-6 flex flex-col gap-1 rounded border p-2"
      style={{
        background: "var(--bg-overlay)",
        borderColor: "var(--border-bright)",
      }}
    >
      <div className="flex items-center gap-2">
        <ShieldAlert size={11} strokeWidth={1.5} color="var(--artemis-earth)" />
        <span
          className="rounded px-1.5 py-[1px] font-mono text-[9px] uppercase"
          style={{ background: tone.bg, color: tone.fg }}
        >
          <Icon size={9} strokeWidth={2.5} className="mb-[1px] mr-1 inline" />
          {tone.label}
        </span>
        <span className="truncate font-mono text-[10px] text-gray-200">
          {event.question}
        </span>
      </div>
      {(event.reason_category || event.operator_note) && (
        <div className="flex flex-wrap items-center gap-1">
          {event.reason_category && (
            <span
              className="rounded border px-1.5 py-[1px] text-[9px] uppercase tracking-wider"
              style={{
                background: "var(--bg-overlay)",
                borderColor: "var(--border-bright)",
                color: "var(--text-muted)",
              }}
            >
              {event.reason_category}
            </span>
          )}
          {event.operator_note && (
            <span className="font-mono text-[10px] text-[var(--text-muted)]">
              “{event.operator_note}”
            </span>
          )}
        </div>
      )}
    </div>
  );
}
