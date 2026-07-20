export type Status = "backlog" | "playing" | "played" | "dropped" | "wishlist";

export const STATUSES: Status[] = ["backlog", "playing", "played", "dropped", "wishlist"];

export const STATUS_LABELS: Record<Status, string> = {
  backlog: "Backlog",
  playing: "Playing",
  played: "Played",
  dropped: "Dropped",
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
  wishlist: number;
  backlog_hours: number;
  played_hours: number;
  logged_hours: number;
  completion: number;
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
