import type { ReactNode } from "react";

import type { BlockRole, LaidBlock } from "@/lib/booklayout";
import { runsFor } from "@/lib/booklayout";
import type { ReaderBlock } from "@/lib/booktext";
import { cn } from "@/lib/cn";
import type { StyleRun } from "@/lib/types";

/**
 * One block of a book, set as a book sets it. The role (from layoutBlocks)
 * picks the element and its typography — the `.bt-*` rules in index.css —
 * and the runs carry the book's italics, bold and small caps.
 *
 * The read-along highlight is a character span into the block's plain text,
 * so a block being narrated renders as plain text with the mark, keeping its
 * role's typography; its inline marks return as soon as the tape moves on.
 */
export function BookBlockText({
  block,
  laid,
  highlight,
  highlightColor,
}: {
  block: ReaderBlock;
  laid: LaidBlock;
  highlight: [number, number] | null;
  highlightColor: string;
}) {
  if (!block.text) return null;
  const Tag = TAGS[laid.role];
  const content = highlight ? (
    <>
      {block.text.slice(0, highlight[0])}
      <mark className="rounded-sm" style={{ background: highlightColor, color: "inherit" }}>
        {block.text.slice(highlight[0], highlight[1])}
      </mark>
      {block.text.slice(highlight[1])}
    </>
  ) : block.style?.runs ? (
    renderRuns(runsFor(block.style.runs, laid.role))
  ) : (
    block.text
  );

  return (
    <>
      {laid.breakBefore && (
        <div className="bt-break" aria-hidden="true">
          ⁂
        </div>
      )}
      <Tag
        className={cn(
          `bt-${laid.role}`,
          laid.opening && "bt-opening",
          laid.groupStart && "bt-group-start",
          laid.afterInset && "bt-after-inset",
        )}
      >
        {content}
      </Tag>
      {laid.endsOpener && <div className="bt-rule" aria-hidden="true" />}
    </>
  );
}

const TAGS: Record<BlockRole, "p" | "h2" | "h3" | "li" | "pre"> = {
  label: "p",
  title: "h2",
  subtitle: "p",
  section: "h3",
  epigraph: "p",
  attribution: "p",
  quote: "p",
  item: "p",
  pre: "pre",
  caption: "p",
  lead: "p",
  body: "p",
};

function renderRuns(runs: StyleRun[]): ReactNode[] {
  return runs.map((run, index) => {
    const marks = run.m ?? "";
    if (marks.includes("n")) return <br key={index} />;
    let node: ReactNode = run.t;
    if (marks.includes("c")) node = <code>{node}</code>;
    if (marks.includes("s")) node = <span className="bt-sc">{node}</span>;
    if (marks.includes("i")) node = <em>{node}</em>;
    if (marks.includes("b")) node = <strong>{node}</strong>;
    if (marks.includes("^")) node = <sup>{node}</sup>;
    if (marks.includes("_")) node = <sub>{node}</sub>;
    return <span key={index}>{node}</span>;
  });
}
