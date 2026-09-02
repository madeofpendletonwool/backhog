import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { api, audioTrackUrl, beaconBookPosition, bookCoverUrl } from "@/lib/api";
import { byline } from "@/lib/format";
import type { AudioTimeline, BookEntry, BookPosition } from "@/lib/types";
import { usePersistentState } from "./usePersistentState";

/**
 * Backhog's audiobook engine.
 *
 * The reading/listening handoff only works if Backhog owns playback, so this
 * is the player rather than a link to one. Three decisions shape everything
 * below:
 *
 * 1. **One element, global seconds.** A book is N files that must behave like
 *    one tape. A single `<audio>` is fed from the timeline, and every number
 *    that leaves this module — the seek bar, the timecodes, the OS scrubber,
 *    the position write — is a second from 0 to the whole book's length.
 *    Track boundaries exist in `trackAt` and `loadTrack` and nowhere else.
 * 2. **It lives above the router.** The provider is mounted outside the
 *    routed outlet, so navigating from the book to the shelf to settings
 *    never touches the element that is making sound.
 * 3. **The clock is its own context.** Position ticks four times a second;
 *    everything else changes a handful of times a session. Splitting them
 *    means a running player does not re-render the page behind it.
 */

/** Playback speeds. Audiobooks live between "a bit slow" and "absurd". */
export const RATES = [0.75, 1, 1.25, 1.5, 1.75, 2, 2.5, 3] as const;

/** The sleep timer's ready-made durations, in minutes. */
export const SLEEP_MINUTES = [5, 15, 30, 45, 60] as const;

/** How often a running player checkpoints its position. */
const WRITE_EVERY_MS = 15_000;

/**
 * How early the next track's opening bytes are warmed, and how many. A track
 * change is the one moment a listener can hear the seams, so the next file's
 * head is pulled into the HTTP cache while the current one is still playing.
 * Best-effort: the response is `immutable`, but whether a browser reuses a
 * cached range for a media element is the browser's business, not ours.
 */
const WARM_WITHIN_SECONDS = 25;
const WARM_BYTES = 512 * 1024;

/** The one message a listener whose NAS went away should ever see. */
const OFFLINE_MESSAGE =
  "Backhog can't reach this audiobook's files. The drive they live on is probably offline.";

/**
 * A sleep timer. `minutes` is carried alongside `endsAt` purely so the UI can
 * say which button you pressed — deriving it back out of a deadline would
 * drift by a second the moment the timer started running.
 */
export type SleepTimer =
  | { kind: "duration"; minutes: number; endsAt: number }
  | { kind: "chapter" }
  | null;

export interface AudioPlayerState {
  /** The book being listened to, or null when nothing is loaded. */
  entry: BookEntry | null;
  timeline: AudioTimeline | null;
  /** Index into `timeline.tracks` of the file currently in the element. */
  trackIndex: number;
  /** The whole book's length in seconds. */
  duration: number;
  playing: boolean;
  /** Buffering, or waiting on the timeline: the transport is not dead. */
  loading: boolean;
  error: string | null;
  rate: number;
  sleep: SleepTimer;
  expanded: boolean;
}

export interface AudioPlayerActions {
  /** Loads a book and (by default) starts it from its stored position. */
  open: (entry: BookEntry, options?: { autoplay?: boolean; startAt?: number }) => void;
  close: () => void;
  play: () => void;
  pause: () => void;
  toggle: () => void;
  /** Seek in global seconds — the bar is the whole book, not one file. */
  seek: (global: number) => void;
  skip: (delta: number) => void;
  nextTrack: () => void;
  previousTrack: () => void;
  jumpToTrack: (index: number) => void;
  /** Refetch the timeline and try the current position again. */
  reload: () => void;
  setRate: (rate: number) => void;
  setSleep: (sleep: SleepTimer) => void;
  setExpanded: (expanded: boolean) => void;
}

const StateContext = createContext<(AudioPlayerState & AudioPlayerActions) | null>(null);
const ClockContext = createContext(0);

const EMPTY_STATE: AudioPlayerState = {
  entry: null,
  timeline: null,
  trackIndex: -1,
  duration: 0,
  playing: false,
  loading: false,
  error: null,
  rate: 1,
  sleep: null,
  expanded: false,
};

/**
 * Which track owns a global second. The mirror of the server's `Timeline.
 * Locate`: tracks own [global_start, global_start + duration), so a boundary
 * second belongs to the track that is starting, and an unmeasured track owns
 * no time at all — it holds its place in the running order and nothing else.
 */
export function trackAt(timeline: AudioTimeline, global: number): number {
  const { tracks } = timeline;
  for (let i = 0; i < tracks.length; i += 1) {
    const track = tracks[i];
    if (track.duration_seconds > 0 && global < track.global_start + track.duration_seconds) {
      return i;
    }
  }
  // Past the end, or a timeline where nothing could be measured: the last
  // track that holds any time, else simply the first file.
  for (let i = tracks.length - 1; i >= 0; i -= 1) {
    if (tracks[i].duration_seconds > 0) return i;
  }
  return tracks.length > 0 ? 0 : -1;
}

export function AudioPlayerProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const audioRef = useRef<HTMLAudioElement>(null);
  const [state, setState] = useState<AudioPlayerState>(EMPTY_STATE);
  const [clock, setClock] = useState(0);

  // Speed is a preference, not a per-book setting: whoever listens at 1.75x
  // listens at 1.75x to the next one too.
  const [rate, storeRate] = usePersistentState("backhog:audio-rate", 1);

  /* Everything the media element's own handlers need, held in refs. The
     listeners are attached once for the life of the app, so a value read
     through a closure would be the one that was current when the player was
     first mounted. */
  const entryIdRef = useRef<string | null>(null);
  const timelineRef = useRef<AudioTimeline | null>(null);
  const trackIndexRef = useRef(-1);
  const globalRef = useRef(0);
  const pendingOffsetRef = useRef<number | null>(null);
  const autoplayRef = useRef(false);
  const startedRef = useRef(false);
  const sleepRef = useRef<SleepTimer>(null);
  const lastWriteRef = useRef({ at: 0, global: -1 });
  const lastPositionStateRef = useRef(0);
  const warmedRef = useRef(new Set<number>());
  const rateRef = useRef(rate);

  const patch = useCallback((next: Partial<AudioPlayerState>) => {
    setState((previous) => ({ ...previous, ...next }));
  }, []);

  // --- position writes ---------------------------------------------------

  /**
   * The write payload for wherever the player is now. The API takes a
   * timestamp *inside one track* plus the file it was measured in, because a
   * track-relative position survives re-measuring or re-ordering the timeline
   * in a way a global second would not. Translating it into a character
   * offset is the server's call, not ours.
   */
  const positionWrite = useCallback(() => {
    const timeline = timelineRef.current;
    const track = timeline?.tracks[trackIndexRef.current];
    if (!track) return null;
    const offset = Math.max(0, globalRef.current - track.global_start);
    return {
      audio_file_id: track.id,
      audio_seconds: track.measured ? Math.min(offset, track.duration_seconds) : offset,
    };
  }, []);

  /**
   * Checkpoint the position. Called on a 15-second throttle while playing and
   * outright on pause, track change, close and unload — never on every
   * `timeupdate`, which would be four writes a second per listener.
   */
  const flush = useCallback(
    (options: { beacon?: boolean; force?: boolean } = {}) => {
      const entryId = entryIdRef.current;
      const write = positionWrite();
      if (!entryId || !write) return;
      // A second of drift is not worth a round trip; a forced write still
      // goes, so pausing twice in the same spot is not silently dropped.
      if (!options.force && Math.abs(globalRef.current - lastWriteRef.current.global) < 1) return;
      lastWriteRef.current = { at: Date.now(), global: globalRef.current };

      if (options.beacon) {
        beaconBookPosition(entryId, write);
        return;
      }
      api
        .putBookPosition(entryId, write)
        .then((result) => {
          queryClient.setQueryData(["bookPosition", entryId], result.position);
          if (result.status_changed) {
            // Finishing a book moves it out of the reading queue and into the
            // stats, the same way finishing a game does.
            for (const key of ["library", "books", "queue", "stats", "bookStats", "entry"]) {
              queryClient.invalidateQueries({ queryKey: [key] });
            }
          }
        })
        .catch(() => {
          // A dropped checkpoint is not worth interrupting playback for; the
          // next one is fifteen seconds away.
        });
    },
    [positionWrite, queryClient],
  );

  // --- the element -------------------------------------------------------

  /**
   * Points the element at one track. Seeking inside the file already loaded
   * is a `currentTime` write and nothing else — only a genuine track change
   * touches `src`, because reassigning it restarts the download.
   */
  const loadTrack = useCallback(
    (index: number, offset: number, play: boolean) => {
      const audio = audioRef.current;
      const timeline = timelineRef.current;
      const entryId = entryIdRef.current;
      const track = timeline?.tracks[index];
      if (!audio || !timeline || !entryId || !track) return;

      globalRef.current = track.global_start + Math.max(0, offset);
      setClock(globalRef.current);

      if (track.missing) {
        // The scanner already knows this file is not where it was. Say so
        // rather than handing the element a URL that will 404.
        audio.pause();
        patch({ playing: false, loading: false, error: OFFLINE_MESSAGE, trackIndex: index });
        trackIndexRef.current = index;
        return;
      }

      if (trackIndexRef.current === index && audio.src) {
        audio.currentTime = Math.max(0, offset);
        if (play && audio.paused) void audio.play().catch(() => undefined);
        return;
      }

      trackIndexRef.current = index;
      pendingOffsetRef.current = offset > 0.5 ? offset : null;
      autoplayRef.current = play;
      patch({ trackIndex: index, loading: true, error: null });

      /* The media fragment gets the element to open at the right byte range
         instead of downloading from zero and seeking, which is the
         difference between resuming instantly and resuming after a 400MB
         file has thought about it. `loadedmetadata` still corrects the
         position, for the browsers that ignore the fragment. */
      const url = audioTrackUrl(entryId, track.id);
      audio.src = pendingOffsetRef.current ? `${url}#t=${pendingOffsetRef.current.toFixed(2)}` : url;
      audio.load();
      if (play) void audio.play().catch(() => undefined);
    },
    [patch],
  );

  /** Starts a freshly loaded book at `resume` global seconds. */
  const start = useCallback(
    (timeline: AudioTimeline, resume: number, play: boolean) => {
      timelineRef.current = timeline;
      startedRef.current = true;
      warmedRef.current.clear();
      trackIndexRef.current = -1;

      const clamped = Math.min(Math.max(0, resume), timeline.total_duration);
      // Resuming is not a position change; recording it would rewrite the
      // same number back at the server on the first pause.
      lastWriteRef.current = { at: Date.now(), global: clamped };

      const index = trackAt(timeline, clamped);
      patch({
        timeline,
        duration: timeline.total_duration,
        loading: false,
        error:
          index < 0
            ? "This book has an audiobook attached, but none of its files are readable."
            : null,
      });
      if (index < 0) return;
      loadTrack(index, clamped - timeline.tracks[index].global_start, play);
    },
    [loadTrack, patch],
  );

  const play = useCallback(() => {
    void audioRef.current?.play().catch(() => {
      patch({ playing: false });
    });
  }, [patch]);

  const pause = useCallback(() => {
    audioRef.current?.pause();
  }, []);

  const toggle = useCallback(() => {
    const audio = audioRef.current;
    if (!audio) return;
    if (audio.paused) play();
    else audio.pause();
  }, [play]);

  const seek = useCallback(
    (global: number) => {
      const timeline = timelineRef.current;
      if (!timeline) return;
      const clamped = Math.min(Math.max(0, global), timeline.total_duration);
      const index = trackAt(timeline, clamped);
      if (index < 0) return;
      const wasPlaying = !(audioRef.current?.paused ?? true);
      loadTrack(index, clamped - timeline.tracks[index].global_start, wasPlaying);
    },
    [loadTrack],
  );

  const skip = useCallback(
    (delta: number) => {
      seek(globalRef.current + delta);
    },
    [seek],
  );

  /**
   * Try the current track again after it failed. The timeline is refetched
   * first: a file the scanner marked missing stays missing in our copy until
   * the NAS is walked again, so retrying the URL alone would fail the same
   * way for the same reason.
   */
  const reload = useCallback(() => {
    const entryId = entryIdRef.current;
    if (!entryId) return;
    const resume = globalRef.current;
    patch({ loading: true, error: null });
    queryClient
      .fetchQuery({
        queryKey: ["bookAudio", entryId],
        queryFn: () => api.bookAudio(entryId),
        staleTime: 0,
      })
      .then((timeline) => {
        if (entryIdRef.current !== entryId) return;
        start(timeline, resume, true);
      })
      .catch((error: unknown) => {
        if (entryIdRef.current !== entryId) return;
        patch({ loading: false, error: describe(error) });
      });
  }, [patch, queryClient, start]);

  const jumpToTrack = useCallback(
    (index: number) => {
      const timeline = timelineRef.current;
      if (!timeline?.tracks[index]) return;
      loadTrack(index, 0, true);
    },
    [loadTrack],
  );

  const nextTrack = useCallback(() => {
    const timeline = timelineRef.current;
    const next = trackIndexRef.current + 1;
    if (!timeline || next >= timeline.tracks.length) return;
    loadTrack(next, 0, !(audioRef.current?.paused ?? true));
  }, [loadTrack]);

  /** Restarts the current track unless you are barely into it. */
  const previousTrack = useCallback(() => {
    const timeline = timelineRef.current;
    const index = trackIndexRef.current;
    const track = timeline?.tracks[index];
    if (!timeline || !track) return;
    const into = globalRef.current - track.global_start;
    const target = into > 3 || index === 0 ? index : index - 1;
    loadTrack(target, 0, !(audioRef.current?.paused ?? true));
  }, [loadTrack]);

  const setRate = useCallback(
    (next: number) => {
      const clamped = Math.min(3, Math.max(0.75, next));
      storeRate(clamped);
      if (audioRef.current) audioRef.current.playbackRate = clamped;
      patch({ rate: clamped });
    },
    [patch, storeRate],
  );

  const setSleep = useCallback(
    (sleep: SleepTimer) => {
      sleepRef.current = sleep;
      patch({ sleep });
    },
    [patch],
  );

  const setExpanded = useCallback(
    (expanded: boolean) => {
      patch({ expanded });
    },
    [patch],
  );

  // --- opening and closing ------------------------------------------------

  const close = useCallback(() => {
    flush({ force: true });
    const audio = audioRef.current;
    if (audio) {
      audio.pause();
      audio.removeAttribute("src");
      audio.load();
    }
    entryIdRef.current = null;
    timelineRef.current = null;
    trackIndexRef.current = -1;
    globalRef.current = 0;
    startedRef.current = false;
    sleepRef.current = null;
    lastWriteRef.current = { at: 0, global: -1 };
    warmedRef.current.clear();
    setClock(0);
    setState({ ...EMPTY_STATE, rate });
  }, [flush, rate]);

  const open = useCallback(
    (entry: BookEntry, options: { autoplay?: boolean; startAt?: number } = {}) => {
      const autoplay = options.autoplay ?? true;

      // Already loaded: this is the "open the player" gesture, not a reload.
      if (entryIdRef.current === entry.id) {
        patch({ entry, expanded: true });
        if (autoplay && (audioRef.current?.paused ?? true)) play();
        return;
      }

      flush({ force: true });
      entryIdRef.current = entry.id;
      timelineRef.current = null;
      trackIndexRef.current = -1;
      globalRef.current = 0;
      startedRef.current = false;
      sleepRef.current = null;
      lastWriteRef.current = { at: 0, global: -1 };
      warmedRef.current.clear();
      setClock(0);
      setState({
        ...EMPTY_STATE,
        entry,
        rate,
        loading: true,
        expanded: false,
      });

      /* When the page that offered the button has already fetched the
         timeline and the position — which the book page has — the element
         can be handed a src inside the click itself. That matters on iOS,
         where a `play()` that has drifted out of the tap's own task is
         refused, and the listener has to press play a second time. */
      const cachedTimeline = queryClient.getQueryData<AudioTimeline>(["bookAudio", entry.id]);
      const cachedPosition = queryClient.getQueryData<BookPosition>(["bookPosition", entry.id]);
      if (cachedTimeline) {
        start(cachedTimeline, options.startAt ?? cachedPosition?.audio?.seconds ?? 0, autoplay);
      }

      void hydrate(queryClient, entry.id)
        .then(({ timeline, position }) => {
          if (entryIdRef.current !== entry.id) return; // switched books mid-flight
          if (startedRef.current) {
            // Already playing from the cached copy; only the timeline itself
            // can have moved on, and re-seeking would stutter the audio.
            timelineRef.current = timeline;
            patch({ timeline, duration: timeline.total_duration });
            return;
          }
          start(timeline, options.startAt ?? position?.audio?.seconds ?? 0, autoplay);
        })
        .catch((error: unknown) => {
          if (entryIdRef.current !== entry.id || startedRef.current) return;
          patch({ loading: false, error: describe(error) });
        });
    },
    [flush, patch, play, queryClient, rate, start],
  );

  /** Pulls the next file's opening bytes into the HTTP cache. Best-effort. */
  const warmNextTrack = useCallback((timeline: AudioTimeline, index: number) => {
    const entryId = entryIdRef.current;
    const next = timeline.tracks[index + 1];
    if (!entryId || !next || next.missing || warmedRef.current.has(next.id)) return;
    warmedRef.current.add(next.id);
    void fetch(audioTrackUrl(entryId, next.id), {
      credentials: "include",
      headers: { Range: `bytes=0-${WARM_BYTES - 1}` },
    })
      .then((response) => response.blob())
      .catch(() => undefined);
  }, []);

  /**
   * The lock screen, the car head unit and every Bluetooth remote talk to the
   * Media Session, not to the page. Metadata without action handlers gets a
   * pretty notification with dead buttons, so both are set together.
   */
  const publishPositionState = useCallback(() => {
    const session = navigator.mediaSession;
    const timeline = timelineRef.current;
    if (!session?.setPositionState || !timeline || timeline.total_duration <= 0) return;
    lastPositionStateRef.current = Date.now();
    try {
      session.setPositionState({
        duration: timeline.total_duration,
        // The OS scrubber is the whole book. A per-file one would make "how
        // far into this book am I" unanswerable from the car.
        position: Math.min(Math.max(0, globalRef.current), timeline.total_duration),
        playbackRate: rateRef.current,
      });
    } catch {
      // Some engines reject a state they consider inconsistent mid-seek.
    }
  }, []);

  // --- the element's own events -------------------------------------------

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio) return;

    const onLoadedMetadata = () => {
      const pending = pendingOffsetRef.current;
      pendingOffsetRef.current = null;
      // The media fragment usually landed us here already; correct it only
      // when it plainly did not, so a fresh load is not a load plus a seek.
      if (pending != null && Math.abs(audio.currentTime - pending) > 1) {
        audio.currentTime = pending;
      }
      audio.playbackRate = rateRef.current;
      patch({ loading: false, error: null });
      if (autoplayRef.current && audio.paused) void audio.play().catch(() => undefined);
      autoplayRef.current = false;
    };

    const onTimeUpdate = () => {
      const timeline = timelineRef.current;
      const track = timeline?.tracks[trackIndexRef.current];
      if (!timeline || !track) return;

      globalRef.current = track.global_start + audio.currentTime;
      setClock(globalRef.current);

      if (!audio.paused) {
        if (Date.now() - lastWriteRef.current.at >= WRITE_EVERY_MS) flush();
        if (Date.now() - lastPositionStateRef.current >= 5000) publishPositionState();

        const remaining = track.duration_seconds - audio.currentTime;
        if (track.measured && remaining > 0 && remaining < WARM_WITHIN_SECONDS) {
          warmNextTrack(timeline, trackIndexRef.current);
        }
      }
    };

    const onPlay = () => {
      patch({ playing: true, error: null });
      setPlaybackState("playing");
      publishPositionState();
    };

    const onPause = () => {
      patch({ playing: false });
      setPlaybackState("paused");
      publishPositionState();
      flush();
    };

    const onEnded = () => {
      const timeline = timelineRef.current;
      if (!timeline) return;
      const next = trackIndexRef.current + 1;
      const sleeping = sleepRef.current?.kind === "chapter";

      if (next >= timeline.tracks.length) {
        // The end of the book: hold at the last second rather than snapping
        // back to zero, and checkpoint it so reopening says "finished".
        globalRef.current = timeline.total_duration;
        setClock(globalRef.current);
        patch({ playing: false });
        flush({ force: true });
        return;
      }
      // "End of chapter" means stop at this boundary, ready for next time.
      loadTrack(next, 0, !sleeping);
      if (sleeping) setSleep(null);
      flush({ force: true });
    };

    const onError = () => {
      const track = timelineRef.current?.tracks[trackIndexRef.current];
      patch({
        playing: false,
        loading: false,
        error: track ? OFFLINE_MESSAGE : "This track could not be played.",
      });
    };

    const onWaiting = () => patch({ loading: true });
    const onPlaying = () => patch({ loading: false });

    audio.addEventListener("loadedmetadata", onLoadedMetadata);
    audio.addEventListener("timeupdate", onTimeUpdate);
    audio.addEventListener("play", onPlay);
    audio.addEventListener("pause", onPause);
    audio.addEventListener("ended", onEnded);
    audio.addEventListener("error", onError);
    audio.addEventListener("waiting", onWaiting);
    audio.addEventListener("playing", onPlaying);
    return () => {
      audio.removeEventListener("loadedmetadata", onLoadedMetadata);
      audio.removeEventListener("timeupdate", onTimeUpdate);
      audio.removeEventListener("play", onPlay);
      audio.removeEventListener("pause", onPause);
      audio.removeEventListener("ended", onEnded);
      audio.removeEventListener("error", onError);
      audio.removeEventListener("waiting", onWaiting);
      audio.removeEventListener("playing", onPlaying);
    };
    // Every dependency here is a stable callback; the listeners are attached
    // once and read every live value through a ref.
  }, [flush, loadTrack, patch, publishPositionState, setSleep, warmNextTrack]);

  useEffect(() => {
    rateRef.current = rate;
    if (audioRef.current) {
      audioRef.current.playbackRate = rate;
      // Chipmunk audiobooks are unlistenable; every engine that can correct
      // for speed should.
      audioRef.current.preservesPitch = true;
    }
    patch({ rate });
  }, [patch, rate]);

  // --- the OS's copy of the player ----------------------------------------

  useEffect(() => {
    const session = navigator.mediaSession;
    if (!session) return;
    const book = state.entry?.book;
    // Metadata without action handlers is a pretty notification with dead
    // buttons, so the two are always set together.
    if (!book) {
      session.metadata = null;
      session.playbackState = "none";
      return;
    }

    session.metadata = new MediaMetadata({
      title: book.title,
      artist: byline(book) || "Unknown author",
      album: "Backhog",
      // An absolute URL: the notification is drawn by the OS, which has no
      // idea what our origin is.
      artwork: book.cover_url
        ? [{ src: new URL(bookCoverUrl(book.id), window.location.origin).href, sizes: "512x512" }]
        : [],
    });

    const handlers: [MediaSessionAction, MediaSessionActionHandler][] = [
      ["play", () => play()],
      ["pause", () => pause()],
      ["stop", () => pause()],
      // The offsets a head unit asks for, with the audiobook defaults when it
      // asks for none: back far enough to re-hear a sentence, forward far
      // enough to clear an ad break.
      ["seekbackward", (details) => skip(-(details.seekOffset ?? 30))],
      ["seekforward", (details) => skip(details.seekOffset ?? 15)],
      ["previoustrack", () => previousTrack()],
      ["nexttrack", () => nextTrack()],
      [
        "seekto",
        (details) => {
          if (details.seekTime != null) seek(details.seekTime);
        },
      ],
    ];
    for (const [action, handler] of handlers) {
      try {
        session.setActionHandler(action, handler);
      } catch {
        // An engine that does not know this action; the rest still work.
      }
    }
    return () => {
      for (const [action] of handlers) {
        try {
          session.setActionHandler(action, null);
        } catch {
          // As above.
        }
      }
    };
  }, [nextTrack, pause, play, previousTrack, seek, skip, state.entry]);

  useEffect(() => {
    publishPositionState();
  }, [publishPositionState, state.trackIndex, state.rate, state.duration]);

  // --- leaving ------------------------------------------------------------

  /**
   * The last write of a session. `pagehide` covers a closed tab and a
   * back-navigation; `visibilitychange` covers a phone being locked or the
   * app being swiped away, which on iOS never fires `pagehide` at all.
   */
  useEffect(() => {
    const save = () => flush({ beacon: true, force: true });
    const onVisibility = () => {
      if (document.visibilityState === "hidden") save();
    };
    window.addEventListener("pagehide", save);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("pagehide", save);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [flush]);

  /** The sleep timer, when it is a clock rather than a chapter boundary. */
  useEffect(() => {
    const sleep = state.sleep;
    if (sleep?.kind !== "duration") return;
    const id = window.setInterval(() => {
      if (Date.now() >= sleep.endsAt) {
        pause();
        setSleep(null);
      }
    }, 1000);
    return () => window.clearInterval(id);
  }, [pause, setSleep, state.sleep]);

  const value = useMemo(
    () => ({
      ...state,
      open,
      close,
      play,
      pause,
      toggle,
      seek,
      skip,
      nextTrack,
      previousTrack,
      jumpToTrack,
      reload,
      setRate,
      setSleep,
      setExpanded,
    }),
    [
      close,
      jumpToTrack,
      nextTrack,
      open,
      pause,
      play,
      previousTrack,
      reload,
      seek,
      setExpanded,
      setRate,
      setSleep,
      skip,
      state,
      toggle,
    ],
  );

  return (
    <StateContext.Provider value={value}>
      <ClockContext.Provider value={clock}>
        {/* One element for the whole app, mounted above the router so it
            survives every navigation. */}
        <audio ref={audioRef} preload="metadata" />
        {children}
      </ClockContext.Provider>
    </StateContext.Provider>
  );
}

/** The player's state and controls. Changes a handful of times a session. */
export function useAudioPlayer(): AudioPlayerState & AudioPlayerActions {
  const value = useContext(StateContext);
  if (!value) throw new Error("useAudioPlayer must be used inside AudioPlayerProvider");
  return value;
}

/**
 * The current position in global seconds. Its own context because it ticks
 * four times a second: only the timecodes and the seek bar subscribe, so the
 * page behind the player is not re-rendered by playback.
 */
export function useAudioClock(): number {
  return useContext(ClockContext);
}

/** Fetches (and caches) a book's timeline and stored position together. */
async function hydrate(queryClient: QueryClient, entryId: string) {
  const [timeline, position] = await Promise.all([
    queryClient.fetchQuery({
      queryKey: ["bookAudio", entryId],
      queryFn: () => api.bookAudio(entryId),
      staleTime: 5 * 60 * 1000,
    }),
    // The position is a nicety — a book with no stored position starts at
    // zero, which is not worth failing the whole open for.
    queryClient
      .fetchQuery({ queryKey: ["bookPosition", entryId], queryFn: () => api.bookPosition(entryId) })
      .catch(() => null),
  ]);
  return { timeline, position };
}

function setPlaybackState(playbackState: MediaSessionPlaybackState) {
  if (navigator.mediaSession) navigator.mediaSession.playbackState = playbackState;
}

function describe(error: unknown): string {
  const message = error instanceof Error ? error.message : "";
  return message || "That audiobook could not be loaded.";
}
