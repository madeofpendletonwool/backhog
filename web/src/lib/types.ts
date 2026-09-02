export type MediaType = "game" | "book";

export type Status = "backlog" | "playing" | "played" | "dropped" | "ignored" | "wishlist";

/** Every status, for the full picker on the detail page. */
export const STATUSES: Status[] = ["backlog", "playing", "played", "dropped", "ignored", "wishlist"];

/**
 * The statuses surfaced as quick-access controls — the library tabs and the
 * card hover-menu. Wishlist lives on the detail page only: it's a shopping list,
 * not a state you flip games into while browsing your owned library.
 */
export const QUICK_STATUSES: Status[] = ["backlog", "playing", "played", "dropped", "ignored"];

export const STATUS_LABELS: Record<Status, string> = {
  backlog: "Backlog",
  playing: "Playing",
  played: "Played",
  dropped: "Dropped",
  ignored: "Ignored",
  wishlist: "Wishlist",
};

/**
 * The same six states, said the way a reader says them. Status is one column
 * in the database and one set of colours on screen; only the words change,
 * because "Playing" a book and "Played" a book are not English.
 */
export const BOOK_STATUS_LABELS: Record<Status, string> = {
  backlog: "To read",
  playing: "Reading",
  played: "Read",
  dropped: "Abandoned",
  ignored: "Ignored",
  wishlist: "Wishlist",
};

/** The label for a status in the arena it is being shown in. */
export function statusLabel(status: Status, media: MediaType = "game"): string {
  return media === "book" ? BOOK_STATUS_LABELS[status] : STATUS_LABELS[status];
}

export interface NamedRef {
  id: number;
  name: string;
}

/**
 * A platform with its curated classification. Unclassified platforms come
 * back as family "other" with a null generation.
 */
export interface Platform extends NamedRef {
  manufacturer: string;
  family: string;
  generation: number | null;
  handheld: boolean;
}

export interface Game {
  id: number;
  name: string;
  slug: string;
  summary: string;
  cover_url: string;
  accent_hex: string;
  first_release_date: number | null;
  igdb_rating: number | null;
  time_to_beat_main: number | null;
  time_to_beat_complete: number | null;
  genres: NamedRef[];
  platforms: Platform[];
  extras: GameExtras | null;
}

/** A trailer or gameplay video (YouTube). */
export interface GameVideo {
  video_id: string;
  name: string;
}

/** A game referenced by another (similar games, DLC, expansions). */
export interface RelatedGame {
  id: number;
  name: string;
  cover_image_id: string;
}

/** An external link (official site, store page, wiki…), with a labeled kind. */
export interface GameWebsite {
  url: string;
  category: string;
}

/**
 * The richer IGDB metadata shown on the detail page. Everything here is
 * display-only — none of it is filtered or sorted on — which is why the backend
 * stores it as one JSON blob rather than in relational tables. Populated lazily
 * the first time a game's detail page is opened after this feature shipped.
 */
export interface GameExtras {
  developer: string;
  publisher: string;
  storyline: string;
  aggregated_rating: number | null;
  category: string;
  game_modes: string[];
  player_perspectives: string[];
  themes: string[];
  franchise: string;
  collection: string;
  alternative_names: string[];
  age_ratings: string[];
  websites: GameWebsite[];
  screenshot_image_ids: string[];
  videos: GameVideo[];
  similar_games: RelatedGame[];
  dlcs: RelatedGame[];
  expansions: RelatedGame[];
}

/** A book work: "The Hobbit", not any particular printing of it. */
export interface Book {
  id: string;
  title: string;
  authors: string[];
  description: string;
  cover_url: string;
  accent_hex: string;
  first_publish_year: number | null;
  subjects: string[];
  /** The printings cache, present on detail and add responses. */
  editions?: BookEdition[];
}

/** One printing of a work. Page maps key off the edition, never the work. */
export interface BookEdition {
  id: string;
  book_id: string;
  isbn10: string;
  isbn13: string;
  publisher: string;
  published_year: number | null;
  page_count: number | null;
  binding: string;
  language: string;
  cover_url: string;
}

/** Fields every library entry carries, whatever it points at. */
interface EntryFields {
  id: string;
  status: Status;
  platform_id: number | null;
  user_rating: number | null;
  notes: string;
  queue_position: number | null;
  logged_minutes: number;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
}

/** An entry that points at a game. */
export interface GameEntry extends EntryFields {
  media_type: "game";
  game: Game;
  book?: never;
}

/** An entry that points at a book. */
export interface BookEntry extends EntryFields {
  media_type: "book";
  book: Book;
  game?: never;
}

/**
 * One item in one user's library, discriminated on media_type: exactly one of
 * `game` / `book` is set. Touching `.game` without narrowing to the game
 * variant is a compile error, not a runtime undefined.
 */
export type Entry = GameEntry | BookEntry;

export function isGameEntry(entry: Entry): entry is GameEntry {
  return entry.media_type === "game";
}

export function isBookEntry(entry: Entry): entry is BookEntry {
  return entry.media_type === "book";
}

export interface User {
  id: string;
  email: string;
  username: string;
  created_at: string;
}

export interface Stats {
  total: number;
  backlog: number;
  playing: number;
  played: number;
  dropped: number;
  ignored: number;
  wishlist: number;
  backlog_hours: number;
  played_hours: number;
  logged_hours: number;
  completion: number;
}

/** The books counterpart of Stats, for the books library strip. */
export interface BookStats {
  total: number;
  backlog: number;
  reading: number;
  read: number;
  dropped: number;
  ignored: number;
  wishlist: number;
  completion: number;
}

/** The book library's filter rail: authors, subjects, languages, statuses. */
export interface BookFacets {
  authors: string[];
  subjects: string[];
  languages: string[];
  statuses: string[];
}

/** Actual play pace derived from logged sessions. Null means no data. */
export interface DebtPace {
  hours_per_week_90d: number | null;
  hours_per_week_all: number | null;
}

/** When the backlog clears at one fixed hours/week rate. */
export interface ClearanceScenario {
  hours_per_week: number;
  weeks: number;
  /** "2027-03-05"; null means it never clears at this pace. */
  clear_by: string | null;
}

export interface DebtProjection {
  current_pace: ClearanceScenario | null;
  scenarios: ClearanceScenario[];
}

export interface DebtReport {
  total_hours: number;
  main_backlog_hours: number;
  started_hours: number;
  short_games_hours: number;
  /** Null by design: a wishlist is a shopping list, not debt you owe yourself. */
  wishlist_hours: number | null;
  /** Unplayed add-on debt; null until any DLC links are known. */
  dlc_hours: number | null;
  pace: DebtPace;
  projection: DebtProjection;
}

export interface PlaySession {
  id: string;
  entry_id: string;
  played_on: string;
  minutes: number;
  note: string;
  created_at: string;
}

/** One category's answer to "what should I play tonight?". */
export interface TonightPick {
  /** Game-scoped by contract: tonight reasons about time-to-beat and genres. */
  entry: GameEntry;
  score: number;
  reason: string;
}

/** The four-category answer to a time budget; any category may be null. */
export interface TonightPicks {
  continue: TonightPick | null;
  short_win: TonightPick | null;
  wildcard: TonightPick | null;
  rescue: TonightPick | null;
}

export interface SteamMatch {
  steam_name: string;
  app_id: number;
  game: Game | null;
  in_library: boolean;
}

export type RuleValue = string | number | string[] | null;

export interface Rule {
  field: string;
  op: string;
  value?: RuleValue;
}

export interface RuleSet {
  match: "all" | "any";
  rules: Rule[];
  sort?: { field: string; dir: "asc" | "desc" };
  limit?: number;
}

export interface GameList {
  id: string;
  name: string;
  description: string;
  kind: "manual" | "smart";
  rules?: RuleSet;
  count: number;
  created_at: string;
}

export interface SmartField {
  key: string;
  label: string;
  type: "text" | "number" | "date" | "enum" | "ref";
  ops: string[];
  enum?: string[];
}

export interface SearchResult {
  game: Game;
  in_library: boolean;
}

/** One hit from GET /api/books/search. */
export interface BookSearchResult {
  book: Book;
  in_library: boolean;
  /** Present when the work is already owned — the attach APIs are entry-keyed. */
  entry_id?: string;
}

/** A franchise or collection ("Mass Effect"), shared across users. */
export interface Series {
  id: string;
  igdb_collection_id: number | null;
  igdb_franchise_id: number | null;
  name: string;
  slug: string;
}

/** One series in the index, with the journey rolled up. */
export interface SeriesSummary extends Series {
  owned_count: number;
  played_count: number;
  /** Played / owned as a percentage, one decimal. */
  completion: number;
  remaining_hours: number;
  /** The first unplayed game in the user's chosen play order. */
  next_game: NamedRef | null;
}

/** How a series journey is ordered. */
export type PlayOrder =
  | "release"
  | "chronological"
  | "recommended"
  | "custom"
  | "good_ones";

export const PLAY_ORDERS: { value: PlayOrder; label: string }[] = [
  { value: "release", label: "Release order" },
  { value: "chronological", label: "Chronological (DLC with its game)" },
  { value: "recommended", label: "Recommended (best first)" },
  { value: "custom", label: "Custom (drag)" },
  { value: "good_ones", label: "Just the good ones (IGDB ≥ 75)" },
];

/** How a member relates to the series. */
export type SeriesMemberKind = "game" | "dlc" | "expansion";

/** One game in a series; status is "unowned" when it isn't in the library. */
export interface SeriesMember {
  game: Game;
  kind: SeriesMemberKind;
  status: Status | "unowned";
  entry_id?: string;
  position?: number | null;
  logged_minutes: number;
}

/** The full series view. */
export interface SeriesDetail extends Series {
  play_order: PlayOrder;
  members: SeriesMember[];
  owned_count: number;
  played_count: number;
  completion: number;
  remaining_hours: number;
  /** Unplayed owned DLC/expansion hours inside this series. */
  dlc_hours: number;
}

/** The top row of the "Your Gaming Problem" dashboard. */
export interface InsightsHeadline {
  games_owned: number;
  unplayed_games: number;
  hours_remaining: number;
  /** Null when there is no play pace to project from. */
  years_at_current_rate: number | null;
}

/**
 * The numbers behind one superlative. Which fields are set depends on the
 * kind — game-backed stats fill the game fields, bucket stats (genre /
 * platform / release year) fill the counts.
 */
export interface SuperlativePayload {
  game?: Game;
  entry_id?: string;
  /** Date the game entered the library (YYYY-MM-DD). */
  added_on?: string;
  hours?: number;
  name?: string;
  owned?: number;
  played?: number;
  backlog_games?: number;
  backlog_hours?: number;
  year?: number;
}

export type SuperlativeKind =
  | "oldest_untouched"
  | "longest_unplayed"
  | "neglected_genre"
  | "worst_platform"
  | "neglected_year";

export interface Superlative {
  kind: SuperlativeKind;
  payload: SuperlativePayload;
  /** Pre-rendered by the backend so the copy lives in one place. */
  label: string;
}

export interface Insights {
  headline: InsightsHeadline;
  superlatives: Superlative[];
}

/** Achievement rarity bands, in ascending order of prestige. */
export type AchievementTier = "bronze" | "silver" | "gold" | "legendary";

export const ACHIEVEMENT_TIERS: AchievementTier[] = ["bronze", "silver", "gold", "legendary"];

/** One achievement from the code-defined catalogue. */
export interface Achievement {
  id: string;
  title: string;
  description: string;
  /** Code key the client maps to an icon glyph. */
  icon: string;
  tier: AchievementTier;
  /** Hidden achievements are served masked while locked: ??? / teasing copy. */
  hidden: boolean;
  /** Eggs unlock only by playing with the app — predicates never fire for them. */
  egg: boolean;
}

/** An achievement plus the user's unlock state: no date and no game = locked. */
export interface AchievementStatus extends Achievement {
  unlocked_at?: string;
  /**
   * The game that triggered the unlock, when there is one. Game-scoped by
   * contract: every unlock predicate reasons about games.
   */
  entry?: GameEntry;
}

/** The per-calendar-year "Backlog Challenge" rollup. */
export interface Season {
  year: number;
  games_completed: number;
  hours_played: number;
  franchises_cleared: number;
  /** Games finished after a year or more of ownership. */
  rescues: number;
}

/** A project kind: what the target is measured against. */
export type ProjectKind = "checklist" | "count_goal" | "rule_goal";

export const PROJECT_KIND_LABELS: Record<ProjectKind, string> = {
  checklist: "Checklist",
  count_goal: "Count goal",
  rule_goal: "Rule goal",
};

/** The computed state of a project's target, served with every project read. */
export interface ProjectProgress {
  /** What "done" means: members, target_count, or the full match pool. */
  target_count: number;
  completed_count: number;
  est_hours_total: number;
  est_hours_done: number;
  est_hours_remaining: number;
  /** completed / target as a percentage, capped at 100. */
  percent: number;
}

/**
 * A temporary objective. Lists answer "what exists"; projects answer "what am
 * I trying to accomplish", and they end.
 */
export interface Project {
  id: string;
  name: string;
  description: string;
  kind: ProjectKind;
  target_count?: number | null;
  rules?: RuleSet;
  created_at: string;
  /** Stamped when the target is met, or manually on close. Null = in progress. */
  completed_at?: string | null;
  progress: ProjectProgress;
}

/**
 * One member of a project view. Done is the manual per-item override; null
 * means completion derives from the entry's status (played = done).
 */
export interface ProjectItem {
  entry: Entry;
  done: boolean | null;
}

/** One file the scanner inventoried on the read-only NAS mount. */
export interface MediaFile {
  id: number;
  root: string;
  path: string;
  kind: "audio" | "epub";
  size_bytes: number;
  duration_seconds?: number | null;
  container_metadata?: Record<string, unknown> | null;
  book_id?: string | null;
  missing_at?: string | null;
}

/** One file the scanner refused to inventory, with the reason why. */
export interface MediaSkipped {
  id: number;
  root: string;
  path: string;
  ext: string;
  reason: "unsupported_extension" | "drm_epub";
  size_bytes: number;
  seen_at: string;
}

/** One proposed (book, confidence) pair for an unattached candidate. */
export interface MediaSuggestion {
  book: Book;
  confidence: number;
  /** Where the book came from: the user's library or an Open Library search. */
  source: "library" | "openlibrary";
  /** Which facts produced the confidence: tags, directory layout, filename. */
  signal: "tags" | "directory" | "filename";
  in_library: boolean;
  /** The user's library entry, when owned — the attach API is entry-keyed. */
  entry_id?: string;
}

/**
 * One attachable unit from the review queue: an audiobook directory of
 * ordered files, or a single EPUB. Files are track-ordered for audio —
 * attaching them in array order is the explicit track order.
 */
export interface MediaCandidate {
  key: string;
  kind: "audio" | "epub";
  root: string;
  dir_path: string;
  title_guess: string;
  author_guess: string;
  files: MediaFile[];
  total_duration_seconds: number;
  suggestions: MediaSuggestion[];
  high_confidence: boolean;
}

/** GET /api/media/candidates. */
export interface MediaCandidatesResponse {
  candidates: MediaCandidate[];
  skipped: MediaSkipped[];
}

/** The live or last-completed scan summary. */
export interface MediaScanStatus {
  running: boolean;
  found: number;
  new: number;
  unsupported: number;
  last?: {
    started_at: string;
    finished_at?: string | null;
    found: number;
    new: number;
    changed: number;
    restored: number;
    missing: number;
    unsupported: number;
    failed: number;
    error?: string;
  } | null;
}

/**
 * One file's slot on a book's audio timeline. `global_start` is where this
 * track begins in the whole book, which is the coordinate the player works
 * in — track boundaries are the server's business, not the listener's.
 *
 * `measured` is false when the file's length could not be read out of its
 * container: the track still holds its place in the running order, it just
 * contributes no time, and every later `global_start` is short by its real
 * length. `missing` marks a file whose path is currently absent from its
 * root — an unmounted NAS, not a deleted book.
 */
export interface AudioTrack {
  id: number;
  track_number: number;
  path: string;
  title: string;
  size_bytes: number;
  duration_seconds: number;
  global_start: number;
  measured: boolean;
  missing: boolean;
}

/** GET /api/books/{entryId}/audio — the book as one continuous tape. */
export interface AudioTimeline {
  tracks: AudioTrack[];
  total_duration: number;
  /** At least one track is unmeasured, so the offsets after it are wrong. */
  degraded: boolean;
}

/** Where the player should start, in global seconds and as a track offset. */
export interface PositionAudio {
  seconds: number;
  track_id: number;
  track_number: number;
  track_seconds: number;
  total_duration: number;
  derived: boolean;
  confidence: number;
}

/** Which spine document an offset falls in. */
export interface PositionChapter {
  spine_index: number;
  title: string;
  href: string;
  char_start: number;
  char_end: number;
}

/** The printed page, which only exists once a page map does. */
export interface PositionPage {
  page: number;
  derived: boolean;
  confidence: number;
}

/**
 * GET /api/books/{entryId}/position — one position seen from all three
 * angles. `char_offset` is the stored truth; `audio` and `page` are derived
 * from it, and are null when there is nothing to derive them onto.
 */
export interface BookPosition {
  char_offset: number;
  source: string;
  percent: number;
  char_count: number;
  chapter: PositionChapter | null;
  audio: PositionAudio | null;
  page: PositionPage | null;
  derived: boolean;
  confidence: number;
  updated_at: string | null;
}

/**
 * A position write. Exactly one of the three coordinates may be sent; the
 * player always sends `audio_seconds` measured *inside one track*, with the
 * file id that names which one, and lets the server decide whether it can
 * translate that into a character offset.
 */
export interface PositionWrite {
  audio_seconds?: number;
  audio_file_id?: number;
  char_offset?: number;
  page?: number;
  source?: string;
}

/** The PUT response: the new position, plus any status the write moved. */
export interface PositionWriteResult {
  position: BookPosition;
  status: Status;
  status_changed: boolean;
  offer_finished: boolean;
}
