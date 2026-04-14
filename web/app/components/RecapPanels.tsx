import Card from "./Card";
import SectionHeader from "./SectionHeader";

/**
 * RecapPanels — three placeholder panels that PR #6 (the LLM summarizer)
 * will hydrate with structured Haiku output. PR #5 ships them empty so the
 * layout is locked in early; the empty-state copy explains the contract to
 * future readers.
 */

interface PanelDef {
  title: string;
  empty: string;
}

const PANELS: PanelDef[] = [
  {
    title: "Summary",
    empty: "Recap will appear here once the turn closes.",
  },
  {
    title: "Key steps",
    empty: "Key steps will appear here.",
  },
  {
    title: "Notable events",
    empty: "Notable events will appear here.",
  },
];

export default function RecapPanels() {
  return (
    <div className="grid gap-3 md:grid-cols-3">
      {PANELS.map((panel) => (
        <Card key={panel.title} className="p-4">
          <SectionHeader title={panel.title} />
          <p className="text-[12px] text-[var(--text-muted)]">{panel.empty}</p>
        </Card>
      ))}
    </div>
  );
}
