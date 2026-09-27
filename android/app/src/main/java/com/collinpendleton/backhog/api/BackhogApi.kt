package com.collinpendleton.backhog.api

import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.POST
import retrofit2.http.Query

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
}
