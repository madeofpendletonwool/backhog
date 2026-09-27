package com.collinpendleton.backhog.api

import kotlinx.serialization.json.JsonObject
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.PUT
import retrofit2.http.Path
import retrofit2.http.Query
import retrofit2.Response

/**
 * The server's `/api` surface. One surface, two arenas: book routes are the
 * games routes with `media=book`, so later stages add endpoints here rather
 * than a second interface.
 */
interface BackhogApi {
    @GET("healthz")
    suspend fun health(): Health

    // --- auth ---------------------------------------------------------

    @GET("auth/me")
    suspend fun me(): User

    /** Unauthenticated. Pass a token to resolve it into the invite it represents. */
    @GET("auth/config")
    suspend fun authConfig(@Query("invite") invite: String? = null): AuthConfig

    @POST("auth/login")
    suspend fun login(@Body body: LoginRequest): User

    @POST("auth/register")
    suspend fun register(@Body body: RegisterRequest): User

    @POST("auth/logout")
    suspend fun logout(): Ok

    @POST("auth/password")
    suspend fun changePassword(@Body body: ChangePasswordRequest): Ok

    // --- library ------------------------------------------------------

    /**
     * The library list. One route, two arenas: `media=game` is pinned by the
     * games screens and `media=book` by the books screens, like the web;
     * the platform/genre filters are the games facets, author/subject/
     * language the books ones — the server ignores whichever half does not
     * match `media`.
     */
    @GET("library")
    suspend fun library(
        @Query("media") media: String = "game",
        @Query("status") status: String? = null,
        @Query("q") q: String? = null,
        @Query("sort") sort: String? = null,
        @Query("platform") platform: Long? = null,
        @Query("genre") genre: Long? = null,
        @Query("author") author: String? = null,
        @Query("subject") subject: String? = null,
        @Query("language") language: String? = null,
        @Query("limit") limit: Int? = null,
        @Query("offset") offset: Int? = null,
    ): LibraryResponse

    @GET("library/facets")
    suspend fun facets(@Query("media") media: String = "game"): FacetsResponse

    @GET("library/queue")
    suspend fun queue(): QueueResponse

    @POST("library/reorder")
    suspend fun reorder(@Body body: ReorderRequest): Ok

    @POST("library")
    suspend fun addEntry(@Body body: AddEntryRequest): Entry

    @GET("games/search")
    suspend fun searchGames(@Query("q") q: String, @Query("limit") limit: Int? = null): SearchResponse

    @GET("games/{gameId}")
    suspend fun game(@Path("gameId") gameId: Long): Game

    @GET("library/{entryId}")
    suspend fun entry(@Path("entryId") entryId: String): Entry

    /** PATCH, not PUT: the body is raw JSON whose keys are exactly the fields to change. */
    @PATCH("library/{entryId}")
    suspend fun patchEntry(@Path("entryId") entryId: String, @Body body: JsonObject): PatchEntryResponse

    @DELETE("library/{entryId}")
    suspend fun deleteEntry(@Path("entryId") entryId: String): Response<Unit>

    @GET("library/{entryId}/lists")
    suspend fun entryLists(@Path("entryId") entryId: String): ListsResponse

    @GET("library/{entryId}/sessions")
    suspend fun sessions(@Path("entryId") entryId: String): SessionsResponse

    @POST("library/{entryId}/sessions")
    suspend fun addSession(@Path("entryId") entryId: String, @Body body: AddSessionRequest): SessionResponse

    @DELETE("sessions/{sessionId}")
    suspend fun deleteSession(@Path("sessionId") sessionId: String): Response<Unit>

    @GET("lists")
    suspend fun lists(): ListsIndexResponse

    /** Client-triggered easter eggs: idempotent and rate-limited server-side. */
    @POST("achievements/{id}/egg")
    suspend fun egg(@Path("id") id: String): EggResponse

    // --- books: the works ----------------------------------------------

    @GET("books/search")
    suspend fun searchBooks(@Query("q") query: String): BookSearchResponse

    /** Resolves a barcode or hand-typed ISBN to the work it is a printing of. */
    @GET("books/isbn/{isbn}")
    suspend fun bookByIsbn(@Path("isbn") isbn: String): Book

    /** A work by its Open Library key, editions included. */
    @GET("books/{bookId}")
    suspend fun getBook(@Path("bookId") bookId: String): Book

    /** Adds a work — `POST /library` under a book-shaped body; `addEntry` is the games side. */
    @POST("library")
    suspend fun addBook(@Body body: AddBookRequest): Entry

    @GET("library/stats")
    suspend fun bookStats(@Query("media") media: String = "book"): BookStats

    @GET("library/facets")
    suspend fun bookFacets(@Query("media") media: String = "book"): BookFacets

    // --- one position, three views ---------------------------------------

    @GET("books/{entryId}/position")
    suspend fun bookPosition(@Path("entryId") entryId: String): BookPosition

    @PUT("books/{entryId}/position")
    suspend fun putBookPosition(@Path("entryId") entryId: String, @Body write: PositionWrite): PositionWriteResult

    /**
     * The same write as [putBookPosition] on the verb a client that may die
     * before its next request uses — the web's sendBeacon form. The reader
     * fires it when the app leaves the foreground.
     */
    @POST("books/{entryId}/position")
    suspend fun postBookPosition(@Path("entryId") entryId: String, @Body write: PositionWrite): PositionWriteResult

    // --- canonical text (the reader) ---------------------------------------

    /** The spine with block offsets. Parses the EPUB on first call — the slow one. */
    @GET("books/{entryId}/text/chapters")
    suspend fun bookTextChapters(@Path("entryId") entryId: String): BookTextChapters

    /** One spine document as prose, block for block with its offsets. */
    @GET("books/{entryId}/text/display")
    suspend fun bookTextDisplay(@Path("entryId") entryId: String, @Query("spine") spine: Int): BookTextDisplay

    /** The paged reader's manifest: an image-native PDF's page axis. */
    @GET("books/{entryId}/pages")
    suspend fun bookPages(@Path("entryId") entryId: String): BookPagesResponse

    // --- the file layer (member/admin) ---------------------------------------

    @GET("media/scan")
    suspend fun mediaScanStatus(): MediaScanStatus

    @POST("media/scan")
    suspend fun kickMediaScan(): StartedResponse

    @GET("media/files")
    suspend fun mediaFiles(
        @Query("kind") kind: String? = null,
        @Query("unattached") unattached: Boolean? = null,
        @Query("include_missing") includeMissing: Boolean? = null,
    ): MediaFilesResponse

    @GET("media/candidates")
    suspend fun mediaCandidates(): MediaCandidatesResponse

    @POST("media/ignore")
    suspend fun ignoreMediaFiles(@Body body: IgnoreFilesRequest): IgnoreFilesResponse

    @DELETE("media/ignore/{fileId}")
    suspend fun unignoreMediaFile(@Path("fileId") fileId: Long): UnignoreFileResponse

    @POST("books/{entryId}/files")
    suspend fun attachFiles(@Path("entryId") entryId: String, @Body body: AttachFilesRequest): AttachFilesResponse

    @DELETE("books/{entryId}/files/{fileId}")
    suspend fun detachFile(@Path("entryId") entryId: String, @Path("fileId") fileId: Long): DetachFileResponse

    /** Makes one of a book's text files its canonical text. */
    @PUT("books/{entryId}/files/{fileId}/primary")
    suspend fun setPrimaryTextFile(@Path("entryId") entryId: String, @Path("fileId") fileId: Long): PromoteFileResponse

    /** Makes one of a book's audiobooks the one that plays. */
    @PUT("books/{entryId}/audio-editions/{editionId}/primary")
    suspend fun setPrimaryAudioEdition(@Path("entryId") entryId: String, @Path("editionId") editionId: Long): Ok

    @GET("books/{entryId}/align")
    suspend fun alignStatus(@Path("entryId") entryId: String): AlignmentStatusView

    @POST("books/{entryId}/align")
    suspend fun enqueueAlignment(@Path("entryId") entryId: String): AlignmentEnqueueResponse

    /** 204, no body — clears the map and cancels any in-flight job. */
    @DELETE("books/{entryId}/align")
    suspend fun clearAlignment(@Path("entryId") entryId: String): Response<Unit>


    // --- the paper bridge --------------------------------------------------

    @POST("books/{entryId}/passage")
    suspend fun matchPassage(@Path("entryId") entryId: String, @Body body: PassageRequest): PassageResult

    @GET("books/{entryId}/copies")
    suspend fun copies(@Path("entryId") entryId: String): CopiesResponse

    @POST("books/{entryId}/copies")
    suspend fun createCopy(@Path("entryId") entryId: String, @Body body: CreateCopyRequest): CopyResponse

    @POST("books/{entryId}/copies/{copyId}/return")
    suspend fun returnCopy(@Path("entryId") entryId: String, @Path("copyId") copyId: String): CopyResponse

    @POST("books/{entryId}/copies/{copyId}/reopen")
    suspend fun reopenCopy(@Path("entryId") entryId: String, @Path("copyId") copyId: String): CopyResponse

    @POST("books/{entryId}/copies/{copyId}/own")
    suspend fun ownCopy(@Path("entryId") entryId: String, @Path("copyId") copyId: String): CopyResponse

    @DELETE("books/{entryId}/copies/{copyId}")
    suspend fun deleteCopy(@Path("entryId") entryId: String, @Path("copyId") copyId: String): Ok

    @GET("books/{entryId}/copies/{copyId}/pages")
    suspend fun copyPages(@Path("entryId") entryId: String, @Path("copyId") copyId: String): AnchorsResponse

    @POST("books/{entryId}/copies/{copyId}/pages")
    suspend fun savePageAnchor(
        @Path("entryId") entryId: String,
        @Path("copyId") copyId: String,
        @Body body: SaveAnchorRequest,
    ): AnchorResponse

    // --- search in book -----------------------------------------------------

    @GET("books/{entryId}/search")
    suspend fun searchInBook(@Path("entryId") entryId: String, @Query("q") query: String): BookSearchAny

    // --- files (reader-safe: paths blanked server-side for a reader) ---------

    @GET("books/{entryId}/files")
    suspend fun bookFiles(@Path("entryId") entryId: String): BookFilesResponse

    // --- sharing ---------------------------------------------------------------

    @GET("books/{entryId}/shares")
    suspend fun shareCandidates(@Path("entryId") entryId: String): ShareCandidatesResponse

    @POST("books/{entryId}/shares")
    suspend fun shareBook(@Path("entryId") entryId: String, @Body body: ShareRequest): BookShare

    @DELETE("books/{entryId}/shares/{userId}")
    suspend fun unshareBook(@Path("entryId") entryId: String, @Path("userId") userId: String): Ok

    /** Both halves of "who has what": shared out, and shared with me. */
    @GET("shares")
    suspend fun sharesOverview(): SharesOverview

    // --- the reading dashboard --------------------------------------------------

    @GET("library/insights")
    suspend fun readingInsights(@Query("media") media: String = "book"): ReadingInsights

    @GET("library/debt")
    suspend fun readingDebt(@Query("media") media: String = "book"): ReadingDebt

    @GET("library/reading")
    suspend fun readingNow(): ReadingNow

    @GET("achievements/reading-season")
    suspend fun readingSeason(@Query("year") year: Int? = null): ReadingSeason
}
