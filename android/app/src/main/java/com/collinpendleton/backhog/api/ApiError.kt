package com.collinpendleton.backhog.api

/** An API error carrying the HTTP status, so callers can special-case 401. */
class ApiError(val status: Int, message: String) : Exception(message) {
    val unauthorized: Boolean get() = status == 401
}
