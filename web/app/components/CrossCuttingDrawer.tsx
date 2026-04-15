"use client";

import { useCallback } from "react";
import { type DrawerSpec, useDrawerState } from "../lib/drawer";
import AgentDrawer from "./AgentDrawer";
import SessionDrawer from "./SessionDrawer";
import SideDrawer from "./SideDrawer";
import SpanDrawer from "./SpanDrawer";
import TurnDrawer from "./TurnDrawer";

/**
 * CrossCuttingDrawer — the single mount point that reads the URL-driven
 * drawer state (PR #36) and renders the correct entity drawer. One instance
 * lives at the root layout so every dashboard route can participate in the
 * same Datadog row-click pattern without each page having to wire its own
 * SideDrawer.
 *
 * The SideDrawer shell stays mounted even while the drawer is "closed" so
 * the slide-out animation plays on both open and close, and so cross-entity
 * navigation (click a turn chip inside a session drawer → drawer content
 * swaps) never flashes a new panel.
 */

function drawerTitle(spec: DrawerSpec): string {
  if (!spec) return "";
  switch (spec.kind) {
    case "agent":
      return "Agent detail";
    case "session":
      return "Session detail";
    case "turn":
      return "Turn detail";
    case "span":
      return "Span detail";
  }
}

function drawerBody(spec: DrawerSpec) {
  if (!spec) return null;
  switch (spec.kind) {
    case "agent":
      return <AgentDrawer agentID={spec.id} />;
    case "session":
      return <SessionDrawer sessionID={spec.id} />;
    case "turn":
      return <TurnDrawer sessionID={spec.sess} turnID={spec.turn} />;
    case "span":
      return <SpanDrawer traceID={spec.traceID} spanID={spec.spanID} />;
  }
}

export default function CrossCuttingDrawer() {
  const { drawer, close } = useDrawerState();
  const onClose = useCallback(() => close(), [close]);
  const open = drawer !== null;
  return (
    <SideDrawer
      open={open}
      onClose={onClose}
      title={drawerTitle(drawer)}
      width="lg"
    >
      {drawerBody(drawer)}
    </SideDrawer>
  );
}
