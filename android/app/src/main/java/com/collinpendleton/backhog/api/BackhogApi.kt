package com.collinpendleton.backhog.api

import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.POST
import retrofit2.http.Query

/**
 * The one API surface. Stage 1 carries the auth spine; the arenas' routes
 * arrive with the stages that render them.
 */
interface BackhogApi {

    @GET("healthz")
    suspend fun healthz(): HealthStatus

    /** Unauthenticated on purpose: it is what the login screen reads before
     *  anyone has an account. A token, when supplied, resolves into the offer
     *  it represents — or comes back with no invite attached. */
    @GET("auth/config")
    suspend fun authConfig(@Query("invite") invite: String? = null): AuthConfig

    @POST("auth/login")
    suspend fun login(@Body body: LoginRequest): User

    @POST("auth/register")
    suspend fun register(@Body body: RegisterRequest): User

    @POST("auth/logout")
    suspend fun logout(): OkResponse

    @GET("auth/me")
    suspend fun me(): User

    @POST("auth/password")
    suspend fun changePassword(@Body body: PasswordChangeRequest): OkResponse
}
