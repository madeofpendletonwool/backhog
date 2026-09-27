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
)

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
