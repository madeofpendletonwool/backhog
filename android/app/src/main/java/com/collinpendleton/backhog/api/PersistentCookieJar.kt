package com.collinpendleton.backhog.api

import android.content.SharedPreferences
import androidx.core.content.edit
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl

/**
 * The session is an HttpOnly cookie (`backhog_session`), so a cookie jar that
 * survives process death is the whole of the auth machinery — no tokens.
 *
 * Only persistent cookies are written through; the server sets Max-Age on
 * the session, and a session cookie (no expiry) is kept in memory like a
 * browser would. Backed by a [CookieStore] so tests need no Android.
 */
class PersistentCookieJar(private val store: CookieStore) : CookieJar {
    private val cookies = mutableMapOf<String, Cookie>()

    init {
        val now = System.currentTimeMillis()
        store.load().forEach { cookie -> if (cookie.expiresAt > now) cookies[key(cookie)] = cookie }
    }

    @Synchronized
    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        val now = System.currentTimeMillis()
        for (cookie in cookies) {
            if (cookie.expiresAt <= now) this.cookies.remove(key(cookie)) else this.cookies[key(cookie)] = cookie
        }
        persist()
    }

    @Synchronized
    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        val now = System.currentTimeMillis()
        val expired = cookies.values.filter { it.expiresAt <= now }
        if (expired.isNotEmpty()) {
            expired.forEach { cookies.remove(key(it)) }
            persist()
        }
        return cookies.values.filter { it.matches(url) }
    }

    /** Forget everything: logout, and switching server. */
    @Synchronized
    fun clear() {
        cookies.clear()
        persist()
    }

    @Synchronized
    fun hasCookie(name: String): Boolean = cookies.values.any { it.name == name }

    private fun persist() = store.save(cookies.values.filter { it.persistent })

    private fun key(cookie: Cookie) = "${cookie.domain}|${cookie.path}|${cookie.name}"
}

interface CookieStore {
    fun load(): List<Cookie>
    fun save(cookies: List<Cookie>)
}

/** Stores each cookie as its Set-Cookie string, re-parsed against its own domain on load. */
class SharedPrefsCookieStore(private val prefs: SharedPreferences) : CookieStore {
    override fun load(): List<Cookie> =
        prefs.getStringSet(KEY, emptySet()).orEmpty().mapNotNull { line ->
            val (domain, header) = line.split('\n', limit = 2).takeIf { it.size == 2 } ?: return@mapNotNull null
            val url = HttpUrl.Builder().scheme("https").host(domain).build()
            Cookie.parse(url, header)
        }

    override fun save(cookies: List<Cookie>) {
        prefs.edit { putStringSet(KEY, cookies.map { "${it.domain}\n$it" }.toSet()) }
    }

    private companion object {
        const val KEY = "cookies"
    }
}
