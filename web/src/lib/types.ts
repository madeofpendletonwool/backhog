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

/** One category's answer to "what should I play tonight?". */
export interface TonightPick {
  entry: Entry;
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

/** One achievement from the code-defined catalogue. */
export interface Achievement {
  id: string;
  title: string;
  description: string;
  /** Code key the client maps to an icon glyph. */
  icon: string;
}

/** An achievement plus the user's unlock state: no date and no game = locked. */
export interface AchievementStatus extends Achievement {
  unlocked_at?: string;
  /** The game that triggered the unlock, when there is one. */
  entry?: Entry;
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
