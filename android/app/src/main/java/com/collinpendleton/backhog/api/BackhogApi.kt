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

    /** The library list. `media=game` is pinned by the games screens, like the web. */
    @GET("library")
    suspend fun library(
        @Query("media") media: String = "game",
        @Query("status") status: String? = null,
        @Query("q") q: String? = null,
        @Query("sort") sort: String? = null,
        @Query("platform") platform: Long? = null,
        @Query("genre") genre: Long? = null,
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

    // --- lists (Stage 3) --------------------------------------------------

    @GET("lists/fields")
    suspend fun smartFields(): SmartFieldsResponse

    @POST("lists")
    suspend fun createList(@Body body: CreateListRequest): ListSummary

    @GET("lists/{listId}")
    suspend fun list(@Path("listId") listId: String): ListDetailResponse

    @PATCH("lists/{listId}")
    suspend fun updateList(@Path("listId") listId: String, @Body body: JsonObject): ListSummary

    @DELETE("lists/{listId}")
    suspend fun deleteList(@Path("listId") listId: String): Response<Unit>

    @POST("lists/{listId}/items")
    suspend fun addListItem(@Path("listId") listId: String, @Body body: ListItemsRequest): Ok

    @DELETE("lists/{listId}/items/{entryId}")
    suspend fun removeListItem(@Path("listId") listId: String, @Path("entryId") entryId: String): Response<Unit>

    @POST("lists/{listId}/reorder")
    suspend fun reorderList(@Path("listId") listId: String, @Body body: ReorderRequest): Ok

    // --- series --------------------------------------------------------------

    @GET("series")
    suspend fun series(): SeriesIndexResponse

    @GET("series/{seriesId}")
    suspend fun seriesDetail(@Path("seriesId") seriesId: String): SeriesDetail

    @PUT("series/{seriesId}/order")
    suspend fun setSeriesPlayOrder(@Path("seriesId") seriesId: String, @Body body: PlayOrderRequest): Ok

    @POST("series/{seriesId}/reorder")
    suspend fun reorderSeries(@Path("seriesId") seriesId: String, @Body body: SeriesReorderRequest): Ok

    @GET("series/backfill")
    suspend fun seriesBackfillStatus(): BackfillStatus

    @POST("series/backfill")
    suspend fun kickSeriesBackfill(): BackfillKickResponse

    // --- projects ---------------------------------------------------------------

    @GET("projects")
    suspend fun projects(): ProjectsIndexResponse

    @POST("projects")
    suspend fun createProject(@Body body: CreateProjectRequest): Project

    @GET("projects/{projectId}")
    suspend fun project(@Path("projectId") projectId: String): ProjectDetailResponse

    @PATCH("projects/{projectId}")
    suspend fun updateProject(@Path("projectId") projectId: String, @Body body: JsonObject): Project

    @DELETE("projects/{projectId}")
    suspend fun deleteProject(@Path("projectId") projectId: String): Response<Unit>

    @POST("projects/{projectId}/items")
    suspend fun addProjectItem(@Path("projectId") projectId: String, @Body body: ListItemsRequest): Ok

    @PATCH("projects/{projectId}/items/{entryId}")
    suspend fun setProjectItemDone(
        @Path("projectId") projectId: String,
        @Path("entryId") entryId: String,
        @Body body: JsonObject,
    ): Ok

    @DELETE("projects/{projectId}/items/{entryId}")
    suspend fun removeProjectItem(@Path("projectId") projectId: String, @Path("entryId") entryId: String): Response<Unit>

    @POST("projects/{projectId}/reorder")
    suspend fun reorderProject(@Path("projectId") projectId: String, @Body body: ReorderRequest): Ok

    @GET("library/{entryId}/projects")
    suspend fun entryProjects(@Path("entryId") entryId: String): ProjectsResponse

    // --- dashboard, debt, picks ---------------------------------------------------

    @GET("library/stats")
    suspend fun stats(@Query("media") media: String = "game"): Stats

    @GET("library/insights")
    suspend fun insights(@Query("media") media: String = "game"): Insights

    @GET("library/debt")
    suspend fun debt(@Query("media") media: String = "game"): DebtReport

    /** `exclude` is how a category re-rolls: the ids it just showed, comma-joined. */
    @GET("library/tonight")
    suspend fun tonight(
        @Query("minutes") minutes: Int,
        @Query("exclude") exclude: String? = null,
    ): TonightPicksResult

    @GET("library/pick")
    suspend fun pick(
        @Query("max_hours") maxHours: Double? = null,
        @Query("min_rating") minRating: Double? = null,
        @Query("genre") genre: Long? = null,
    ): Entry

    // --- Steam import ----------------------------------------------------------

    @POST("import/steam/preview")
    suspend fun steamPreview(@Body body: SteamPreviewRequest): SteamPreviewResponse

    @POST("library/bulk")
    suspend fun bulkAdd(@Body body: BulkAddRequest): BulkAddResponse

    // --- achievements --------------------------------------------------------------

    @GET("achievements")
    suspend fun achievements(): AchievementsResponse

    @GET("achievements/season")
    suspend fun season(@Query("year") year: Int? = null): Season
}
