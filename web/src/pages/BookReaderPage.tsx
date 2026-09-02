import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams, useParams } from "react-router-dom";

import { Dialog } from "@/components/ui/Dialog";
import { Gi } from "@/components/ui/Gi";
import { Button, EmptyState, Skeleton } from "@/components/ui/primitives";
import { useAudioClock, useAudioPlayer } from "@/hooks/useAudioPlayer";
import { useBook, useBookEntry } from "@/hooks/useBooks";
import { usePersistentState } from "@/hooks/usePersistentState";
import { ApiError, api, beaconBookPosition, bookAssetUrl } from "@/lib/api";
import {
  blockIndexAt,
  chapterAt,
  chapterTitle,
  percentAt,
  readableChapters,
  readerBlocks,
  sentenceSpanAt,
} from "@/lib/booktext";
import { cn } from "@/lib/cn";
import { formatTimecode } from "@/lib/format";
import type { BookEntry, BookPosition, PositionTranslation, TextChapter } from "@/lib/types";

/**
 * The in-app EPUB reader — the other half of the reading/listening handoff.
 *
 * **Scrolled, not paginated, and deliberately.** The truth about where you
 * are is a canonical character offset, and a scrolled column keeps the
 * mapping from screen to offset direct: every paragraph is a real element
 * carrying its own offset, so "where am I" is a `getBoundingClientRect` and
 * "put me back" is a `scrollTo`. Pagination would need a measuring engine
 * that re-flows the whole book on every font-size, margin and viewport
 * change, and it would still have to fall back to offsets to say anything
 * about position — the pages would be a lossy view over the same number.
 * Scrolling gets the acceptance criteria for free: change the type and the
 * paragraph you were on is still the paragraph you are on.
 *
 * **Nothing here parses EPUB markup.** The server hands over two parallel
 * things: the canonical block offsets and the same blocks as prose. Both are
 * plain strings that become React text nodes — there is no
 * `dangerouslySetInnerHTML` on this page and no HTML from a book ever
 * reaches the DOM, so a book carrying `<script>` has nothing to run and a
 * book carrying `onclick=` has nothing to attach to. Illustrations are the
 * one thing with an address, and the address is always ours: the parser
 * drops remote, protocol-relative and `data:` references at ingest,
 * `isInternalHref` refuses them again here, and the bytes come from our own
 * authenticated asset endpoint. Design invariant 5 holds by construction.
 *
 * **Typography.** Body text of a book is long-form prose, so it uses a real
 * reading face — never Silkscreen, which is for labels and numbers. The
 * chrome around the column is the app's; the column itself is its own
 * high-contrast reading surface (see SURFACES).
 */
export function BookReaderPage() {
  const { entryId } = useParams<{ entryId: string }>();
  const { entry, isLoading } = useBookEntry(entryId);

  if (isLoading) return <ReaderSkeleton />;

  if (!entry) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-20 text-center">
        <p className="text-ink-300">That book isn't on your shelf.</p>
        <Link to="/books" className="mt-4 inline-block text-sm text-brand-400 hover:text-brand-300">
          Back to the shelf
        </Link>
      </div>
    );
  }

  return <Reader entry={entry} />;
}

/** How often a moving reader checkpoints its position — the player's rate. */
const WRITE_EVERY_MS = 15_000;

/** How far under the toolbar the "you are here" line sits, in pixels. */
const ANCHOR_INSET = 8;

type SurfaceName = "dark" | "sepia" | "light";
type FaceName = "serif" | "sans";

interface ReaderPrefs {
  /** Body size in px. */
  fontSize: number;
  lineHeight: number;
  /** Column width in rem — the measure, which is what margins really set. */
  measure: number;
  face: FaceName;
  surface: SurfaceName;
}

const DEFAULT_PREFS: ReaderPrefs = {
  fontSize: 19,
  lineHeight: 1.65,
  measure: 36,
  face: "serif",
  surface: "dark",
};

/**
 * The reading surfaces.
 *
 * These are the one place in the app that names colours instead of reaching
 * for an ink token, because the column is not a themed panel: it is paper,
 * and it has to stay the same paper in all six themes (the room is
 * themeable, what is in it is not). Every pairing clears invariant 8's
 * contrast floor with room to spare — measured against their own fill,
 * dark is ~14:1, sepia ~11:1 and light ~16:1 — so the floor is kept by the
 * same rule the ink ladder keeps it by, just solved once here.
 */
const SURFACES: Record<SurfaceName, {
  label: string;
  bg: string;
  fg: string;
  muted: string;
  rule: string;
}> = {
  dark: { label: "Night", bg: "#101118", fg: "#e7e8ee", muted: "#8b8fa0", rule: "#262935" },
  sepia: { label: "Paper", bg: "#f3ecdd", fg: "#292217", muted: "#6b6049", rule: "#dcd0b8" },
  light: { label: "Day", bg: "#fbfbfd", fg: "#15161b", muted: "#5b5f6e", rule: "#e1e2e9" },
};

/** Reading faces. Silkscreen is not among them and never will be. */
const FACES: Record<FaceName, { label: string; stack: string }> = {
  serif: {
    label: "Serif",
    stack:
      '"Iowan Old Style", "Palatino Linotype", Palatino, "Book Antiqua", Georgia, ui-serif, serif',
  },
  sans: {
    label: "Sans",
    stack:
      'ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", sans-serif',
  },
};

function Reader({ entry }: { entry: BookEntry }) {
  const queryClient = useQueryClient();
  const player = useAudioPlayer();
  // The work read carries the printings, which is where a page count lives.
  const { data: work } = useBook(entry.book.id);
  const [searchParams, setSearchParams] = useSearchParams();

  const [prefs, setPrefs] = usePersistentState<ReaderPrefs>("backhog:reader", DEFAULT_PREFS);
  const [panel, setPanel] = useState<"none" | "contents" | "type">("none");

  // The player's handoff ("Continue reading" there) lands here as
  // ?offset=N: the offset to open at instead of the stored one. Derived from
  // the URL every render, so a handoff that arrives while the book is
  // already open still lands; the parameter is stripped the moment it has
  // been consumed, so a refresh restores the stored position like any other
  // open.
  const requestedParam = searchParams.get("offset");
  const requestedOffset =
    requestedParam !== null &&
    Number.isFinite(Number(requestedParam)) &&
    Number(requestedParam) >= 0
      ? Math.floor(Number(requestedParam))
      : null;
  // The block a handoff landed on pulses once, so the eye lands with it.
  const [flash, setFlash] = useState<number | null>(null);

  const text = useQuery({
    queryKey: ["bookTextChapters", entry.id],
    queryFn: () => api.bookTextChapters(entry.id),
    staleTime: Infinity, // a parsed book does not change while you read it
    retry: false,
  });
  const position = useQuery({
    queryKey: ["bookPosition", entry.id],
    queryFn: () => api.bookPosition(entry.id),
  });
  const audio = useQuery({
    queryKey: ["bookAudio", entry.id],
    queryFn: () => api.bookAudio(entry.id),
    retry: false, // 404 here means "no audiobook", not a flaky network
  });

  const chapters = useMemo(() => text.data?.chapters ?? [], [text.data]);
  const [spine, setSpine] = useState<number | null>(null);

  const articleRef = useRef<HTMLDivElement>(null);
  const toolbarRef = useRef<HTMLDivElement>(null);
  /** Where the reader is right now. A ref because it moves per frame. */
  const offsetRef = useRef(0);
  /** An offset waiting to be scrolled to once its chapter has rendered. */
  const pendingRef = useRef<number | null>(null);
  /** Writes stay shut until the restore has landed, so an un-anchored
   *  page-one render can never overwrite a real stored position. */
  const restoredRef = useRef(false);
  const writtenRef = useRef<number | null>(null);
  const [liveOffset, setLiveOffset] = useState(0);

  const chapter = spine === null ? null : chapters.find((c) => c.spine_index === spine) ?? null;

  const display = useQuery({
    queryKey: ["bookTextDisplay", entry.id, spine],
    queryFn: () => api.bookTextDisplay(entry.id, spine as number),
    enabled: spine !== null,
    staleTime: Infinity,
  });

  const blocks = useMemo(
    () => (chapter && display.data ? readerBlocks(chapter, display.data.blocks) : []),
    [chapter, display.data],
  );

  // --- opening the book ------------------------------------------------
  // The stored offset picks the chapter; the chapter picks the paragraph.
  // Nothing about the last session's font size or window is involved, which
  // is exactly why the offset and not a scroll percentage is the truth.
  // A handoff ?offset= overrides it exactly once.
  useEffect(() => {
    if (spine !== null || chapters.length === 0) return;
    // A position that will not load must not hold the book shut: an
    // unreadable position is the start of the book, which is where a book
    // with no stored position starts anyway.
    if (!position.data && !position.isError) return;
    const stored = position.data?.char_offset ?? 0;
    const at =
      requestedOffset !== null
        ? Math.min(Math.max(requestedOffset, 0), text.data?.char_count ?? requestedOffset)
        : stored;
    const target = chapterAt(chapters, at) ?? readableChapters(chapters)[0] ?? chapters[0];
    offsetRef.current = at;
    setLiveOffset(at);
    pendingRef.current = at;
    setSpine(target.spine_index);
    if (requestedOffset !== null) {
      setFlash(at);
      setSearchParams({}, { replace: true });
    }
  }, [chapters, position.data, position.isError, requestedOffset, setSearchParams, spine, text.data]);

  useEffect(() => {
    if (flash === null) return;
    const id = window.setTimeout(() => setFlash(null), 2600);
    return () => window.clearTimeout(id);
  }, [flash]);

  /** Scrolls the block owning `offset` up to the reading line. */
  const anchorTo = useCallback((offset: number) => {
    const article = articleRef.current;
    if (!article) return false;
    const nodes = Array.from(article.querySelectorAll<HTMLElement>("[data-offset]"));
    if (nodes.length === 0) return false;

    let target = nodes[0];
    for (const node of nodes) {
      if (Number(node.dataset.offset) > offset) break;
      target = node;
    }
    const line = toolbarRef.current?.getBoundingClientRect().bottom ?? 0;
    const top = target.getBoundingClientRect().top + window.scrollY - line - ANCHOR_INSET;
    window.scrollTo({ top: Math.max(0, top), behavior: "auto" });
    return true;
  }, []);

  // A handoff landing while the book is already open: jump straight to the
  // narrated paragraph instead of waiting for a reopen.
  useEffect(() => {
    if (requestedOffset === null || spine === null) return;
    const clamped = Math.min(
      Math.max(requestedOffset, 0),
      text.data?.char_count ?? requestedOffset,
    );
    offsetRef.current = clamped;
    setLiveOffset(clamped);
    const target = chapterAt(chapters, clamped);
    if (target && target.spine_index !== spine) {
      pendingRef.current = clamped;
      restoredRef.current = false;
      setSpine(target.spine_index);
    } else {
      anchorTo(clamped);
    }
    setFlash(clamped);
    setSearchParams({}, { replace: true });
  }, [anchorTo, chapters, requestedOffset, setSearchParams, spine, text.data]);

  // Land the restore before paint, so opening a book never flashes page one.
  useLayoutEffect(() => {
    if (pendingRef.current === null) return;
    if (blocks.length === 0) {
      // An image-only chapter has nothing to anchor to; opening the writes
      // anyway keeps a reader who lands there from being silently frozen.
      if (display.data) {
        pendingRef.current = null;
        restoredRef.current = true;
      }
      return;
    }
    if (anchorTo(pendingRef.current)) {
      pendingRef.current = null;
      restoredRef.current = true;
    }
  }, [anchorTo, blocks, display.data]);

  // --- where you are ---------------------------------------------------
  useEffect(() => {
    let frame = 0;
    const measure = () => {
      frame = 0;
      const article = articleRef.current;
      if (!article) return;
      const nodes = Array.from(article.querySelectorAll<HTMLElement>("[data-offset]"));
      if (nodes.length === 0) return;

      const line = (toolbarRef.current?.getBoundingClientRect().bottom ?? 0) + ANCHOR_INSET;
      let current = nodes[0];
      for (const node of nodes) {
        if (node.getBoundingClientRect().top > line) break;
        current = node;
      }
      const offset = Number(current.dataset.offset);
      if (!Number.isFinite(offset)) return;
      offsetRef.current = offset;
      setLiveOffset(offset); // same value bails out of the render
    };
    const onScroll = () => {
      if (!frame) frame = window.requestAnimationFrame(measure);
    };

    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
    };
  }, [blocks]);

  // Re-anchor whenever the type changes. This is the acceptance criterion
  // "change the font size mid-book and the position does not move": the
  // paragraph is the anchor, so the reflow moves the page under it rather
  // than moving the reader through the book.
  const metrics = `${prefs.fontSize}:${prefs.lineHeight}:${prefs.measure}:${prefs.face}`;
  useLayoutEffect(() => {
    if (!restoredRef.current) return;
    anchorTo(offsetRef.current);
  }, [anchorTo, metrics]);

  // --- read-along ------------------------------------------------------
  // While this book's audio plays, the tape is the truth about where you
  // are: the reader follows it sentence by sentence and stands down its own
  // position writes — one writer at a time, and the moving one wins.
  const following = player.entry?.id === entry.id;
  const followingLive = following && player.playing;
  const followingRef = useRef(false);
  useEffect(() => {
    followingRef.current = followingLive;
  }, [followingLive]);

  /** The canonical offset the tape is currently narrating, when known. */
  const [readAlong, setReadAlong] = useState<number | null>(null);
  /** True once a lookup proved there is no map to follow. */
  const [readAlongDead, setReadAlongDead] = useState(false);

  const followTape = useCallback(
    (char: number | null) => {
      if (char === null) {
        setReadAlongDead(true);
        return;
      }
      setReadAlongDead(false);
      setReadAlong(char);
      // The toolbar's percentage follows the tape too, without writing it.
      offsetRef.current = char;
      setLiveOffset(char);
      // Follow the narrator across chapter boundaries with the same
      // machinery a TOC pick uses: pend the offset, render, anchor.
      if (spine !== null && chapters.length > 0) {
        const current = chapters.find((c) => c.spine_index === spine);
        const inside = current && char >= current.char_start && char < current.char_end;
        if (!inside) {
          const target = chapterAt(chapters, char);
          if (target && target.spine_index !== spine) {
            pendingRef.current = char;
            restoredRef.current = false;
            setSpine(target.spine_index);
          }
        }
      }
    },
    [chapters, spine],
  );

  // The highlight belongs to the tape: when it stops following, it goes.
  useEffect(() => {
    if (!following) setReadAlong(null);
  }, [following]);

  // --- reporting where you are -----------------------------------------
  const save = useCallback(
    (offset: number) => {
      writtenRef.current = offset;
      api
        .putBookPosition(entry.id, { char_offset: offset, source: "read" })
        .then((result) => queryClient.setQueryData(["bookPosition", entry.id], result.position))
        .catch(() => {
          // A failed write must not be remembered as written, or the next
          // checkpoint would skip an offset that never landed.
          writtenRef.current = null;
        });
    },
    [entry.id, queryClient],
  );

  const flush = useCallback(() => {
    // Following the tape, the reader has nothing to add — the player is
    // already checkpointing the listening position, which *is* this offset.
    if (followingRef.current) return;
    if (!restoredRef.current) return;
    if (offsetRef.current === writtenRef.current) return;
    save(offsetRef.current);
  }, [save]);

  useEffect(() => {
    const id = window.setInterval(flush, WRITE_EVERY_MS);
    // Leaving the reader is itself a checkpoint.
    return () => {
      window.clearInterval(id);
      flush();
    };
  }, [flush]);

  // A closing tab or a backgrounded phone gets the beacon, which is the only
  // write a browser promises to finish on the way out.
  useEffect(() => {
    const leave = () => {
      if (followingRef.current) return;
      if (!restoredRef.current) return;
      if (offsetRef.current === writtenRef.current) return;
      writtenRef.current = offsetRef.current;
      beaconBookPosition(entry.id, { char_offset: offsetRef.current, source: "read" });
    };
    const onVisibility = () => {
      if (document.visibilityState === "hidden") leave();
    };
    window.addEventListener("pagehide", leave);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("pagehide", leave);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [entry.id]);

  // Keep the narrated paragraph on screen, gently, without stealing a
  // scroll the listener started themselves.
  useEffect(() => {
    if (!followingLive || readAlong === null || blocks.length === 0) return;
    const blockOffset = blocks[blockIndexAt(blocks, readAlong)]?.offset;
    if (blockOffset === undefined) return;
    const block = articleRef.current?.querySelector<HTMLElement>(
      `[data-offset="${blockOffset}"]`,
    );
    if (!block) return;
    const rect = block.getBoundingClientRect();
    const line = toolbarRef.current?.getBoundingClientRect().bottom ?? 0;
    if (rect.top >= line && rect.bottom <= window.innerHeight) return;
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const top = rect.top + window.scrollY - line - window.innerHeight * 0.3;
    window.scrollTo({ top: Math.max(0, top), behavior: reduced ? "auto" : "smooth" });
  }, [blocks, followingLive, readAlong]);

  // --- the handoff -----------------------------------------------------
  const hasAudio = (audio.data?.tracks.length ?? 0) > 0;
  // derived means the server translated this offset through an alignment
  // map. Without one there is genuinely no second to jump to — the control
  // stays visible and says so, because knowing what unlocks it beats a
  // missing button.
  const aligned = position.data?.audio?.derived === true;
  const handoffBlocked = !hasAudio
    ? "No audiobook is attached to this book, so there is nothing to hand off to. Attach one from Book files."
    : !aligned
      ? "Switching from a paragraph needs the audiobook lined up against the text. Run Align on the book's page, and this opens."
      : null;

  // The confirmation asks the server where this paragraph would land on the
  // tape — a speculative lookup, nothing stored — and shows the passage
  // next to the timestamp. Showing it is not decoration: it is how a bad
  // map is caught in one glance, by the one person who knows what they
  // were just reading.
  const [handoff, setHandoff] = useState<PositionTranslation | null>(null);
  const prepareHandoff = useMutation({
    mutationFn: () => api.translateBookPosition(entry.id, { char: offsetRef.current }),
    onSuccess: (translation) => {
      if (translation.audio) setHandoff(translation);
    },
  });

  const startListening = useMutation({
    mutationFn: async () => {
      if (!handoff?.audio) return null;
      // Confirming is also the checkpoint: the reader's offset is written
      // before the player starts, so both ends agree on where the tape
      // begins even if the player is closed before its first tick.
      await api.putBookPosition(entry.id, {
        char_offset: handoff.char_offset,
        source: "read",
      });
      return handoff.char_offset;
    },
    onSuccess: (offset) => {
      const seconds = handoff?.audio?.seconds ?? null;
      setHandoff(null);
      if (seconds == null) return;
      writtenRef.current = offset;
      queryClient.setQueryData(["bookPosition", entry.id], (previous: BookPosition | undefined) =>
        previous ? { ...previous, char_offset: offset ?? previous.char_offset } : previous,
      );
      player.open(entry, { autoplay: true, startAt: seconds });
    },
  });

  const goToChapter = useCallback((target: TextChapter) => {
    offsetRef.current = target.char_start;
    setLiveOffset(target.char_start);
    pendingRef.current = target.char_start;
    // Hold the writes until the new chapter has been anchored.
    restoredRef.current = false;
    setSpine(target.spine_index);
    setPanel("none");
  }, []);

  // --- the states before the text --------------------------------------
  if (text.isLoading) return <ReaderSkeleton />;
  if (text.error) return <ReaderProblem entry={entry} error={text.error} />;

  const surface = SURFACES[prefs.surface];
  const toc = readableChapters(chapters);
  const spineOrder = toc.findIndex((c) => c.spine_index === spine);
  const previous = spineOrder > 0 ? toc[spineOrder - 1] : null;
  const next = spineOrder >= 0 && spineOrder < toc.length - 1 ? toc[spineOrder + 1] : null;

  const percent = percentAt(text.data, liveOffset);
  const page = position.data?.page?.page ?? null;
  const totalPages = (work?.editions ?? []).find((edition) => edition.page_count)?.page_count ?? null;

  // The flash names a character offset, but the pulse paints a block: the
  // one the offset lands inside.
  const flashBlock =
    flash !== null && blocks.length > 0
      ? (blocks[blockIndexAt(blocks, flash)]?.offset ?? null)
      : null;

  // Where the tape is, rendered: the block it is narrating and the sentence
  // inside it. The sentence is an estimate — canonical offsets are byte
  // offsets into folded text, so they never map exactly onto the display
  // string — but picking a paragraph's sentence only needs the neighbourhood
  // right, and the highlight says "about here", which is what it is.
  const readAlongView = (() => {
    if (!following || readAlong === null) return null;
    const index = blockIndexAt(blocks, readAlong);
    const block = blocks[index];
    if (!block?.text) return null;
    const end =
      index + 1 < blocks.length
        ? blocks[index + 1].offset
        : (chapter?.char_end ?? block.offset + 1);
    const fraction = (readAlong - block.offset) / Math.max(1, end - block.offset);
    return { block: index, span: sentenceSpanAt(block.text, fraction) };
  })();
  const readAlongBlock = readAlongView?.block ?? -1;
  const readAlongSpan = readAlongView?.span ?? null;

  // The passage the handoff confirmation shows: the sentence the alignment
  // matched, taken from the paragraph it landed in.
  const handoffPassage = (() => {
    if (!handoff || blocks.length === 0) return null;
    const index = blockIndexAt(blocks, handoff.char_offset);
    const block = blocks[index];
    if (!block?.text) return null;
    const end =
      index + 1 < blocks.length
        ? blocks[index + 1].offset
        : (chapter?.char_end ?? block.offset + 1);
    const fraction = (handoff.char_offset - block.offset) / Math.max(1, end - block.offset);
    const span = sentenceSpanAt(block.text, fraction);
    return span ? block.text.slice(span[0], span[1]) : block.text;
  })();

  return (
    <div style={{ background: surface.bg, color: surface.fg }} className="min-h-screen">
      <div
        ref={toolbarRef}
        className="sticky top-16 z-20 border-b backdrop-blur lg:top-0"
        style={{ borderColor: surface.rule, background: `${surface.bg}f2` }}
      >
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-2 px-4 py-2.5 sm:px-6">
          <Link
            to={`/books/${entry.id}`}
            className="inline-flex shrink-0 items-center gap-1.5 rounded-lg text-sm transition-opacity hover:opacity-70 focus-visible:focus-ring"
            style={{ color: surface.muted }}
          >
            <Gi name="arrow-left" className="size-4" />
            <span className="hidden sm:inline">{entry.book.title}</span>
          </Link>

          <span className="min-w-0 flex-1 truncate text-sm" title={chapter ? chapterTitle(chapter) : ""}>
            {chapter ? chapterTitle(chapter) : " "}
          </span>

          <ToolbarButton
            surface={surface}
            active={panel === "contents"}
            label="Contents"
            icon="list-tree"
            onClick={() => setPanel(panel === "contents" ? "none" : "contents")}
          />
          <ToolbarButton
            surface={surface}
            active={panel === "type"}
            label="Reading settings"
            icon="sliders"
            onClick={() => setPanel(panel === "type" ? "none" : "type")}
          />

          {/* The control stays visible when it cannot be used: knowing the
              handoff exists and what unlocks it beats a missing button. */}
          <span title={handoffBlocked ?? "Start the audiobook from this paragraph"}>
            <Button
              size="sm"
              variant="primary"
              disabled={handoffBlocked !== null}
              loading={prepareHandoff.isPending}
              onClick={() => prepareHandoff.mutate()}
            >
              <Gi name="headphones" className="size-3.5" />
              <span className="hidden sm:inline">Continue in audio</span>
            </Button>
          </span>
        </div>

        <div
          className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-3 gap-y-1 px-4 pb-2 text-xs sm:px-6"
          style={{ color: surface.muted }}
        >
          <div className="h-1 min-w-24 flex-1 overflow-hidden rounded-full" style={{ background: surface.rule }}>
            <div
              className="h-full rounded-full transition-[width] duration-300"
              style={{ width: `${percent}%`, background: surface.muted }}
            />
          </div>
          <span className="shrink-0 tabular-nums">{Math.round(percent)}%</span>
          {/* The page view exists only once a page map does; until then the
              server sends page: null and this simply is not there. */}
          {page !== null && (
            <span className="shrink-0 tabular-nums">
              page {page}
              {totalPages !== null ? ` of ${totalPages}` : ""}
            </span>
          )}
          {handoffBlocked && (
            <span className="min-w-0 basis-full truncate sm:basis-auto" title={handoffBlocked}>
              {handoffBlocked}
            </span>
          )}
        </div>

        {panel === "contents" && (
          <Contents chapters={toc} current={spine} surface={surface} onPick={goToChapter} />
        )}
        {panel === "type" && <TypeControls prefs={prefs} onChange={setPrefs} surface={surface} />}
      </div>

      <div
        className="mx-auto px-5 pb-32 pt-8 sm:px-6"
        style={{
          maxWidth: `${prefs.measure}rem`,
          fontFamily: FACES[prefs.face].stack,
          fontSize: `${prefs.fontSize}px`,
          lineHeight: prefs.lineHeight,
        }}
      >
        {(display.isLoading || spine === null) && (
          <div className="space-y-3 py-6">
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-11/12" />
            <Skeleton className="h-4 w-10/12" />
          </div>
        )}

        <div ref={articleRef}>
          {blocks.map((block, index) => (
            <div
              key={`${block.offset}-${index}`}
              data-offset={block.offset}
              className={cn(flashBlock === block.offset && "reader-flash")}
              style={
                flashBlock === block.offset
                  ? { ["--reader-flash-from" as string]: `${surface.fg}40` }
                  : undefined
              }
            >
              {block.images.map((image) => (
                <img
                  key={image.href}
                  src={bookAssetUrl(entry.id, image.href)}
                  alt={image.alt ?? ""}
                  loading="lazy"
                  className="mx-auto my-6 block h-auto max-w-full"
                />
              ))}
              {block.text && readAlongBlock === index && readAlongSpan ? (
                <p className="my-[0.85em]">
                  {block.text.slice(0, readAlongSpan[0])}
                  <mark
                    className="rounded-sm text-inherit"
                    style={{ background: `${surface.fg}26`, color: "inherit" }}
                  >
                    {block.text.slice(readAlongSpan[0], readAlongSpan[1])}
                  </mark>
                  {block.text.slice(readAlongSpan[1])}
                </p>
              ) : (
                block.text && <p className="my-[0.85em]">{block.text}</p>
              )}
            </div>
          ))}
        </div>

        {display.data && blocks.length === 0 && (
          <p className="py-10 text-center text-sm" style={{ color: surface.muted }}>
            This section has no text — usually a cover or a plate page.
          </p>
        )}

        <div
          className="mt-10 flex items-center justify-between gap-3 border-t pt-6 text-sm"
          style={{ borderColor: surface.rule }}
        >
          <ChapterStep surface={surface} chapter={previous} onPick={goToChapter} direction="back" />
          <ChapterStep surface={surface} chapter={next} onPick={goToChapter} direction="forward" />
        </div>
      </div>

      <ReadAlongTicker
        active={followingLive && !readAlongDead}
        entryId={entry.id}
        onOffset={followTape}
      />

      <Dialog open={handoff !== null} onClose={() => setHandoff(null)} label="Continue in audio">
        {handoff?.audio && (
          <>
            <h2 className="text-lg font-semibold text-ink-100">Continue in audio?</h2>
            <p className="mt-2 text-sm text-ink-400">
              The audiobook starts where this paragraph is:
            </p>
            {handoffPassage && (
              <blockquote
                className="mt-4 border-l-2 border-line pl-4 text-[15px] leading-relaxed text-ink-200"
                style={{ fontFamily: FACES.serif.stack }}
              >
                {handoffPassage}
              </blockquote>
            )}
            <div className="mt-4 flex flex-wrap items-baseline gap-x-2 text-sm">
              <span className="text-ink-400">starting at</span>
              <span className="font-display text-base tracking-wider text-ink-100">
                {formatTimecode(handoff.audio.seconds)}
              </span>
              {handoff.chapter && (
                <span className="text-ink-500">
                  ·{" "}
                  {handoff.chapter.title.trim() ||
                    `Section ${handoff.chapter.spine_index + 1}`}
                </span>
              )}
            </div>
            {handoff.alignment?.state === "low_confidence" ? (
              <p className="mt-4 flex items-start gap-2 text-xs leading-relaxed text-amber-300">
                <Gi name="gauge" className="mt-px size-3.5 shrink-0" />
                <span>
                  This audiobook doesn't match this ebook closely enough — it may be abridged or a
                  different translation — so this starting point may be off.
                </span>
              </p>
            ) : handoff.audio.anchor_distance > 0 ? (
              <p className="mt-4 text-xs leading-relaxed text-ink-500">
                Estimated between the alignment's fixed points, so the first words may land a
                sentence or two from where you stopped.
              </p>
            ) : null}
            <div className="mt-6 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setHandoff(null)}>
                Cancel
              </Button>
              <Button
                variant="primary"
                loading={startListening.isPending}
                onClick={() => startListening.mutate()}
              >
                <Gi name="headphones" className="size-3.5" />
                Start listening
              </Button>
            </div>
          </>
        )}
      </Dialog>
    </div>
  );
}

type Surface = (typeof SURFACES)[SurfaceName];

/**
 * A toolbar toggle. It borrows the reading surface's colours rather than the
 * app's ink ramp, because it sits on the paper rather than on a panel.
 */
function ToolbarButton({
  surface,
  active,
  label,
  icon,
  onClick,
}: {
  surface: Surface;
  active: boolean;
  label: string;
  icon: "list-tree" | "sliders";
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      aria-pressed={active}
      title={label}
      className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg transition-opacity hover:opacity-70 focus-visible:focus-ring"
      style={{
        color: active ? surface.fg : surface.muted,
        background: active ? surface.rule : "transparent",
      }}
    >
      <Gi name={icon} className="size-4" />
    </button>
  );
}

/** The TOC, indented by the depth the book's own nav document declared. */
function Contents({
  chapters,
  current,
  surface,
  onPick,
}: {
  chapters: TextChapter[];
  current: number | null;
  surface: Surface;
  onPick: (chapter: TextChapter) => void;
}) {
  if (chapters.length === 0) {
    return (
      <p className="mx-auto max-w-5xl px-4 pb-4 text-sm sm:px-6" style={{ color: surface.muted }}>
        This book's spine has no readable sections.
      </p>
    );
  }

  return (
    <nav
      className="mx-auto max-h-[60vh] max-w-5xl overflow-y-auto border-t px-4 py-2 sm:px-6"
      style={{ borderColor: surface.rule }}
    >
      {chapters.map((chapter) => (
        <button
          key={chapter.spine_index}
          type="button"
          onClick={() => onPick(chapter)}
          className={cn(
            "block w-full truncate rounded-lg py-1.5 text-left text-sm transition-opacity hover:opacity-70 focus-visible:focus-ring",
            chapter.spine_index === current && "font-semibold",
          )}
          style={{
            paddingLeft: `${Math.min(chapter.depth, 4) * 0.9}rem`,
            color: chapter.spine_index === current ? surface.fg : surface.muted,
          }}
        >
          {chapterTitle(chapter)}
        </button>
      ))}
    </nav>
  );
}

/** Font size, line height, measure, face and paper. */
function TypeControls({
  prefs,
  onChange,
  surface,
}: {
  prefs: ReaderPrefs;
  onChange: (next: ReaderPrefs) => void;
  surface: Surface;
}) {
  const set = <K extends keyof ReaderPrefs>(key: K, value: ReaderPrefs[K]) =>
    onChange({ ...prefs, [key]: value });

  return (
    <div
      className="mx-auto max-w-5xl space-y-3 border-t px-4 py-3 text-sm sm:px-6"
      style={{ borderColor: surface.rule, color: surface.muted }}
    >
      <Stepper
        label="Text size"
        value={`${prefs.fontSize}px`}
        surface={surface}
        onDown={() => set("fontSize", Math.max(14, prefs.fontSize - 1))}
        onUp={() => set("fontSize", Math.min(28, prefs.fontSize + 1))}
      />
      <Stepper
        label="Line height"
        value={prefs.lineHeight.toFixed(2)}
        surface={surface}
        onDown={() => set("lineHeight", Math.max(1.3, round2(prefs.lineHeight - 0.05)))}
        onUp={() => set("lineHeight", Math.min(2.2, round2(prefs.lineHeight + 0.05)))}
      />
      <Stepper
        label="Margins"
        value={`${prefs.measure}rem`}
        surface={surface}
        // Wider margins are a narrower column: the measure is the thing
        // that matters to reading, so it is the thing being set.
        onDown={() => set("measure", Math.min(52, prefs.measure + 2))}
        onUp={() => set("measure", Math.max(24, prefs.measure - 2))}
      />
      <Choice
        label="Face"
        surface={surface}
        options={(Object.keys(FACES) as FaceName[]).map((name) => ({
          value: name,
          label: FACES[name].label,
        }))}
        value={prefs.face}
        onChange={(value) => set("face", value)}
      />
      <Choice
        label="Paper"
        surface={surface}
        options={(Object.keys(SURFACES) as SurfaceName[]).map((name) => ({
          value: name,
          label: SURFACES[name].label,
        }))}
        value={prefs.surface}
        onChange={(value) => set("surface", value)}
      />
    </div>
  );
}

function Stepper({
  label,
  value,
  surface,
  onDown,
  onUp,
}: {
  label: string;
  value: string;
  surface: Surface;
  onDown: () => void;
  onUp: () => void;
}) {
  return (
    <div className="flex items-center gap-3">
      <span className="w-24 shrink-0">{label}</span>
      <button
        type="button"
        aria-label={`Decrease ${label.toLowerCase()}`}
        onClick={onDown}
        className="inline-flex size-7 items-center justify-center rounded-lg focus-visible:focus-ring"
        style={{ background: surface.rule, color: surface.fg }}
      >
        <Gi name="minus" className="size-3.5" />
      </button>
      <span className="w-16 text-center tabular-nums" style={{ color: surface.fg }}>
        {value}
      </span>
      <button
        type="button"
        aria-label={`Increase ${label.toLowerCase()}`}
        onClick={onUp}
        className="inline-flex size-7 items-center justify-center rounded-lg focus-visible:focus-ring"
        style={{ background: surface.rule, color: surface.fg }}
      >
        <Gi name="plus" className="size-3.5" />
      </button>
    </div>
  );
}

function Choice<T extends string>({
  label,
  options,
  value,
  surface,
  onChange,
}: {
  label: string;
  options: { value: T; label: string }[];
  value: T;
  surface: Surface;
  onChange: (value: T) => void;
}) {
  return (
    <div className="flex items-center gap-3">
      <span className="w-24 shrink-0">{label}</span>
      <div className="flex gap-1.5">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => onChange(option.value)}
            aria-pressed={option.value === value}
            className="rounded-lg px-2.5 py-1 text-xs transition-opacity hover:opacity-80 focus-visible:focus-ring"
            style={{
              background: option.value === value ? surface.rule : "transparent",
              color: option.value === value ? surface.fg : surface.muted,
            }}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function ChapterStep({
  chapter,
  surface,
  direction,
  onPick,
}: {
  chapter: TextChapter | null;
  surface: Surface;
  direction: "back" | "forward";
  onPick: (chapter: TextChapter) => void;
}) {
  if (!chapter) return <span />;
  return (
    <button
      type="button"
      onClick={() => onPick(chapter)}
      className={cn(
        "inline-flex min-w-0 items-center gap-1.5 rounded-lg transition-opacity hover:opacity-70 focus-visible:focus-ring",
        direction === "forward" && "ml-auto flex-row-reverse text-right",
      )}
      style={{ color: surface.muted }}
    >
      <Gi name="arrow-left" className={cn("size-4 shrink-0", direction === "forward" && "rotate-180")} />
      <span className="truncate">{chapterTitle(chapter)}</span>
    </button>
  );
}

/**
 * Why there is no text. The three reasons are genuinely different problems
 * with genuinely different fixes, so they get genuinely different words.
 */
function ReaderProblem({ entry, error }: { entry: BookEntry; error: unknown }) {
  const status = error instanceof ApiError ? error.status : 0;
  const message = error instanceof Error ? error.message : "Something went wrong.";

  if (status === 404) {
    return (
      <EmptyState
        icon={<Gi name="book-pile" className="size-7" />}
        title="No ebook attached"
        description="This book has no EPUB attached yet, so there is nothing to read here. Attach one from the scanned files and the reader opens."
        action={
          <Link to="/books/files">
            <Button variant="primary">
              <Gi name="full-folder" className="size-3.5" />
              Book files
            </Button>
          </Link>
        }
      />
    );
  }

  if (status === 422) {
    return (
      <EmptyState
        icon={<Gi name="lock" className="size-7" />}
        title="This EPUB is DRM-protected"
        description="Backhog does not break DRM. A DRM-free copy of the same book will open here without any further setup."
        action={
          <Link to={`/books/${entry.id}`}>
            <Button variant="secondary">Back to the book</Button>
          </Link>
        }
      />
    );
  }

  return (
    <EmptyState
      icon={<Gi name="x-circle" className="size-7" />}
      title="Couldn't open this book"
      description={message}
      action={
        <Link to={`/books/${entry.id}`}>
          <Button variant="secondary">Back to the book</Button>
        </Link>
      }
    />
  );
}

function ReaderSkeleton() {
  return (
    <div className="mx-auto max-w-2xl px-6 py-10">
      <Skeleton className="h-6 w-1/3" />
      <div className="mt-8 space-y-3">
        {Array.from({ length: 12 }, (_, index) => (
          <Skeleton key={index} className={index % 4 === 3 ? "h-4 w-8/12" : "h-4 w-full"} />
        ))}
      </div>
    </div>
  );
}

function round2(value: number): number {
  return Math.round(value * 100) / 100;
}

/**
 * Asks where the tape is, at the tape's pace. It lives in its own component
 * because the audio clock ticks four times a second and the reader must not:
 * this leaf subscribes, throttles to one speculative lookup per couple of
 * seconds of tape, and the page it serves re-renders only when the answer
 * actually moves. The lookup itself is the server's translation — the reader
 * never invents a mapping of its own — and the first "no alignment" answer
 * stands down the polling for the rest of the session.
 */
function ReadAlongTicker({
  active,
  entryId,
  onOffset,
}: {
  active: boolean;
  entryId: string;
  onOffset: (offset: number | null) => void;
}) {
  const clock = useAudioClock();
  const lastRef = useRef(-Infinity);

  useEffect(() => {
    if (!active) return;
    if (Math.abs(clock - lastRef.current) < 2) return;
    lastRef.current = clock;
    let cancelled = false;
    api
      .translateBookPosition(entryId, { audio: clock })
      .then((translation) => {
        if (!cancelled) onOffset(translation.char_offset);
      })
      .catch(() => {
        if (!cancelled) onOffset(null);
      });
    return () => {
      cancelled = true;
    };
  }, [active, clock, entryId, onOffset]);

  return null;
}
