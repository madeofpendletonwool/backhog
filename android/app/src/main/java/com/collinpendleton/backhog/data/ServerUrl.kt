package com.collinpendleton.backhog.data

import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/**
 * What the person typed on the server screen, resolved into a base URL — and,
 * when they pasted a web invite link (`https://host/register?invite=…`), the
 * invite token it carried. The web's invite links are the only deep link the
 * server hands out, and their host is whatever the server runs on, so pasting
 * one here is how an invite reaches the app.
 */
data class ServerInput(val baseUrl: String, val invite: String?)

object ServerUrl {
    /** Parse free-form input. Null if it cannot be a server address. HTTPS is assumed when no scheme is given. */
    fun parse(raw: String): ServerInput? {
        val trimmed = raw.trim()
        if (trimmed.isEmpty() || trimmed.any { it.isWhitespace() }) return null
        val withScheme = if ("://" in trimmed) trimmed else "https://$trimmed"
        val url = withScheme.toHttpUrlOrNull() ?: return null
        if (url.host.isBlank()) return null

        val invite = url.queryParameter("invite")?.takeIf { it.isNotBlank() }
        // Everything after the app's own mount point is a page, not part of the
        // server address: /register, /login, /api, /books/…
        val segments = url.pathSegments.filter { it.isNotEmpty() }
        val cut = segments.indexOfFirst { it in PAGE_SEGMENTS }
        val keep = if (cut >= 0) segments.take(cut) else segments
        return ServerInput(baseUrl = format(url, keep), invite = invite)
    }

    /** `{base}/api/` — Retrofit wants the trailing slash. */
    fun apiRoot(baseUrl: String): String = "${baseUrl.trimEnd('/')}/api/"

    /** `{base}/api/covers/game/{id}` — public (no session), JPEG, immutable-cached. */
    fun gameCoverUrl(baseUrl: String, gameId: Long): String =
        "${baseUrl.trimEnd('/')}/api/covers/game/$gameId"

    private fun format(url: HttpUrl, segments: List<String>): String {
        val builder = url.newBuilder().query(null).fragment(null).encodedPath("/")
        segments.forEach { builder.addPathSegment(it) }
        return builder.build().toString().trimEnd('/')
    }

    private val PAGE_SEGMENTS = setOf("api", "register", "login", "books", "library", "settings", "admin")
}
