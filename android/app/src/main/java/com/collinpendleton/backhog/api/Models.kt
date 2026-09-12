package com.collinpendleton.backhog.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Typed models transcribed from web/src/lib/types.ts — that file is the
 * source of truth, and this one grows with each stage. Field names keep the
 * wire's snake_case via @SerialName so the JSON never has to be reshaped.
 */

// --- shared vocabulary -----------------------------------------------------

@Serializable
enum class MediaType {
    @SerialName("game") GAME,
    @SerialName("book") BOOK,
}

@Serializable
enum class Status {
    @SerialName("backlog") BACKLOG,
    @SerialName("playing") PLAYING,
    @SerialName("played") PLAYED,
    @SerialName("dropped") DROPPED,
    @SerialName("ignored") IGNORED,
    @SerialName("wishlist") WISHLIST,
}

/** Every status, for the full picker on the detail page. */
val STATUSES = Status.entries

/** The statuses surfaced as quick-access controls — wishlist is a shopping
 *  list, not a state you flip entries into while browsing the library. */
val QUICK_STATUSES =
    listOf(Status.BACKLOG, Status.PLAYING, Status.PLAYED, Status.DROPPED, Status.IGNORED)

/** The same six states, said the way a reader says them. */
val STATUS_LABELS =
    mapOf(
        Status.BACKLOG to "Backlog",
        Status.PLAYING to "Playing",
        Status.PLAYED to "Played",
        Status.DROPPED to "Dropped",
        Status.IGNORED to "Ignored",
        Status.WISHLIST to "Wishlist",
    )

val BOOK_STATUS_LABELS =
    mapOf(
        Status.BACKLOG to "To read",
        Status.PLAYING to "Reading",
        Status.PLAYED to "Read",
        Status.DROPPED to "Abandoned",
        Status.IGNORED to "Ignored",
        Status.WISHLIST to "Wishlist",
    )

/** The label for a status in the arena it is being shown in. */
fun statusLabel(status: Status, media: MediaType = MediaType.GAME): String =
    if (media == MediaType.BOOK) BOOK_STATUS_LABELS.getValue(status) else STATUS_LABELS.getValue(status)

@Serializable
data class NamedRef(
    val id: Long,
    val name: String,
)

/** A platform with its curated classification. */
@Serializable
data class Platform(
    val id: Long,
    val name: String,
    val manufacturer: String,
    val family: String,
    val generation: Long? = null,
    val handheld: Boolean,
)

// --- games -------------------------------------------------------------

@Serializable
data class Game(
    val id: Long,
    val name: String,
    val slug: String,
    val summary: String,
    @SerialName("cover_url") val coverUrl: String,
    @SerialName("accent_hex") val accentHex: String,
    @SerialName("first_release_date") val firstReleaseDate: Long? = null,
    @SerialName("igdb_rating") val igdbRating: Double? = null,
    @SerialName("time_to_beat_main") val timeToBeatMain: Double? = null,
    @SerialName("time_to_beat_complete") val timeToBeatComplete: Double? = null,
    val genres: List<NamedRef>,
    val platforms: List<Platform>,
    val extras: GameExtras? = null,
)

@Serializable
data class GameVideo(
    @SerialName("video_id") val videoId: String,
    val name: String,
)

@Serializable
data class RelatedGame(
    val id: Long,
    val name: String,
    @SerialName("cover_image_id") val coverImageId: String,
)

@Serializable
data class GameWebsite(
    val url: String,
    val category: String,
)

/** The richer IGDB metadata shown on the detail page — display-only JSON blob. */
@Serializable
data class GameExtras(
    val developer: String,
    val publisher: String,
    val storyline: String,
    @SerialName("aggregated_rating") val aggregatedRating: Double? = null,
    val category: String,
    @SerialName("game_modes") val gameModes: List<String>,
    @SerialName("player_perspectives") val playerPerspectives: List<String>,
    val themes: List<String>,
    val franchise: String,
    val collection: String,
    @SerialName("alternative_names") val alternativeNames: List<String>,
    @SerialName("age_ratings") val ageRatings: List<String>,
    val websites: List<GameWebsite>,
    @SerialName("screenshot_image_ids") val screenshotImageIds: List<String>,
    val videos: List<GameVideo>,
    @SerialName("similar_games") val similarGames: List<RelatedGame>,
    val dlcs: List<RelatedGame>,
    val expansions: List<RelatedGame>,
)

// --- books -------------------------------------------------------------

/** A book work: "The Hobbit", not any particular printing of it. */
@Serializable
data class Book(
    val id: String,
    val title: String,
    /** The API emits null, not [], when a work has no authors. */
    val authors: List<String>? = null,
    val description: String,
    @SerialName("cover_url") val coverUrl: String,
    @SerialName("accent_hex") val accentHex: String,
    @SerialName("first_publish_year") val firstPublishYear: Long? = null,
    /** The API emits null, not [], when a work has no subjects. */
    val subjects: List<String>? = null,
    /** The printings cache, present on detail and add responses. */
    val editions: List<BookEdition>? = null,
)

/** One printing of a work. Page maps key off the edition, never the work. */
@Serializable
data class BookEdition(
    val id: String,
    @SerialName("book_id") val bookId: String,
    val isbn10: String,
    val isbn13: String,
    val publisher: String,
    @SerialName("published_year") val publishedYear: Long? = null,
    @SerialName("page_count") val pageCount: Long? = null,
    val binding: String,
    val language: String,
    @SerialName("cover_url") val coverUrl: String,
)

// --- library entries ---------------------------------------------------

/**
 * One item in one user's library, discriminated on media_type: exactly one of
 * [GameEntry.game] / [BookEntry.book] is set, same contract as the web's
 * discriminated union.
 */
@Serializable
sealed interface Entry {
    val id: String
    val status: Status
    val platformId: Long?
    val editionId: String?
    val userRating: Long?
    val notes: String
    val queuePosition: Long?
    val loggedMinutes: Long
    val startedAt: String?
    val finishedAt: String?
    val createdAt: String
    val updatedAt: String
}

@Serializable
@SerialName("game")
data class GameEntry(
    override val id: String,
    override val status: Status,
    @SerialName("shared_by") val sharedBy: String? = null,
    @SerialName("platform_id") override val platformId: Long? = null,
    @SerialName("edition_id") override val editionId: String? = null,
    @SerialName("user_rating") override val userRating: Long? = null,
    override val notes: String,
    @SerialName("queue_position") override val queuePosition: Long? = null,
    @SerialName("logged_minutes") override val loggedMinutes: Long,
    @SerialName("started_at") override val startedAt: String? = null,
    @SerialName("finished_at") override val finishedAt: String? = null,
    @SerialName("created_at") override val createdAt: String,
    @SerialName("updated_at") override val updatedAt: String,
    val game: Game,
) : Entry

@Serializable
@SerialName("book")
data class BookEntry(
    override val id: String,
    override val status: Status,
    @SerialName("shared_by") val sharedBy: String? = null,
    @SerialName("platform_id") override val platformId: Long? = null,
    @SerialName("edition_id") override val editionId: String? = null,
    @SerialName("user_rating") override val userRating: Long? = null,
    override val notes: String,
    @SerialName("queue_position") override val queuePosition: Long? = null,
    @SerialName("logged_minutes") override val loggedMinutes: Long,
    @SerialName("started_at") override val startedAt: String? = null,
    @SerialName("finished_at") override val finishedAt: String? = null,
    @SerialName("created_at") override val createdAt: String,
    @SerialName("updated_at") override val updatedAt: String,
    val book: Book,
) : Entry

// --- auth / users ------------------------------------------------------

/**
 * What an account may touch, in order. admin administers on top of member;
 * member is the full application, file management included; reader is the
 * full application minus the file layer.
 */
@Serializable
enum class Role {
    @SerialName("admin") ADMIN,
    @SerialName("member") MEMBER,
    @SerialName("reader") READER,
}

val ROLES = Role.entries

/** How each role is named and explained wherever one is chosen. */
val ROLE_COPY =
    mapOf(
        Role.ADMIN to RoleCopy("Administrator", "Everything, plus accounts, invites and server settings."),
        Role.MEMBER to RoleCopy("Member", "The whole app, including attaching files and scanning the library."),
        Role.READER to RoleCopy("Reader", "Reads, listens and tracks their own shelf. Cannot manage library files."),
    )

data class RoleCopy(val label: String, val blurb: String)

/** True for everyone but a reader. */
fun canManageMedia(role: Role?): Boolean = role != null && role != Role.READER

fun isAdmin(role: Role?): Boolean = role == Role.ADMIN

@Serializable
data class User(
    val id: String,
    val email: String,
    val username: String,
    val role: Role,
    /** Set on a suspended account, which cannot sign in at all. */
    @SerialName("disabled_at") val disabledAt: String? = null,
    @SerialName("created_at") val createdAt: String,
)

/** What the sign-in pages read before anyone has an account. */
@Serializable
data class AuthConfig(
    @SerialName("registration_enabled") val registrationEnabled: Boolean,
    /** No accounts exist yet: the first sign-up is always allowed and becomes
     *  the administrator. */
    val setup: Boolean,
    /** The offer a supplied invite token resolved into; absent for a missing,
     *  spent or expired token — the four cases are not distinguished. */
    val invite: InviteOffer? = null,
)

@Serializable
data class InviteOffer(
    val email: String,
    val role: Role,
    @SerialName("invited_by") val invitedBy: String,
    @SerialName("expires_at") val expiresAt: String,
)

// --- misc envelopes ----------------------------------------------------

/** GET /api/healthz — the server-config screen's green light. */
@Serializable
data class HealthStatus(
    val status: String,
    /** IGDB metadata lookups are on. */
    val metadata: Boolean,
    /** Steam import is on. */
    val steam: Boolean,
)

@Serializable
data class OkResponse(val ok: Boolean)

// --- requests ----------------------------------------------------------

@Serializable
data class LoginRequest(val email: String, val password: String)

@Serializable
data class RegisterRequest(
    val email: String,
    val username: String,
    val password: String,
    /** The token from a sign-up link; carries the role the account lands on. */
    val invite: String? = null,
)

@Serializable
data class PasswordChangeRequest(
    @SerialName("current_password") val currentPassword: String,
    @SerialName("new_password") val newPassword: String,
)
