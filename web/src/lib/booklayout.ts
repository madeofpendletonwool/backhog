import type { ReaderBlock } from "./booktext";
import type { StyleRun } from "./types";

/**
 * From the book's markup to the page's typography.
 *
 * The server reports what each block *is* — a heading, a subtitle, a line of
 * an epigraph — in the book's own terms. This decides what each block is
 * *for* on the page, which takes a little context: an <h1> reading "1."
 * followed by an <h1> reading "THE NAME GAME" is a chapter label and a
 * chapter title, not two titles; the paragraph after them opens the chapter
 * and wants no indent; the last line of an epigraph that starts with a dash
 * is its attribution. A Kindle draws all of this from the publisher's
 * stylesheet. We draw it from structure instead, so every book gets the
 * same considered typography in the reader's own face and paper, rather
 * than each publisher's idea of a heading.
 */

export type BlockRole =
  | "label" // "1.", "Part I", "Chapter Three" — sits above the title
  | "title" // the chapter's title
  | "subtitle"
  | "section" // a heading inside the chapter
  | "epigraph"
  | "attribution" // an epigraph's closing "—AUTHOR"
  | "quote"
  | "item"
  | "pre"
  | "caption"
  | "lead" // the first paragraph after an opener, a heading or a break
  | "body";

export interface LaidBlock {
  role: BlockRole;
  /** The block opens the chapter's prose: the drop cap's place. */
  opening: boolean;
  /** A scene-break ornament sits above it. */
  breakBefore: boolean;
  /** The chapter opener ends after this block: the ornament's place. */
  endsOpener: boolean;
  /** The first of a run of same-role blocks — an epigraph's first line. */
  groupStart: boolean;
  /** Prose resuming after a quote or list, which wants air above it. */
  afterInset: boolean;
}

/** Text short and numeric enough to be a label rather than a title. */
const LABEL_RE =
  /^(?:(?:chapter|part|book|section|act|volume|vol\.?)\s+)?(?:[\divxlcdm]+|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty)\.?$|^(?:chapter|part|book|prologue|epilogue|interlude|introduction)$/i;

/** A block whose every word is bold. */
function allBold(block: ReaderBlock): boolean {
  const runs = block.style?.runs;
  return Boolean(runs?.every((run) => run.t.trim() === "" || (run.m ?? "").includes("b")));
}

/** A short block set entirely in bold — a title the book marked up as a paragraph. */
function boldDisplayLine(block: ReaderBlock): boolean {
  return block.text.length <= 80 && allBold(block);
}

export function layoutBlocks(blocks: ReaderBlock[]): LaidBlock[] {
  const out: LaidBlock[] = blocks.map(() => ({
    role: "body",
    opening: false,
    breakBefore: false,
    endsOpener: false,
    groupStart: false,
    afterInset: false,
  }));

  // The opener: the run of display matter at the top of the document,
  // before its first paragraph of prose.
  let i = 0;
  let sawTitle = false;
  let sawHeading = false;
  for (; i < blocks.length; i += 1) {
    const block = blocks[i];
    const kind = block.style?.kind;
    if (kind === "heading") {
      const next = blocks[i + 1];
      const label =
        !sawTitle &&
        LABEL_RE.test(block.text.trim()) &&
        (next?.style?.kind === "heading" || (next !== undefined && boldDisplayLine(next)));
      out[i].role = label ? "label" : sawTitle ? "subtitle" : "title";
      if (!label) sawTitle = true;
      sawHeading = true;
    } else if (kind === "subtitle") {
      out[i].role = "subtitle";
    } else if (kind === "epigraph") {
      out[i].role = "epigraph";
    } else if (sawHeading && !sawTitle && boldDisplayLine(block)) {
      // "Part I" then a bold "YOU AND ME": the title as a paragraph.
      out[i].role = "title";
      sawTitle = true;
    } else {
      break;
    }
  }
  const openerEnd = sawHeading ? i : 0;
  if (openerEnd > 0) out[openerEnd - 1].endsOpener = true;

  let afterBreak = openerEnd > 0;
  let openingPlaced = openerEnd === 0;
  for (let j = openerEnd; j < blocks.length; j += 1) {
    const block = blocks[j];
    const style = block.style;
    out[j].breakBefore = Boolean(style?.break);
    if (style?.break) afterBreak = true;
    switch (style?.kind) {
      case "heading":
        out[j].role = "section";
        afterBreak = true;
        continue;
      case "subtitle":
        out[j].role = "subtitle";
        continue;
      case "epigraph":
        out[j].role = "epigraph";
        continue;
      case "quote":
      case "item":
      case "pre":
      case "caption":
        out[j].role = style.kind;
        continue;
    }
    if (block.text === "") continue;
    out[j].role = afterBreak ? "lead" : "body";
    // A paragraph set wholly in bold is display matter (a pull quote, a
    // reader's question), not the prose a drop cap opens.
    if (!openingPlaced && afterBreak && !allBold(block)) {
      out[j].opening = true;
      openingPlaced = true;
    }
    afterBreak = false;
  }

  // An epigraph's last line that opens with a dash names its author.
  for (let j = 0; j < blocks.length; j += 1) {
    if (out[j].role !== "epigraph") continue;
    const last = out[j + 1]?.role !== "epigraph";
    if (last && /^[—―–-]/.test(blocks[j].text.trim())) out[j].role = "attribution";
  }
  for (let j = 0; j < blocks.length; j += 1) {
    const previous = j > 0 ? out[j - 1].role : null;
    const role = out[j].role === "attribution" ? "epigraph" : out[j].role;
    const previousRole = previous === "attribution" ? "epigraph" : previous;
    out[j].groupStart = role !== previousRole || out[j].breakBefore;
    out[j].afterInset =
      role !== previousRole && (previousRole === "quote" || previousRole === "item" || previousRole === "pre");
  }
  return out;
}

/** Roles whose weight the reader sets, so the book's own bold is dropped. */
export const DISPLAY_ROLES = new Set<BlockRole>(["label", "title", "subtitle", "section", "attribution"]);

/**
 * The runs a block renders, with any the role overrides removed: a title is
 * set by the reader, not by the publisher's habit of bolding every heading.
 */
export function runsFor(runs: StyleRun[], role: BlockRole): StyleRun[] {
  if (!DISPLAY_ROLES.has(role) && role !== "epigraph") return runs;
  return runs.map((run) => (run.m ? { ...run, m: run.m.replace("b", "") } : run));
}
