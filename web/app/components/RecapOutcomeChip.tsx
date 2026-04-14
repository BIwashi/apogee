import type { RecapOutcome } from "../lib/api-types";
import type { StatusKey } from "../lib/design-tokens";
import StatusPill from "./StatusPill";

/**
 * RecapOutcomeChip — compact status pill mapping the four summariser
 * outcomes onto the shared semantic status tones. Used in the Recap
 * summary panel on the turn detail page.
 */

interface Props {
  outcome: RecapOutcome;
}

const TONE: Record<RecapOutcome, StatusKey> = {
  success: "success",
  partial: "warning",
  failure: "critical",
  aborted: "muted",
};

const LABEL: Record<RecapOutcome, string> = {
  success: "success",
  partial: "partial",
  failure: "failure",
  aborted: "aborted",
};

export default function RecapOutcomeChip({ outcome }: Props) {
  return <StatusPill tone={TONE[outcome]}>{LABEL[outcome]}</StatusPill>;
}
