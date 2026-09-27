package com.collinpendleton.backhog.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// The Stage 4 surface, transcribed from web/src/lib/types.ts (the source of
// truth). Everything here is also written by the games arena's shared routes;
// only the book-specific shapes live in this file.

@Serializable
enum class Status {
    @SerialName("backlog") Backlog,
    @SerialName("playing") Playing,
    @SerialName("played") Played,
    @SerialName("dropped") Dropped,
    @SerialName("ignored") Ignored,
    @SerialName("wishlist") Wishlist;

    /** The label in the books arena — "Reading" a book and "Played" a book are not English. */
    val bookLabel: String
        get() = when (this) {
            Backlog -> "To read"
            Playing -> "Reading"
            Played -> "Read"
            Dropped -> "Abandoned"
            Ignored -> "Ignored"
            Wishlist -> "Wishlist"
        }

    companion object {
        /** The quick-access shelf tabs; Wishlist lives on the detail page only. */
        val Quick = listOf(Backlog, Playing, Played, Dropped, Ignored)
    }
}

/** A book work: "The Hobbit", not any particular printing of it. */
@Serializable
data class Book(
    val id: String,
    val title: String,
    /** The API emits null, not [], when a work has no authors. */
    val authors: List<String>? = null,
    val description: String = "",
    @SerialName("cover_url") val coverUrl: String = "",
    @SerialName("accent_hex") val accentHex: String = "",
    @SerialName("first_publish_year") val firstPublishYear: Int? = null,
    val subjects: List<String>? = null,
    /** The printings cache, present on detail and add responses. */
    val editions: List<BookEdition>? = null,
)

/** One printing of a work. Page maps key off the edition, never the work. */
@Serializable
data class BookEdition(
    val id: String,
    @SerialName("book_id") val bookId: String = "",
    val isbn10: String = "",
    val isbn13: String = "",
    val publisher: String = "",
    @SerialName("published_year") val publishedYear: Int? = null,
    @SerialName("page_count") val pageCount: Int? = null,
    val binding: String = "",
    val language: String = "",
    @SerialName("cover_url") val coverUrl: String = "",
)

/** One item on the shelf. `book` is set on every entry the books arena returns. */
@Serializable
data class Entry(
    val id: String,
    @SerialName("media_type") val mediaType: String,
    val status: Status,
    val book: Book? = null,
    /** Whose attached files this book is read through, when they are not yours. */
    @SerialName("shared_by") val sharedBy: String? = null,
    @SerialName("edition_id") val editionId: String? = null,
    @SerialName("user_rating") val userRating: Int? = null,
    val notes: String = "",
    @SerialName("queue_position") val queuePosition: Int? = null,
    @SerialName("logged_minutes") val loggedMinutes: Int = 0,
    @SerialName("started_at") val startedAt: String? = null,
    @SerialName("finished_at") val finishedAt: String? = null,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
    @SerialName("progress_percent") val progressPercent: Double? = null,
    @SerialName("last_read_at") val lastReadAt: String? = null,
    @SerialName("progress_source") val progressSource: String? = null,
)

@Serializable
data class LibraryResponse(
    val entries: List<Entry>,
    val total: Int,
)

@Serializable
data class AddBookRequest(
    @SerialName("book_id") val bookId: String,
    @SerialName("edition_id") val editionId: String? = null,
    val status: Status? = null,
)

/** The books counterpart of Stats, for the shelf's strip. */
@Serializable
data class BookStats(
    val total: Int = 0,
    val backlog: Int = 0,
    val reading: Int = 0,
    val read: Int = 0,
    val dropped: Int = 0,
    val ignored: Int = 0,
    val wishlist: Int = 0,
    val completion: Double = 0.0,
)

/** The shelf's filter rail: authors, subjects, languages, statuses. */
@Serializable
data class BookFacets(
    val authors: List<String> = emptyList(),
    val subjects: List<String> = emptyList(),
    val languages: List<String> = emptyList(),
    val statuses: List<String> = emptyList(),
)

@Serializable
data class BookSearchResult(
    val book: Book,
    @SerialName("in_library") val inLibrary: Boolean = false,
    @SerialName("entry_id") val entryId: String? = null,
)

@Serializable
data class BookSearchResponse(val results: List<BookSearchResult>)

@Serializable
data class PlaySession(
    val id: String,
    @SerialName("entry_id") val entryId: String,
    @SerialName("played_on") val playedOn: String,
    val minutes: Int,
    val note: String = "",
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class AddSessionRequest(
    val minutes: Int,
    val note: String = "",
    @SerialName("played_on") val playedOn: String? = null,
)

@Serializable
data class SessionsResponse(val sessions: List<PlaySession> = emptyList())

@Serializable
data class AddSessionResponse(
    val session: PlaySession,
    val unlocks: List<UnlockToast> = emptyList(),
)

/** An achievement the mutation unlocked — shown as a toast, nothing more. */
@Serializable
data class UnlockToast(
    val id: String,
    val title: String,
    @SerialName("unlocked_at") val unlockedAt: String? = null,
)

@Serializable
data class EntryUpdateResult(
    val entry: Entry,
    val unlocks: List<UnlockToast> = emptyList(),
)

// --- position: one place, three views --------------------------------------

@Serializable
data class PositionChapter(
    @SerialName("spine_index") val spineIndex: Int = 0,
    val title: String = "",
    @SerialName("title_source") val titleSource: String = "",
    val number: Int = 0,
    @SerialName("char_start") val charStart: Long = 0,
    @SerialName("char_end") val charEnd: Long = 0,
)

@Serializable
data class PositionAudio(
    val seconds: Double,
    @SerialName("track_id") val trackId: Long = 0,
    @SerialName("track_number") val trackNumber: Int = 0,
    @SerialName("track_seconds") val trackSeconds: Double = 0.0,
    @SerialName("total_duration") val totalDuration: Double = 0.0,
    val derived: Boolean = false,
    val confidence: Double = 0.0,
    @SerialName("anchor_distance") val anchorDistance: Double = 0.0,
)

/** The printed page, which only exists once a page map does. */
@Serializable
data class PositionPage(
    val page: Int,
    val derived: Boolean = false,
    val confidence: Double = 0.0,
    @SerialName("anchor_distance") val anchorDistance: Double = 0.0,
    /** Pages of error bar; null when the map holds a single anchor and cannot bound it. */
    val margin: Double? = null,
)

@Serializable
data class BookPosition(
    @SerialName("position_mode") val positionMode: String = "text",
    @SerialName("char_offset") val charOffset: Long = 0,
    val source: String = "",
    val percent: Double = 0.0,
    @SerialName("char_count") val charCount: Long = 0,
    val chapter: PositionChapter? = null,
    val audio: PositionAudio? = null,
    val page: PositionPage? = null,
    @SerialName("page_index") val pageIndex: Int? = null,
    @SerialName("page_count") val pageCount: Int = 0,
    val derived: Boolean = false,
    val confidence: Double = 0.0,
    @SerialName("updated_at") val updatedAt: String? = null,
)

@Serializable
data class PositionWrite(
    @SerialName("audio_seconds") val audioSeconds: Double? = null,
    @SerialName("audio_file_id") val audioFileId: Long? = null,
    @SerialName("char_offset") val charOffset: Long? = null,
    val page: Int? = null,
    @SerialName("page_index") val pageIndex: Int? = null,
    val source: String? = null,
)

// --- passage matching (page scanning) ---------------------------------------

@Serializable
data class PassageMatch(
    @SerialName("char_offset") val charOffset: Long,
    @SerialName("char_end") val charEnd: Long,
    val confidence: Double,
)

@Serializable
data class PassageContext(
    val before: String = "",
    val passage: String = "",
    val after: String = "",
)

@Serializable
data class PassageResult(
    val match: PassageMatch,
    val alternatives: List<PassageMatch> = emptyList(),
    val ambiguous: Boolean = false,
    val context: PassageContext = PassageContext(),
)

@Serializable
data class PassageRequest(val text: String)

// --- physical copies --------------------------------------------------------

@Serializable
data class PhysicalCopy(
    val id: String,
    @SerialName("entry_id") val entryId: String = "",
    @SerialName("edition_id") val editionId: String,
    val notes: String = "",
    /** "owned" or "borrowed". */
    val acquisition: String = "owned",
    @SerialName("due_at") val dueAt: String? = null,
    @SerialName("returned_at") val returnedAt: String? = null,
    @SerialName("anchor_count") val anchorCount: Int = 0,
    @SerialName("seeded_count") val seededCount: Int = 0,
    @SerialName("drives_pages") val drivesPages: Boolean = false,
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class PDFSeedInfo(
    val available: Boolean = false,
    @SerialName("page_count") val pageCount: Int = 0,
    val reason: String? = null,
)

@Serializable
data class CopiesResponse(
    val copies: List<PhysicalCopy> = emptyList(),
    @SerialName("pdf_seed") val pdfSeed: PDFSeedInfo = PDFSeedInfo(),
)

@Serializable
data class CreateCopyRequest(
    @SerialName("edition_id") val editionId: String,
    val notes: String = "",
    val acquisition: String = "owned",
    @SerialName("due_at") val dueAt: String? = null,
)

@Serializable
data class CopyResponse(val copy: PhysicalCopy)

@Serializable
data class PageAnchor(
    @SerialName("printed_page") val printedPage: Int,
    @SerialName("char_offset") val charOffset: Long,
    /** "ocr", "manual", or "pdf". */
    val source: String = "manual",
    val confidence: Double = 0.0,
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class AnchorsResponse(val anchors: List<PageAnchor> = emptyList())

@Serializable
data class AnchorResponse(val anchor: PageAnchor)

@Serializable
data class SaveAnchorRequest(
    @SerialName("printed_page") val printedPage: Int,
    @SerialName("char_offset") val charOffset: Long,
    val source: String,
    val confidence: Double? = null,
)

// --- search in book ----------------------------------------------------------

@Serializable
data class SearchHit(
    @SerialName("char_offset") val charOffset: Long = 0,
    @SerialName("char_end") val charEnd: Long = 0,
    val percent: Double = 0.0,
    val context: PassageContext = PassageContext(),
    val chapter: PositionChapter? = null,
    val audio: PositionAudio? = null,
    val page: PositionPage? = null,
)

@Serializable
data class SearchPageHit(
    @SerialName("page_index") val pageIndex: Int = 0,
    val percent: Double = 0.0,
    val context: PassageContext = PassageContext(),
)

/** Either axis in one shape: text hits fill `results`, paged hits fill `pageResults`. */
@Serializable
data class BookSearchAny(
    val query: String = "",
    /** "text" hits carry offsets; "page" hits carry page indexes. */
    val axis: String = "text",
    /** "phrase" means the book contains what was typed; "loose" is closest passages instead. */
    val mode: String = "phrase",
    val total: Int = 0,
    val truncated: Boolean = false,
    val results: List<SearchHit> = emptyList(),
    val pageResults: List<SearchPageHit> = emptyList(),
    /** Present when the query was too short or the lettering unread: the server says so in words. */
    val error: String? = null,
)

// --- files (reader-safe view) ------------------------------------------------

@Serializable
data class MediaFile(
    val id: Long,
    val path: String = "",
    /** "audio" or "epub". */
    val kind: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
    @SerialName("duration_seconds") val durationSeconds: Double? = null,
    @SerialName("primary_text") val primaryText: Boolean = false,
    @SerialName("missing_at") val missingAt: String? = null,
)

@Serializable
data class AudioEdition(
    val id: Long,
    val primary: Boolean = false,
    val label: String = "",
    val narrator: String? = null,
    @SerialName("track_count") val trackCount: Int = 0,
    @SerialName("total_duration") val totalDuration: Double = 0.0,
    val degraded: Boolean = false,
    @SerialName("missing_count") val missingCount: Int = 0,
)

@Serializable
data class BookFilesResponse(
    val files: List<MediaFile> = emptyList(),
    @SerialName("audio_editions") val audioEditions: List<AudioEdition> = emptyList(),
)

// --- sharing ------------------------------------------------------------------

@Serializable
data class ShareCandidate(
    @SerialName("user_id") val userId: String,
    val username: String,
    val role: Role = Role.Reader,
    val shared: Boolean = false,
    @SerialName("in_library") val inLibrary: Boolean = false,
)

@Serializable
data class ShareCandidatesResponse(val candidates: List<ShareCandidate> = emptyList())

/** One grant: the files behind this book, readable by this person. */
@Serializable
data class BookShare(
    val id: String,
    @SerialName("book_id") val bookId: String = "",
    @SerialName("book_title") val bookTitle: String? = null,
    @SerialName("owner_id") val ownerId: String = "",
    @SerialName("owner_username") val ownerUsername: String? = null,
    @SerialName("user_id") val userId: String = "",
    val username: String = "",
    @SerialName("user_email") val userEmail: String? = null,
    @SerialName("shared_at") val sharedAt: String = "",
    @SerialName("in_library") val inLibrary: Boolean = false,
)

@Serializable
data class SharesOverview(
    val shared: List<BookShare> = emptyList(),
    val received: List<BookShare> = emptyList(),
)

@Serializable
data class ShareRequest(@SerialName("user_id") val userId: String)

// --- the reading dashboard -----------------------------------------------------

@Serializable
data class ReadingHeadline(
    @SerialName("books_owned") val booksOwned: Int = 0,
    @SerialName("unread_books") val unreadBooks: Int = 0,
    @SerialName("pages_owed") val pagesOwed: Double = 0.0,
    @SerialName("hours_owed") val hoursOwed: Double = 0.0,
    @SerialName("years_at_current_rate") val yearsAtCurrentRate: Double? = null,
)

@Serializable
data class ReadingPace(
    @SerialName("pages_per_hour") val pagesPerHour: Double = 0.0,
    @SerialName("chars_per_hour") val charsPerHour: Double = 0.0,
    @SerialName("chars_per_page") val charsPerPage: Double = 0.0,
    /** False until there is enough logged reading — the projection is a default until then. */
    val measured: Boolean = false,
    @SerialName("session_hours") val sessionHours: Double = 0.0,
    @SerialName("hours_per_week_90d") val hoursPerWeek90d: Double? = null,
    @SerialName("hours_per_week_all") val hoursPerWeekAll: Double? = null,
)

@Serializable
data class BookSuperlativePayload(
    val book: Book? = null,
    @SerialName("entry_id") val entryId: String? = null,
    @SerialName("added_on") val addedOn: String? = null,
    val pages: Double? = null,
    val hours: Double? = null,
    val name: String? = null,
    val owned: Int? = null,
    val read: Int? = null,
    val starts: Int? = null,
)

@Serializable
data class BookSuperlative(
    val kind: String,
    val payload: BookSuperlativePayload = BookSuperlativePayload(),
    /** Pre-rendered by the backend so the copy lives in one place. */
    val label: String = "",
)

@Serializable
data class ReadingInsights(
    val headline: ReadingHeadline = ReadingHeadline(),
    val pace: ReadingPace = ReadingPace(),
    val superlatives: List<BookSuperlative> = emptyList(),
)

@Serializable
data class ClearanceScenario(
    @SerialName("hours_per_week") val hoursPerWeek: Double = 0.0,
    val weeks: Double = 0.0,
    /** "2027-03-05"; null means it never clears at this pace. */
    @SerialName("clear_by") val clearBy: String? = null,
)

@Serializable
data class DebtProjection(
    @SerialName("current_pace") val currentPace: ClearanceScenario? = null,
    val scenarios: List<ClearanceScenario> = emptyList(),
)

@Serializable
data class ReadingDebt(
    @SerialName("books_owned") val booksOwned: Int = 0,
    @SerialName("unread_books") val unreadBooks: Int = 0,
    @SerialName("pages_owed") val pagesOwed: Double = 0.0,
    @SerialName("hours_owed") val hoursOwed: Double = 0.0,
    @SerialName("page_hours") val pageHours: Double = 0.0,
    @SerialName("audio_hours") val audioHours: Double = 0.0,
    @SerialName("audio_books") val audioBooks: Int = 0,
    @SerialName("unsized_books") val unsizedBooks: Int = 0,
    @SerialName("short_books_hours") val shortBooksHours: Double = 0.0,
    val pace: ReadingPace = ReadingPace(),
    val projection: DebtProjection = DebtProjection(),
)

@Serializable
data class ReadingSeason(
    val year: Int = 0,
    @SerialName("books_finished") val booksFinished: Int = 0,
    @SerialName("pages_read") val pagesRead: Double = 0.0,
    @SerialName("hours_listened") val hoursListened: Double = 0.0,
    @SerialName("authors_cleared") val authorsCleared: Int = 0,
    val rescues: Int = 0,
)

@Serializable
data class ReadingNowBook(
    val entry: Entry,
    val percent: Double = 0.0,
    @SerialName("remaining_hours") val remainingHours: Double? = null,
    val audio: Boolean = false,
    @SerialName("last_read_at") val lastReadAt: String? = null,
)

@Serializable
data class ReadingNow(val books: List<ReadingNowBook> = emptyList())
