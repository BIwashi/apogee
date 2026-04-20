// Constants shared across MissionMap and the per-row sub-components.
//
// The icons + tone palette mirror the tier-3 narrative kind enum so
// the spine node badge always matches the LLM's classification. Layout
// constants describe the SVG geometry of one row (only the spine
// column is fixed-width; the card body to its right is fluid).
import {
  Bug,
  Compass,
  GitCommit,
  HelpCircle,
  Lightbulb,
  Search,
  TestTube,
  UserCog,
  Wrench,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { PhaseKind } from "../../lib/api-types";

// Icon per PhaseKind — reuses the vocabulary already in the tier-3
// summariser prompt so the node badge matches the LLM output.
export const KIND_ICON: Record<PhaseKind, LucideIcon> = {
  implement: Wrench,
  review: Search,
  debug: Bug,
  plan: Compass,
  test: TestTube,
  commit: GitCommit,
  delegate: UserCog,
  explore: Lightbulb,
  other: HelpCircle,
};

// Fixed palette per PhaseKind. Each entry pulls one of the Artemis
// tokens so the node colour on the graph reads as "this kind of
// work" at a glance.
export const KIND_TONE: Record<
  PhaseKind,
  { fill: string; ring: string; label: string }
> = {
  implement: {
    fill: "var(--artemis-earth)",
    ring: "var(--artemis-earth)",
    label: "Implement",
  },
  review: {
    fill: "var(--artemis-space)",
    ring: "var(--artemis-space)",
    label: "Review",
  },
  debug: {
    fill: "var(--artemis-red)",
    ring: "var(--artemis-red)",
    label: "Debug",
  },
  plan: {
    fill: "var(--artemis-blue)",
    ring: "var(--artemis-blue)",
    label: "Plan",
  },
  test: {
    fill: "var(--status-warning)",
    ring: "var(--status-warning)",
    label: "Test",
  },
  commit: {
    fill: "var(--status-success)",
    ring: "var(--status-success)",
    label: "Commit",
  },
  delegate: {
    fill: "var(--artemis-shadow)",
    ring: "var(--artemis-shadow)",
    label: "Delegate",
  },
  explore: { fill: "var(--accent)", ring: "var(--accent)", label: "Explore" },
  other: {
    fill: "var(--border-bright)",
    ring: "var(--border-bright)",
    label: "Other",
  },
};

// Layout constants for the git-graph. All measurements are in pixels
// inside the card; the whole component is fluid-width and only the
// spine column is fixed.
export const SPINE_X = 40; // x coordinate of the main-line spine
export const NODE_R = 14; // outer radius of a phase node
export const ROW_GAP = 28; // vertical gap between rows (in addition to card height)
export const BRANCH_WIDTH = 120; // how far a side-quest branch extends to the right
export const BRANCH_R = 7; // radius of a branch (intervention) node
