package com.collinpendleton.backhog.api

import kotlinx.serialization.json.JsonObject
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.PATCH
import retrofit2.http.POST
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
}
