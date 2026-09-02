import type { BookTextChapters, ChapterImage, TextChapter } from "./types";

/**
 * The reader's coordinate maths, kept out of the component because it is the
 * part that has to be exactly right.
 *
 * A book comes back as two parallel things. `chapter.blocks` is the canonical
 * byte offset of every block in a spine document — the address space every
 * stored position in the Books arena lives in. `api.bookTextDisplay` returns
 * the same blocks as prose. Block i of one *is* block i of the other; the
 * server builds both in a single pass to guarantee it. That pairing is what
 * lets the page show a paragraph and report an offset that means it.
 *
 * Nothing here does byte arithmetic on a JavaScript string. Offsets are UTF-8
 * byte offsets and JS strings are UTF-16, so treating one as the other breaks
 * the first time a book has an accent in it. They are opaque anchors here.
 */

/** One rendered paragraph: what it says, and where it is. */
export interface ReaderBlock {
  /** Absolute canonical byte offset of this block's first byte. */
  offset: number;
  text: string;
  /** Illustrations that render above this block. */
  images: ChapterImage[];
}

/**
 * Pairs a chapter's canonical offsets with its display text.
 *
 * A length mismatch means the sidecar and the display file disagree, which
 * only happens if one was written by an older parser. Rather than render text
 * under offsets that do not belong to it — every position the reader then
 * wrote would be wrong — it pairs only as far as both agree.
 */
export function readerBlocks(chapter: TextChapter, display: string[]): ReaderBlock[] {
  const offsets = chapter.blocks ?? [];
  const imagesAt = groupImages(chapter, offsets.length);

  if (offsets.length === 0) {
    // An image-only document (a cover page) owns no text but may still have
    // art. Give it one empty block for the images to hang on.
    const images = imagesAt.get(0) ?? [];
    return images.length > 0 ? [{ offset: chapter.char_start, text: "", images }] : [];
  }

  const paired = Math.min(offsets.length, display.length);
  const blocks: ReaderBlock[] = [];
  for (let i = 0; i < paired; i += 1) {
    blocks.push({ offset: offsets[i], text: display[i], images: imagesAt.get(i) ?? [] });
  }
  return blocks;
}

/**
 * Buckets a chapter's images by the block they render above. Anything
 * anchored past the last block is pulled onto it, so a trailing plate still
 * appears rather than being dropped for pointing one past the end.
 */
function groupImages(chapter: TextChapter, blockCount: number): Map<number, ChapterImage[]> {
  const grouped = new Map<number, ChapterImage[]>();
  const last = Math.max(0, blockCount - 1);
  for (const image of chapter.images ?? []) {
    if (!isInternalHref(image.href)) continue;
    const at = Math.min(last, Math.max(0, image.before_block));
    const bucket = grouped.get(at);
    if (bucket) bucket.push(image);
    else grouped.set(at, [image]);
  }
  return grouped;
}

/**
 * Whether an href is a plain relative path inside the EPUB.
 *
 * The parser already refuses everything else, and the asset endpoint refuses
 * it again — this is the third check, at the point where a string would
 * become an `<img src>`. It is cheap, and it means no single mistake upstream
 * can turn a book into an off-origin request (design invariant 5).
 */
export function isInternalHref(href: string): boolean {
  if (!href || href.startsWith("/") || href.startsWith("//")) return false;
  const colon = href.indexOf(":");
  const slash = href.indexOf("/");
  if (colon >= 0 && (slash < 0 || colon < slash)) return false;
  return !href.split("/").includes("..");
}

/**
 * The chapter holding an offset. Ranges are [char_start, char_end), so the
 * very end of the book belongs to the last chapter that holds any text —
 * the same rule the server's chapterAt uses.
 */
export function chapterAt(chapters: TextChapter[], offset: number): TextChapter | null {
  let last: TextChapter | null = null;
  for (const chapter of chapters) {
    if (chapter.char_end > chapter.char_start) last = chapter;
    if (offset >= chapter.char_start && offset < chapter.char_end) return chapter;
  }
  return last;
}

/** The index of the block containing an offset: the last one at or before it. */
export function blockIndexAt(blocks: ReaderBlock[], offset: number): number {
  let found = 0;
  for (let i = 0; i < blocks.length; i += 1) {
    if (blocks[i].offset > offset) break;
    found = i;
  }
  return found;
}

/** Chapters that hold text, which are the ones worth listing in a TOC. */
export function readableChapters(chapters: TextChapter[]): TextChapter[] {
  return chapters.filter((chapter) => chapter.char_end > chapter.char_start);
}

/** A chapter's display name; spine documents often have no TOC entry. */
export function chapterTitle(chapter: TextChapter): string {
  return chapter.title.trim() || `Section ${chapter.spine_index + 1}`;
}

/** How far through the whole book an offset is, 0–100. */
export function percentAt(text: BookTextChapters | undefined, offset: number): number {
  if (!text || text.char_count <= 0) return 0;
  return Math.min(100, Math.max(0, (offset / text.char_count) * 100));
}
