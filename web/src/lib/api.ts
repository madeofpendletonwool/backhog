import type {
  Entry,
  GameList,
  Game,
  NamedRef,
  RuleSet,
  SearchResult,
  SmartField,
  Stats,
  Status,
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

  updateEntry: (id: string, patch: Partial<Record<string, unknown>>) =>
    request<Entry>(`/library/${id}`, { method: "PATCH", body: body(patch) }),

  deleteEntry: (id: string) => request<void>(`/library/${id}`, { method: "DELETE" }),

  stats: () => request<Stats>("/library/stats"),

  facets: () => request<{ platforms: NamedRef[]; genres: NamedRef[] }>("/library/facets"),

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
};

/** Cover images are served by our own API from the local cache. */
export const coverUrl = (gameId: number) => `/api/covers/${gameId}`;
