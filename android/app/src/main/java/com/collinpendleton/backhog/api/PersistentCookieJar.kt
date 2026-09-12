package com.collinpendleton.backhog.api

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import java.io.File

/**
 * A persistent cookie jar: the backhog session cookie (`backhog_session`) is
 * HttpOnly, so the cookie *is* the account, and it must survive process death
 * and app restart for cold-start session resume. Cleared wholesale on logout
 * and on switch-server — the app talks to exactly one server at a time.
 *
 * Cookies are held in memory and mirrored to a JSON file with atomic
 * write-and-rename; a cookie that fails to parse or has expired is dropped
 * on load, so a stale file can never pin a dead session.
 */
class PersistentCookieJar(private val store: File) : CookieJar {

    @Serializable
    private data class StoredCookie(
        val name: String,
        val value: String,
        val domain: String,
        val path: String,
        val expiresAt: Long,
        val secure: Boolean,
        val httpOnly: Boolean,
    )

    private val json = Json { ignoreUnknownKeys = true }
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val lock = Any()
    private val cookies = LinkedHashMap<String, Cookie>()

    init {
        synchronized(lock) {
            runCatching {
                val stored = json.decodeFromString<List<StoredCookie>>(store.readText())
                val now = System.currentTimeMillis()
                stored.forEach { c ->
                    if (c.expiresAt > now) {
                        cookies[keyOf(c.domain, c.path, c.name)] = c.toOkHttp()
                    }
                }
            }
        }
    }

    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        val now = System.currentTimeMillis()
        synchronized(lock) {
            val expired = cookies.values.filter { it.expiresAt < now }
            if (expired.isNotEmpty()) {
                expired.forEach { cookies.remove(keyOf(it.domain, it.path, it.name)) }
                persist()
            }
            return cookies.values.filter { it.matches(url) }
        }
    }

    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        if (cookies.isEmpty()) return
        synchronized(lock) {
            val now = System.currentTimeMillis()
            cookies.forEach { cookie ->
                val key = keyOf(cookie.domain, cookie.path, cookie.name)
                // A Max-Age-in-the-past cookie is the server's way of taking
                // one back (logout does exactly this) — honour the deletion.
                if (cookie.expiresAt < now) {
                    this.cookies.remove(key)
                } else {
                    this.cookies[key] = cookie
                }
            }
            persist()
        }
    }

    /** Drop every cookie — logout and switch-server. */
    fun clear() {
        synchronized(lock) {
            cookies.clear()
            persist()
        }
    }

    private fun persist() {
        val snapshot = synchronized(lock) {
            cookies.values.map { c ->
                StoredCookie(
                    name = c.name,
                    value = c.value,
                    domain = c.domain,
                    path = c.path,
                    expiresAt = c.expiresAt,
                    secure = c.secure,
                    httpOnly = c.httpOnly,
                )
            }
        }
        scope.launch {
            runCatching {
                val tmp = File(store.parentFile, store.name + ".tmp")
                tmp.writeText(json.encodeToString(snapshot))
                if (!tmp.renameTo(store)) {
                    store.delete()
                    tmp.renameTo(store)
                }
            }
        }
    }

    private fun keyOf(domain: String, path: String, name: String) = "$domain|$path|$name"

    private fun StoredCookie.toOkHttp(): Cookie =
        Cookie.Builder()
            .name(name)
            .value(value)
            .domain(domain)
            .path(path)
            .expiresAt(expiresAt)
            .apply {
                if (secure) secure()
                if (httpOnly) httpOnly()
            }
            .build()
}
