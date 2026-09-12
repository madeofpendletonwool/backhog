package com.collinpendleton.backhog.api

import android.content.Context
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlinx.serialization.json.Json
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import retrofit2.HttpException
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Owns the HTTP stack: one OkHttp client with the persistent cookie jar and
 * the 401 watcher, plus a Retrofit instance rebuilt whenever the base URL
 * changes (switch-server). All calls go through [call], which flattens
 * Retrofit's HttpException into the web client's `ApiError(status, message)`
 * contract — the server's error envelope is `{"error": "…"}` on every route.
 */
class ApiClient(context: Context, val cookieJar: PersistentCookieJar) {

    val json: Json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        coerceInputValues = true
        // The library entry union discriminates on media_type — the wire's
        // own field, same as the web's TypeScript union.
        classDiscriminator = "media_type"
    }

    private val unauthorizedWatcher =
        Interceptor { chain ->
            val request = chain.request()
            val response = chain.proceed(request)
            if (response.code == 401) {
                val path = request.url.encodedPath
                val authRoute = path.startsWith("/api/auth/") || path == "/api/healthz"
                // A 401 on the auth routes themselves is an answer (wrong
                // password, unauthenticated /me probe), not a session that
                // died mid-flight. Everything else means the cookie stopped
                // being valid: say so once, globally.
                if (!authRoute) SessionEvents.unauthorized.tryEmit(Unit)
            }
            response
        }

    private val http: OkHttpClient =
        OkHttpClient.Builder()
            .cookieJar(cookieJar)
            .addInterceptor(unauthorizedWatcher)
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .writeTimeout(30, TimeUnit.SECONDS)
            .build()

    private val lock = Any()
    private var baseUrlField: String = ""
    private var apiField: BackhogApi? = null

    /** The Retrofit surface for `baseUrl`, rebuilt only when it changed. */
    fun apiFor(baseUrl: String): BackhogApi {
        synchronized(lock) {
            if (apiField == null || baseUrlField != baseUrl) {
                apiField =
                    Retrofit.Builder()
                        .baseUrl(baseUrl)
                        .client(http)
                        .addConverterFactory(json.asConverterFactory("application/json".toMediaType()))
                        .build()
                        .create(BackhogApi::class.java)
                baseUrlField = baseUrl
            }
            return apiField!!
        }
    }

    /**
     * Run an API call, translating an HTTP failure into [ApiError] with the
     * server's own message. Network-level failures (offline, DNS, timeout)
     * propagate as [IOException] for the UI to name honestly.
     */
    suspend fun <T> call(block: suspend () -> T): T =
        try {
            block()
        } catch (e: HttpException) {
            throw e.toApiError()
        }

    /** [call] as a Result — the screens' error channel. */
    suspend fun <T> callCatching(block: suspend () -> T): Result<T> =
        try {
            Result.success(block())
        } catch (e: HttpException) {
            Result.failure(e.toApiError())
        } catch (e: Exception) {
            Result.failure(e)
        }

    private fun HttpException.toApiError(): ApiError {
        val body = runCatching { response()?.errorBody()?.string() }.getOrNull()
        val message = body?.let { runCatching { json.parseToJsonElement(it) }.getOrNull() }
            ?.let { element ->
                (element as? kotlinx.serialization.json.JsonObject)
                    ?.get("error")
                    ?.let { (it as? kotlinx.serialization.json.JsonPrimitive)?.content }
            }
        return ApiError(code(), message ?: message())
    }

    /**
     * Validate a candidate server URL with GET /api/healthz, before anything
     * is saved. Uses the shared client directly — the URL is not (yet) the
     * configured one, so Retrofit's fixed base URL is the wrong tool.
     */
    fun healthCheck(baseUrl: String): Result<HealthStatus> {
        val request = Request.Builder().url("$baseUrl/api/healthz").build()
        return runCatching {
            http.newCall(request).execute().use { response ->
                val text = response.body?.string().orEmpty()
                if (!response.isSuccessful) {
                    throw ApiError(response.code, errorMessage(text) ?: "HTTP ${response.code}")
                }
                json.decodeFromString(HealthStatus.serializer(), text)
            }
        }
    }

    private fun errorMessage(text: String): String? =
        runCatching { json.parseToJsonElement(text) }.getOrNull()
            ?.let { it as? kotlinx.serialization.json.JsonObject }
            ?.get("error")
            ?.let { it as? kotlinx.serialization.json.JsonPrimitive }
            ?.content

    companion object {
        /**
         * Make sense of whatever the user typed into the server box: a bare
         * host, an origin, or a whole invite link pasted from a message
         * (`https://hog.example.com/register?invite=…`). HTTPS is assumed —
         * production servers sit behind Let's Encrypt and v1 offers no
         * cleartext escape hatch.
         *
         * Returns the normalized origin (always with scheme, never with a
         * path) and the invite token when one came along.
         */
        fun normalizeServerInput(raw: String): Pair<String, String?> {
            val trimmed = raw.trim()
            if (trimmed.isEmpty()) return "" to null

            val url =
                if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) {
                    trimmed.toHttpUrlOrNull()
                } else {
                    "https://$trimmed".toHttpUrlOrNull()
                }

            if (url != null) {
                val base = "${url.scheme}://${url.host}${if (url.port != 80 && url.port != 443) ":${url.port}" else ""}/"
                val invite = url.queryParameter("invite")
                return base to invite?.takeIf { it.isNotBlank() }
            }

            // Not URL-shaped — treat it as a bare host and let the health
            // check be the judge.
            return "https://${trimmed.trimEnd('/')}/" to null
        }
    }
}
