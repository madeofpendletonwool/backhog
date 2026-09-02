import type {
  AchievementStatus,
  AudioTimeline,
  Book,
  BookFacets,
  BookPosition,
  BookSearchResult,
  BookStats,
  DebtReport,
  Entry,
  GameList,
  Game,
  GameEntry,
  Insights,
  MediaCandidatesResponse,
  MediaFile,
  MediaScanStatus,
  NamedRef,
  PlayOrder,
  PlaySession,
  Platform,
  PositionWrite,
  PositionWriteResult,
  Project,
  ProjectItem,
  ProjectKind,
  RuleSet,
  SearchResult,
  Season,
  Series,
  SeriesDetail,
  SeriesSummary,
  SmartField,
  Stats,
  Status,
  SteamMatch,
  TonightPicks,
  User,
} from "./types";

/** An API error carrying the HTTP status, so callers can special-case 401. */
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api${path}`, {
    // The session lives in an HttpOnly cookie, so every call must send it.
    credentials: "include",
    headers: init.body ? { "Content-Type": "application/json" } : undefined,
    ...init,
  });

  if (response.status === 204) return undefined as T;

  const text = await response.text();
  const data = text ? JSON.parse(text) : null;

  if (!response.ok) {
    throw new ApiError(response.status, data?.error ?? response.statusText);
  }
  return data as T;
}

const body = (value: unknown) => JSON.stringify(value);

export const api = {
  // --- auth -----------------------------------------------------------
  me: () => request<User>("/auth/me"),

  login: (email: string, password: string) =>
    request<User>("/auth/login", { method: "POST", body: body({ email, password }) }),

  register: (email: string, username: string, password: string) =>
    request<User>("/auth/register", {
      method: "POST",
      body: body({ email, username, password }),
    }),

  logout: () => request<{ ok: boolean }>("/auth/logout", { method: "POST" }),

  changePassword: (current_password: string, new_password: string) =>
    request<{ ok: boolean }>("/auth/password", {
      method: "POST",
      body: body({ current_password, new_password }),
    }),

  // --- games ----------------------------------------------------------
  searchGames: (q: string, signal?: AbortSignal) =>
    request<{ results: SearchResult[] }>(`/games/search?q=${encodeURIComponent(q)}`, { signal }),

  getGame: (id: number) => request<Game>(`/games/${id}`),

  gameSeries: (gameId: number) => request<{ series: Series[] }>(`/games/${gameId}/series`),

  // --- library --------------------------------------------------------
  library: (params: Record<string, string | number | undefined>) => {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== "") query.set(key, String(value));
    }
    return request<{ entries: Entry[]; total: number }>(`/library?${query}`);
  },

  addToLibrary: (game_id: number, status?: Status) =>
    request<Entry>("/library", { method: "POST", body: body({ game_id, status }) }),

  getEntry: (id: string) => request<Entry>(`/library/${id}`),

  entryLists: (id: string) => request<{ list_ids: string[] }>(`/library/${id}/lists`),

  /** The PATCH response also carries any achievements the change unlocked. */
  updateEntry: (id: string, patch: Partial<Record<string, unknown>>) =>
    request<{ entry: Entry; unlocks: AchievementStatus[] }>(`/library/${id}`, {
      method: "PATCH",
      body: body(patch),
    }),

  deleteEntry: (id: string) => request<void>(`/library/${id}`, { method: "DELETE" }),

  stats: () => request<Stats>("/library/stats"),

  debt: () => request<DebtReport>("/library/debt"),

  // --- series ---------------------------------------------------------
  seriesIndex: () => request<{ series: SeriesSummary[] }>("/series"),

  seriesDetail: (id: string) => request<SeriesDetail>(`/series/${id}`),

  setSeriesPlayOrder: (id: string, play_order: PlayOrder) =>
    request<{ ok: boolean }>(`/series/${id}/order`, {
      method: "PUT",
      body: body({ play_order }),
    }),

  reorderSeries: (seriesId: string, game_id: number, before_id: number, after_id: number) =>
    request<{ ok: boolean }>(`/series/${seriesId}/reorder`, {
      method: "POST",
      body: body({ game_id, before_id, after_id }),
    }),

  kickSeriesBackfill: () =>
    request<{ started: boolean }>("/series/backfill", { method: "POST" }),

  seriesBackfillStatus: () =>
    request<{ running: boolean }>("/series/backfill"),

  insights: () => request<Insights>("/library/insights"),

  // --- achievements -----------------------------------------------------
  achievements: () => request<{ achievements: AchievementStatus[] }>("/achievements"),

  /** The "YYYY Backlog Challenge" rollup; defaults to the current year. */
  season: (year?: number) =>
    request<Season>(`/achievements/season${year != null ? `?year=${year}` : ""}`),

  /**
   * Fire an easter egg. Only egg ids are accepted (everything else 400s);
   * the response carries the revealed achievement and whether this call is
   * the one that unlocked it — the toast payload.
   */
  unlockEgg: (id: string) =>
    request<{ unlocked: boolean; achievement: AchievementStatus }>(
      `/achievements/${id}/egg`,
      { method: "POST" },
    ),

  // --- play sessions --------------------------------------------------
  sessions: (entryId: string) =>
    request<{ sessions: PlaySession[] }>(`/library/${entryId}/sessions`),

  /** The response also carries any achievements the session unlocked. */
  addSession: (entryId: string, input: { minutes: number; played_on?: string; note?: string }) =>
    request<{ session: PlaySession; unlocks: AchievementStatus[] }>(
      `/library/${entryId}/sessions`,
      { method: "POST", body: body(input) },
    ),

  deleteSession: (sessionId: string) =>
    request<void>(`/sessions/${sessionId}`, { method: "DELETE" }),

  // --- pick / import --------------------------------------------------
  /** Tonight's picks: four explainable suggestions for a time budget. */
  tonight: (minutes: number, exclude: string[] = []) => {
    const query = new URLSearchParams({ minutes: String(minutes) });
    if (exclude.length > 0) query.set("exclude", exclude.join(","));
    return request<TonightPicks>(`/library/tonight?${query}`);
  },

  /** Game-scoped by contract: the random pick reasons about playtime. */
  pick: (params: { max_hours?: number; min_rating?: number; genre?: number }) => {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value) query.set(key, String(value));
    }
    return request<GameEntry>(`/library/pick?${query}`);
  },

  steamPreview: (steam_id: string) =>
    request<{ steam_id: string; total: number; unmatched: number; matches: SteamMatch[] }>(
      "/import/steam/preview",
      { method: "POST", body: body({ steam_id }) },
    ),

  bulkAdd: (game_ids: number[], status: Status) =>
    request<{ added: number; skipped: number }>("/library/bulk", {
      method: "POST",
      body: body({ game_ids, status }),
    }),

  health: () => request<{ status: string; metadata: boolean; steam: boolean }>("/healthz"),

  facets: () => request<{ platforms: Platform[]; genres: NamedRef[] }>("/library/facets"),

  queue: () => request<{ entries: Entry[] }>("/library/queue"),

  reorderQueue: (entry_id: string, before_id: string, after_id: string) =>
    request<{ ok: boolean }>("/library/reorder", {
      method: "POST",
      body: body({ entry_id, before_id, after_id }),
    }),

  // --- lists ----------------------------------------------------------
  lists: () => request<{ lists: GameList[] }>("/lists"),

  createList: (input: {
    name: string;
    description?: string;
    kind: "manual" | "smart";
    rules?: RuleSet;
  }) => request<GameList>("/lists", { method: "POST", body: body(input) }),

  getList: (id: string) =>
    request<{ list: GameList; entries: Entry[] }>(`/lists/${id}`),

  updateList: (id: string, patch: { name?: string; description?: string; rules?: RuleSet }) =>
    request<GameList>(`/lists/${id}`, { method: "PATCH", body: body(patch) }),

  deleteList: (id: string) => request<void>(`/lists/${id}`, { method: "DELETE" }),

  addListItem: (listId: string, entry_id: string) =>
    request<{ ok: boolean }>(`/lists/${listId}/items`, {
      method: "POST",
      body: body({ entry_id }),
    }),

  removeListItem: (listId: string, entryId: string) =>
    request<void>(`/lists/${listId}/items/${entryId}`, { method: "DELETE" }),

  reorderListItem: (listId: string, entry_id: string, before_id: string, after_id: string) =>
    request<{ ok: boolean }>(`/lists/${listId}/reorder`, {
      method: "POST",
      body: body({ entry_id, before_id, after_id }),
    }),

  smartFields: () => request<{ fields: SmartField[] }>("/lists/fields"),

  // --- projects ---------------------------------------------------------
  projects: () => request<{ projects: Project[] }>("/projects"),

  createProject: (input: {
    name: string;
    description?: string;
    kind: ProjectKind;
    target_count?: number | null;
    rules?: RuleSet;
  }) => request<Project>("/projects", { method: "POST", body: body(input) }),

  getProject: (id: string) =>
    request<{ project: Project; items: ProjectItem[] }>(`/projects/${id}`),

  updateProject: (
    id: string,
    patch: {
      name?: string;
      description?: string;
      target_count?: number | null;
      rules?: RuleSet;
      completed?: boolean;
    },
  ) => request<Project>(`/projects/${id}`, { method: "PATCH", body: body(patch) }),

  deleteProject: (id: string) => request<void>(`/projects/${id}`, { method: "DELETE" }),

  addProjectItem: (projectId: string, entry_id: string) =>
    request<{ ok: boolean }>(`/projects/${projectId}/items`, {
      method: "POST",
      body: body({ entry_id }),
    }),

  removeProjectItem: (projectId: string, entryId: string) =>
    request<void>(`/projects/${projectId}/items/${entryId}`, { method: "DELETE" }),

  setProjectItemDone: (projectId: string, entryId: string, done: boolean | null) =>
    request<{ ok: boolean }>(`/projects/${projectId}/items/${entryId}`, {
      method: "PATCH",
      body: body({ done }),
    }),

  reorderProjectItem: (projectId: string, entry_id: string, before_id: string, after_id: string) =>
    request<{ ok: boolean }>(`/projects/${projectId}/reorder`, {
      method: "POST",
      body: body({ entry_id, before_id, after_id }),
    }),

  /** Which checklist projects a given entry belongs to. */
  entryProjects: (id: string) =>
    request<{ project_ids: string[] }>(`/library/${id}/projects`),

  // --- books / media attach ---------------------------------------------
  searchBooks: (q: string, signal?: AbortSignal) =>
    request<{ results: BookSearchResult[] }>(`/books/search?q=${encodeURIComponent(q)}`, { signal }),

  /** Resolves a barcode or hand-typed ISBN to the work it is a printing of. */
  bookByISBN: (isbn: string, signal?: AbortSignal) =>
    request<Book>(`/books/isbn/${encodeURIComponent(isbn)}`, { signal }),

  /** A work by its Open Library key, editions included. */
  getBook: (bookId: string) => request<Book>(`/books/${bookId}`),

  /**
   * Adds a work. `editionId` names the printing the reader actually owns —
   * page counts and (later) page maps hang off the edition, not the work.
   */
  addBookToLibrary: (
    bookId: string,
    options: { editionId?: string | null; status?: Status } = {},
  ) =>
    request<Entry>("/library", {
      method: "POST",
      body: body({
        book_id: bookId,
        edition_id: options.editionId ?? undefined,
        status: options.status,
      }),
    }),

  bookStats: () => request<BookStats>("/library/stats?media=book"),

  bookFacets: () => request<BookFacets>("/library/facets?media=book"),

  /** The attach review queue: unattached groups with ranked suggestions. */
  mediaCandidates: () => request<MediaCandidatesResponse>("/media/candidates"),

  mediaScanStatus: () => request<MediaScanStatus>("/media/scan"),

  kickMediaScan: () =>
    request<{ started: boolean }>("/media/scan", { method: "POST" }),

  /** file_ids order is the explicit track order for audio. */
  attachFiles: (entryId: string, fileIds: number[], kind: "audio" | "epub") =>
    request<{ attached: number; files: MediaFile[] }>(
      `/books/${entryId}/files`,
      { method: "POST", body: body({ file_ids: fileIds, kind }) },
    ),

  detachFile: (entryId: string, fileId: number) =>
    request<{ detached: boolean }>(`/books/${entryId}/files/${fileId}`, {
      method: "DELETE",
    }),

  bookFiles: (entryId: string) =>
    request<{ files: MediaFile[] }>(`/books/${entryId}/files`),

  ignoreMediaFiles: (fileIds: number[]) =>
    request<{ ignored: number }>("/media/ignore", {
      method: "POST",
      body: body({ file_ids: fileIds }),
    }),

  unignoreMediaFile: (fileId: number) =>
    request<{ ignored: boolean }>(`/media/ignore/${fileId}`, {
      method: "DELETE",
    }),

  // --- audiobook playback -----------------------------------------------
  /** The attached audiobook as one continuous timeline; 404 when there is none. */
  bookAudio: (entryId: string) => request<AudioTimeline>(`/books/${entryId}/audio`),

  bookPosition: (entryId: string) => request<BookPosition>(`/books/${entryId}/position`),

  putBookPosition: (entryId: string, write: PositionWrite) =>
    request<PositionWriteResult>(`/books/${entryId}/position`, {
      method: "PUT",
      body: body(write),
    }),
};

/** Cover images are served by our own API from the local cache. */
export const coverUrl = (gameId: number) => `/api/covers/game/${gameId}`;

/** The books half of the same cache, keyed by Open Library work id. */
export const bookCoverUrl = (bookId: string) => `/api/covers/book/${bookId}`;

/**
 * One track's bytes, range-served. The element loads this same-origin, so the
 * session cookie rides along without a `crossorigin` attribute; adding one
 * would turn every seek into a CORS request against our own API.
 */
export const audioTrackUrl = (entryId: string, trackId: number) =>
  `/api/books/${entryId}/audio/${trackId}`;

/**
 * The last position write of a session, sent while the page is going away.
 *
 * `sendBeacon` is the only write a browser promises to finish after a tab
 * closes or a phone backgrounds the app — but it can only POST, which is why
 * the API accepts POST on the position route as an alias for PUT. Where it is
 * unavailable or refuses the payload, `keepalive` fetch is the fallback and
 * gets to use the real verb.
 */
export function beaconBookPosition(entryId: string, write: PositionWrite): void {
  const url = `/api/books/${entryId}/position`;
  const payload = JSON.stringify(write);

  if (typeof navigator.sendBeacon === "function") {
    const blob = new Blob([payload], { type: "application/json" });
    if (navigator.sendBeacon(url, blob)) return;
  }
  void fetch(url, {
    method: "PUT",
    credentials: "include",
    keepalive: true,
    headers: { "Content-Type": "application/json" },
    body: payload,
  }).catch(() => {
    // The page is unloading; there is no one left to tell.
  });
}
