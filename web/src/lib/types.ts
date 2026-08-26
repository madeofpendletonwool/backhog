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

export interface NamedRef {
  id: number;
  name: string;
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
  platforms: NamedRef[];
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

export interface Entry {
  id: string;
  game: Game;
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
