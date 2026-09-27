package com.collinpendleton.backhog.api

import kotlinx.serialization.json.Json
import retrofit2.HttpException
import java.io.IOException

/** An API failure carrying the HTTP status, so callers can special-case 401. `status` is 0 for network errors. */
class ApiError(val status: Int, message: String, cause: Throwable? = null) : Exception(message, cause) {
    val isUnauthorized: Boolean get() = status == 401
}

private val errorJson = Json { ignoreUnknownKeys = true }

/** Normalise whatever a call threw into an [ApiError], reading the server's `{"error"}` body. */
fun Throwable.toApiError(): ApiError = when (this) {
    is ApiError -> this
    is HttpException -> {
        val body = response()?.errorBody()?.string()
        val message = body
            ?.let { runCatching { errorJson.decodeFromString(ErrorBody.serializer(), it).error }.getOrNull() }
            ?.takeIf { it.isNotBlank() }
            ?: message()
            ?: "HTTP ${code()}"
        ApiError(code(), message, this)
    }
    is IOException -> ApiError(0, "Could not reach the server: ${message ?: javaClass.simpleName}", this)
    else -> ApiError(-1, message ?: javaClass.simpleName, this)
}

/** Run an API call, turning any failure into an [ApiError] result. */
suspend fun <T> apiCall(block: suspend () -> T): Result<T> =
    try {
        Result.success(block())
    } catch (e: kotlin.coroutines.cancellation.CancellationException) {
        throw e
    } catch (e: Throwable) {
        Result.failure(e.toApiError())
    }
