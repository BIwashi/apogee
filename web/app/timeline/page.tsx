"use client";

/**
 * `/timeline` — Datadog Service Catalog's Timeline equivalent. For now it
 * renders the fleet dashboard directly so the sidebar entry has a real
 * destination; over time it may grow its own dense timeline view. Kept as a
 * separate route from `/` so the breadcrumb and sidebar highlight stay
 * coherent even when a scope is active.
 */

import Dashboard from "../page";

export default function TimelinePage() {
  return <Dashboard />;
}
