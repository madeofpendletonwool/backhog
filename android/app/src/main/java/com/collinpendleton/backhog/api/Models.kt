package com.collinpendleton.backhog.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Transcribed from web/src/lib/types.ts, which stays the source of truth.
// Grow this file per stage; unknown fields are ignored by the client's Json.

@Serializable
enum class Role {
    @SerialName("admin") Admin,
    @SerialName("member") Member,
    @SerialName("reader") Reader;

    val label: String
        get() = when (this) {
            Admin -> "Administrator"
            Member -> "Member"
            Reader -> "Reader"
        }

    val blurb: String
        get() = when (this) {
            Admin -> "Everything, plus accounts, invites and server settings."
            Member -> "The whole app, including attaching files and scanning the library."
            Reader -> "Reads, listens and tracks their own shelf. Cannot manage library files."
        }
}

@Serializable
data class User(
    val id: String,
    val email: String,
    val username: String,
    val role: Role,
    @SerialName("disabled_at") val disabledAt: String? = null,
    @SerialName("created_at") val createdAt: String,
) {
    /** True for everyone but a reader — mirrors the server's RequireMediaManager. */
    val canManageMedia: Boolean get() = role != Role.Reader
    val isAdmin: Boolean get() = role == Role.Admin
}

/** What the sign-in screens read before anyone has an account. */
@Serializable
data class AuthConfig(
    @SerialName("registration_enabled") val registrationEnabled: Boolean,
    /** No accounts exist yet: the first sign-up is allowed and becomes the administrator. */
    val setup: Boolean = false,
    val invite: InviteOffer? = null,
)

@Serializable
data class InviteOffer(
    val email: String,
    val role: Role,
    @SerialName("invited_by") val invitedBy: String,
    @SerialName("expires_at") val expiresAt: String,
)

@Serializable
data class Health(
    val status: String,
    val metadata: Boolean = false,
    val steam: Boolean = false,
)

@Serializable
data class Ok(val ok: Boolean = true)

@Serializable
data class LoginRequest(val email: String, val password: String)

@Serializable
data class RegisterRequest(
    val email: String,
    val username: String,
    val password: String,
    val invite: String? = null,
)

@Serializable
data class ChangePasswordRequest(
    @SerialName("current_password") val currentPassword: String,
    @SerialName("new_password") val newPassword: String,
)

/** The server's error body: `{"error": "..."}`. */
@Serializable
internal data class ErrorBody(val error: String? = null)

// --- the library (Stage 2) -------------------------------------------------
// Transcribed from web/src/lib/types.ts + api/internal/models/models.go.

/**
 * The six states one item can be in. Wishlist is a shopping list, not a state
 * you flip owned games into while browsing — the library tabs show the quick
 * set, the detail page shows all six.
 */
@Serializable
enum class EntryStatus(val key: String, val gameLabel: String, val bookLabel: String) {
    @SerialName("backlog") Backlog("backlog", "Backlog", "To read"),
    @SerialName("playing") Playing("playing", "Playing", "Reading"),
    @SerialName("played") Played("played", "Played", "Read"),
    @SerialName("dropped") Dropped("dropped", "Dropped", "Abandoned"),
    @SerialName("ignored") Ignored("ignored", "Ignored", "Ignored"),
    @SerialName("wishlist") Wishlist("wishlist", "Wishlist", "Wishlist");

    fun label(isBook: Boolean): String = if (isBook) bookLabel else gameLabel

    companion object {
        /** The library tabs and quick status menus; wishlist stays on the detail page. */
        val quick: List<EntryStatus> = listOf(Backlog, Playing, Played, Dropped, Ignored)
        val all: List<EntryStatus> = entries.toList()
        fun fromKey(key: String?): EntryStatus? = entries.firstOrNull { it.key == key }
    }
}

@Serializable
data class NamedRef(val id: Long, val name: String)

/** A platform with its curated classification; unclassified ones come back family "other". */
@Serializable
data class Platform(
    val id: Long,
    val name: String,
    val manufacturer: String? = null,
    val family: String? = null,
    val generation: Int? = null,
    val handheld: Boolean = false,
)

@Serializable
data class GameWebsite(val url: String, val category: Int? = null)

@Serializable
data class GameVideo(
    @SerialName("video_id") val videoId: String,
    val name: String = "Trailer",
)

/** A related game (similar / DLC / expansion): display-only for now, like the web. */
@Serializable
data class RelatedGame(
    val id: Long,
    val name: String,
    @SerialName("cover_image_id") val coverImageId: String? = null,
)

/** The deep IGDB dossier, fetched lazily and cached server-side; null until a detail read. */
@Serializable
data class GameExtras(
    val developer: String? = null,
    val publisher: String? = null,
    val storyline: String? = null,
    val category: String? = null,
    @SerialName("aggregated_rating") val aggregatedRating: Double? = null,
    @SerialName("game_modes") val gameModes: List<String> = emptyList(),
    @SerialName("player_perspectives") val playerPerspectives: List<String> = emptyList(),
    val themes: List<String> = emptyList(),
    @SerialName("alternative_names") val alternativeNames: List<String> = emptyList(),
    @SerialName("age_ratings") val ageRatings: List<String> = emptyList(),
    val franchise: String? = null,
    val collection: String? = null,
    val websites: List<GameWebsite> = emptyList(),
    @SerialName("screenshot_image_ids") val screenshotImageIds: List<String> = emptyList(),
    val videos: List<GameVideo> = emptyList(),
    @SerialName("similar_games") val similarGames: List<RelatedGame> = emptyList(),
    val dlcs: List<RelatedGame> = emptyList(),
    val expansions: List<RelatedGame> = emptyList(),
)

@Serializable
data class Game(
    val id: Long,
    val name: String,
    val slug: String = "",
    val summary: String = "",
    @SerialName("cover_url") val coverUrl: String = "",
    @SerialName("accent_hex") val accentHex: String = "",
    /** Unix seconds. */
    @SerialName("first_release_date") val firstReleaseDate: Long? = null,
    @SerialName("igdb_rating") val igdbRating: Double? = null,
    /** Seconds; null until a detail fetch, which the server does on add. */
    @SerialName("time_to_beat_main") val timeToBeatMain: Long? = null,
    @SerialName("time_to_beat_complete") val timeToBeatComplete: Long? = null,
    val genres: List<NamedRef> = emptyList(),
    val platforms: List<Platform> = emptyList(),
    val extras: GameExtras? = null,
) {
    /** `accent_hex` or the web's brand-purple fallback (`accentStyle` in lib/format.ts). */
    val accentHexOrNull: String? get() = accentHex.takeIf { it.isNotBlank() }
}

/**
 * Just the book fields the shared surfaces (the queue) need. Books grow their
 * own full model in Stage 4; a queue row only asks for a title and a cover.
 */
@Serializable
data class BookBrief(
    val id: String,
    val title: String,
    val authors: List<String> = emptyList(),
    @SerialName("cover_url") val coverUrl: String = "",
    @SerialName("accent_hex") val accentHex: String = "",
    @SerialName("first_publish_year") val firstPublishYear: Int? = null,
)

/**
 * One item in one user's library. Exactly one of game/book is set — the server
 * omits the other from the payload rather than serialising a null.
 */
@Serializable
data class Entry(
    val id: String,
    @SerialName("media_type") val mediaType: String = "game",
    val game: Game? = null,
    val book: BookBrief? = null,
    val status: EntryStatus = EntryStatus.Backlog,
    @SerialName("shared_by") val sharedBy: String? = null,
    @SerialName("platform_id") val platformId: Long? = null,
    /**
     * The printing this copy is anchored to (Stage 4): the page count its
     * progress is measured in. Null for a book added by title alone, and
     * always for a game.
     */
    @SerialName("edition_id") val editionId: String? = null,
    @SerialName("user_rating") val userRating: Int? = null,
    val notes: String = "",
    @SerialName("queue_position") val queuePosition: Double? = null,
    @SerialName("logged_minutes") val loggedMinutes: Int = 0,
    @SerialName("started_at") val startedAt: String? = null,
    @SerialName("finished_at") val finishedAt: String? = null,
    @SerialName("created_at") val createdAt: String,
    @SerialName("updated_at") val updatedAt: String,
    /**
     * The stored reading position as a percentage (Stage 4), and when it was
     * last written. Absent for a game and for a book never opened.
     */
    @SerialName("progress_percent") val progressPercent: Double? = null,
    @SerialName("last_read_at") val lastReadAt: String? = null,
    /** What last wrote the position: "read", "listen", "scan" or "manual". */
    @SerialName("progress_source") val progressSource: String? = null,
) {
    val isBook: Boolean get() = mediaType == "book"
    val isGame: Boolean get() = !isBook

    /** Only games carry a time-to-beat; a book contributes nothing to an hour total. */
    val hours: Double
        get() = game?.timeToBeatMain?.takeIf { it > 0 }?.let { it / 3600.0 } ?: 0.0

    /** The accent to tint this entry's chrome with, sampled server-side from its artwork. */
    val title: String get() = if (isBook) book?.title ?: "" else game?.name ?: ""
}

@Serializable
data class PlaySession(
    val id: String,
    @SerialName("entry_id") val entryId: String,
    /** Local calendar day, `YYYY-MM-DD`. */
    @SerialName("played_on") val playedOn: String,
    val minutes: Int,
    val note: String = "",
    @SerialName("created_at") val createdAt: String,
)

@Serializable
data class LibraryResponse(
    val entries: List<Entry> = emptyList(),
    val total: Int = 0,
)

@Serializable
data class QueueResponse(val entries: List<Entry> = emptyList())

@Serializable
data class FacetsResponse(
    val platforms: List<Platform> = emptyList(),
    val genres: List<NamedRef> = emptyList(),
)

@Serializable
data class SearchResult(
    val game: Game,
    @SerialName("in_library") val inLibrary: Boolean = false,
)

@Serializable
data class SearchResponse(val results: List<SearchResult> = emptyList())

/** The body of `POST /api/library`: one of game_id/book_id, an optional status, an optional platform. */
@Serializable
data class AddEntryRequest(
    @SerialName("game_id") val gameId: Long? = null,
    val status: EntryStatus? = null,
    @SerialName("platform_id") val platformId: Long? = null,
)

/**
 * The body of `POST /api/library/reorder`: the moved entry and its new
 * neighbours. No defaults: the client Json omits default-valued fields, and
 * an empty before/after id is meaningful ("no neighbour on that side").
 */
@Serializable
data class ReorderRequest(
    @SerialName("entry_id") val entryId: String,
    @SerialName("before_id") val beforeId: String,
    @SerialName("after_id") val afterId: String,
)

@Serializable
data class AddSessionRequest(
    val minutes: Int,
    /** Omitted entirely lets the server default to today. */
    @SerialName("played_on") val playedOn: String? = null,
    val note: String? = null,
)

/** One unlock riding a mutation's response — the hook the web shows toasts from. */
@Serializable
data class AchievementStatus(
    val id: String,
    val title: String,
    val description: String = "",
    val icon: String = "",
    val tier: String = "bronze",
    val domain: String = "any",
    val hidden: Boolean = false,
    val egg: Boolean = false,
    @SerialName("unlocked_at") val unlockedAt: String? = null,
    /** The entry that tipped it over, on gallery reads and unlock toasts. */
    val entry: Entry? = null,
)

@Serializable
data class AchievementsResponse(val achievements: List<AchievementStatus> = emptyList())

@Serializable
data class PatchEntryResponse(
    val entry: Entry,
    val unlocks: List<AchievementStatus> = emptyList(),
)

@Serializable
data class SessionResponse(
    val session: PlaySession,
    val unlocks: List<AchievementStatus> = emptyList(),
)

@Serializable
data class SessionsResponse(val sessions: List<PlaySession> = emptyList())

@Serializable
data class ListsResponse(@SerialName("list_ids") val listIds: List<String> = emptyList())

/** The lists index — used read-only in Stage 2 to name an entry's memberships. */
@Serializable
data class ListSummary(
    val id: String,
    val name: String,
    val description: String = "",
    val kind: String = "manual",
    /** The compiled rule set, on smart lists. */
    val rules: RuleSet? = null,
    val count: Int = 0,
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class ListsIndexResponse(val lists: List<ListSummary> = emptyList())

/** The egg endpoint's answer: whether this attempt was the one that unlocked it. */
@Serializable
data class EggResponse(
    val unlocked: Boolean = false,
    val achievement: AchievementStatus? = null,
)

// --- lists, series, projects, dashboard, picks, Steam, achievements (Stage 3) ---

/** One condition of a smart rule set. The value is string | number | string[] on the wire. */
@Serializable
data class Rule(
    val field: String,
    val op: String,
    /** JsonPrimitive / JsonArray-of-strings / null; built and read through SmartLists. */
    val value: kotlinx.serialization.json.JsonElement? = null,
)

@Serializable
data class RuleSort(val field: String, val dir: String = "desc")

@Serializable
data class RuleSet(
    val match: String = "all",
    val rules: List<Rule> = emptyList(),
    val sort: RuleSort? = null,
    val limit: Int = 0,
)

/** One queryable field as `GET /api/lists/fields` describes it — the builder's whitelist. */
@Serializable
data class SmartField(
    val key: String,
    val label: String,
    val type: String,
    val ops: List<String> = emptyList(),
    val enum: List<String> = emptyList(),
    val media: String? = null,
)

@Serializable
data class SmartFieldsResponse(val fields: List<SmartField> = emptyList())

@Serializable
data class CreateListRequest(
    val name: String,
    val description: String = "",
    val kind: String = "manual",
    /** Omitted for manual lists — encodeDefaults is off, so null drops the key. */
    val rules: RuleSet? = null,
)

/** The list-with-entries payload of `GET /api/lists/{id}`. */
@Serializable
data class ListDetailResponse(
    val list: ListSummary,
    val entries: List<Entry> = emptyList(),
)

@Serializable
data class ListItemsRequest(
    @SerialName("entry_id") val entryId: String? = null,
    @SerialName("entry_ids") val entryIds: List<String>? = null,
)

@Serializable
data class ProjectsResponse(@SerialName("project_ids") val projectIds: List<String> = emptyList())

@Serializable
data class ProjectProgress(
    @SerialName("target_count") val targetCount: Int = 0,
    @SerialName("completed_count") val completedCount: Int = 0,
    @SerialName("est_hours_total") val estHoursTotal: Double = 0.0,
    @SerialName("est_hours_done") val estHoursDone: Double = 0.0,
    @SerialName("est_hours_remaining") val estHoursRemaining: Double = 0.0,
    val percent: Double = 0.0,
)

@Serializable
data class Project(
    val id: String,
    val name: String,
    val description: String = "",
    val kind: String = "checklist",
    @SerialName("media_scope") val mediaScope: String = "game",
    @SerialName("target_count") val targetCount: Int? = null,
    val rules: RuleSet? = null,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("completed_at") val completedAt: String? = null,
    val progress: ProjectProgress = ProjectProgress(),
)

@Serializable
data class ProjectItem(
    val entry: Entry,
    val done: Boolean? = null,
)

@Serializable
data class ProjectsIndexResponse(val projects: List<Project> = emptyList())

@Serializable
data class CreateProjectRequest(
    val name: String,
    val description: String = "",
    val kind: String = "checklist",
    val media: String = "game",
    @SerialName("target_count") val targetCount: Int? = null,
    val rules: RuleSet? = null,
)

@Serializable
data class ProjectDetailResponse(
    val project: Project,
    val items: List<ProjectItem> = emptyList(),
)

// --- series -----------------------------------------------------------------

@Serializable
data class SeriesSummary(
    val id: String,
    @SerialName("igdb_collection_id") val igdbCollectionId: Long? = null,
    @SerialName("igdb_franchise_id") val igdbFranchiseId: Long? = null,
    val name: String,
    val slug: String = "",
    @SerialName("owned_count") val ownedCount: Int = 0,
    @SerialName("played_count") val playedCount: Int = 0,
    val completion: Double = 0.0,
    @SerialName("remaining_hours") val remainingHours: Double = 0.0,
    @SerialName("next_game") val nextGame: NamedRef? = null,
)

@Serializable
data class SeriesIndexResponse(val series: List<SeriesSummary> = emptyList())

@Serializable
data class SeriesMember(
    val game: Game,
    val kind: String = "game",
    val status: String = "unowned",
    @SerialName("entry_id") val entryId: String? = null,
    val position: Double? = null,
    @SerialName("logged_minutes") val loggedMinutes: Int = 0,
) {
    val owned: Boolean get() = status != "unowned"
}

@Serializable
data class SeriesDetail(
    val id: String,
    val name: String,
    val slug: String = "",
    @SerialName("play_order") val playOrder: String = "release",
    val members: List<SeriesMember> = emptyList(),
    @SerialName("owned_count") val ownedCount: Int = 0,
    @SerialName("played_count") val playedCount: Int = 0,
    val completion: Double = 0.0,
    @SerialName("remaining_hours") val remainingHours: Double = 0.0,
    @SerialName("dlc_hours") val dlcHours: Double = 0.0,
)

@Serializable
data class PlayOrderRequest(@SerialName("play_order") val playOrder: String)

@Serializable
data class SeriesReorderRequest(
    @SerialName("game_id") val gameId: Long,
    @SerialName("before_id") val beforeId: Long = 0,
    @SerialName("after_id") val afterId: Long = 0,
)

@Serializable
data class BackfillStatus(val running: Boolean = false)

@Serializable
data class BackfillKickResponse(val started: Boolean = false)

// --- dashboard ---------------------------------------------------------------

@Serializable
data class Stats(
    val total: Int = 0,
    val backlog: Int = 0,
    val playing: Int = 0,
    val played: Int = 0,
    val dropped: Int = 0,
    val ignored: Int = 0,
    val wishlist: Int = 0,
    @SerialName("backlog_hours") val backlogHours: Double = 0.0,
    @SerialName("played_hours") val playedHours: Double = 0.0,
)

@Serializable
data class InsightsHeadline(
    @SerialName("games_owned") val gamesOwned: Int = 0,
    @SerialName("unplayed_games") val unplayedGames: Int = 0,
    @SerialName("hours_remaining") val hoursRemaining: Double = 0.0,
    @SerialName("years_at_current_rate") val yearsAtCurrentRate: Double? = null,
)

@Serializable
data class SuperlativePayload(
    val game: Game? = null,
    @SerialName("entry_id") val entryId: String? = null,
    @SerialName("added_on") val addedOn: String? = null,
    val hours: Double? = null,
    val name: String? = null,
    val owned: Int = 0,
    val played: Int = 0,
    @SerialName("backlog_games") val backlogGames: Int = 0,
    @SerialName("backlog_hours") val backlogHours: Double = 0.0,
    val year: Int? = null,
)

@Serializable
data class Superlative(
    val kind: String,
    val payload: SuperlativePayload = SuperlativePayload(),
    val label: String = "",
)

@Serializable
data class Insights(
    val headline: InsightsHeadline = InsightsHeadline(),
    val superlatives: List<Superlative> = emptyList(),
)

@Serializable
data class Pace(
    @SerialName("hours_per_week_90d") val hoursPerWeek90d: Double? = null,
    @SerialName("hours_per_week_all") val hoursPerWeekAll: Double? = null,
)

// ClearanceScenario and DebtProjection live in BookModels.kt, shared by both
// arenas' debt reports.

@Serializable
data class DebtReport(
    @SerialName("total_hours") val totalHours: Double = 0.0,
    @SerialName("main_backlog_hours") val mainBacklogHours: Double = 0.0,
    @SerialName("started_hours") val startedHours: Double = 0.0,
    @SerialName("short_games_hours") val shortGamesHours: Double = 0.0,
    @SerialName("wishlist_hours") val wishlistHours: Double? = null,
    @SerialName("dlc_hours") val dlcHours: Double? = null,
    val pace: Pace = Pace(),
    val projection: DebtProjection = DebtProjection(),
)

// --- tonight picks ------------------------------------------------------------

@Serializable
data class TonightPick(
    val entry: Entry,
    val score: Double = 0.0,
    val reason: String = "",
)

@Serializable
data class TonightPicksResult(
    @SerialName("continue") val continuePick: TonightPick? = null,
    @SerialName("short_win") val shortWin: TonightPick? = null,
    @SerialName("wildcard") val wildcard: TonightPick? = null,
    @SerialName("rescue") val rescue: TonightPick? = null,
)

// --- Steam import -------------------------------------------------------------

@Serializable
data class SteamPreviewRequest(@SerialName("steam_id") val steamId: String)

@Serializable
data class SteamMatch(
    @SerialName("steam_name") val steamName: String,
    @SerialName("app_id") val appId: Long = 0,
    val game: Game? = null,
    @SerialName("in_library") val inLibrary: Boolean = false,
)

@Serializable
data class SteamPreviewResponse(
    @SerialName("steam_id") val steamId: String = "",
    val total: Int = 0,
    val unmatched: Int = 0,
    val matches: List<SteamMatch> = emptyList(),
)

@Serializable
data class BulkAddRequest(
    @SerialName("game_ids") val gameIds: List<Long>,
    val status: EntryStatus? = null,
)

@Serializable
data class BulkAddResponse(val added: Int = 0, val skipped: Int = 0)

// --- achievements gallery -------------------------------------------------------

@Serializable
data class Season(
    val year: Int = 0,
    @SerialName("games_completed") val gamesCompleted: Int = 0,
    @SerialName("hours_played") val hoursPlayed: Double = 0.0,
    @SerialName("franchises_cleared") val franchisesCleared: Int = 0,
    val rescues: Int = 0,
)
