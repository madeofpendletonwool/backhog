package com.collinpendleton.backhog.api

import com.collinpendleton.backhog.BuildConfig
import com.collinpendleton.backhog.data.ServerUrl
import kotlinx.serialization.json.Json
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.logging.HttpLoggingInterceptor
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import java.util.concurrent.TimeUnit

object ApiClient {
    val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        coerceInputValues = true
    }

    /**
     * Endpoints whose 401 is an answer, not an expired session: a wrong
     * password at sign-in or on the change-password form.
     */
    private val OWN_401 = setOf("/api/auth/login", "/api/auth/register", "/api/auth/password")

    fun create(baseUrl: String, cookieJar: PersistentCookieJar, onUnauthorized: () -> Unit): BackhogApi {
        val client = OkHttpClient.Builder()
            .cookieJar(cookieJar)
            .addInterceptor(unauthorizedInterceptor(onUnauthorized))
            .apply {
                if (BuildConfig.DEBUG) {
                    addInterceptor(HttpLoggingInterceptor().setLevel(HttpLoggingInterceptor.Level.BASIC))
                }
            }
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .build()

        return Retrofit.Builder()
            .baseUrl(ServerUrl.apiRoot(baseUrl))
            .client(client)
            .addConverterFactory(json.asConverterFactory("application/json".toMediaType()))
            .build()
            .create(BackhogApi::class.java)
    }

    /** Any 401 outside the auth forms means the session is gone: route to sign-in. */
    internal fun unauthorizedInterceptor(onUnauthorized: () -> Unit) = Interceptor { chain ->
        val response = chain.proceed(chain.request())
        if (response.code == 401 && OWN_401.none { chain.request().url.encodedPath.endsWith(it) }) {
            onUnauthorized()
        }
        response
    }
}
