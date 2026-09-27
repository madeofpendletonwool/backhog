package com.collinpendleton.backhog.api

import okhttp3.Cookie
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PersistentCookieJarTest {
    private class MemoryStore : CookieStore {
        var saved: List<String> = emptyList()
        override fun load(): List<Cookie> = saved.mapNotNull { Cookie.parse(url, it) }
        override fun save(cookies: List<Cookie>) {
            saved = cookies.map { it.toString() }
        }
    }

    private companion object {
        val url = "https://h.example/api/auth/login".toHttpUrl()
    }

    private fun session(maxAge: Long = 3600) =
        Cookie.parse(url, "backhog_session=s3cret; Path=/; Max-Age=$maxAge; HttpOnly; Secure; SameSite=Lax")!!

    @Test fun `session survives a restart`() {
        val store = MemoryStore()
        PersistentCookieJar(store).saveFromResponse(url, listOf(session()))

        val reborn = PersistentCookieJar(store)
        assertTrue(reborn.hasCookie("backhog_session"))
        assertEquals("s3cret", reborn.loadForRequest("https://h.example/api/auth/me".toHttpUrl()).single().value)
    }

    @Test fun `logout's expiring cookie removes it`() {
        val store = MemoryStore()
        val jar = PersistentCookieJar(store)
        jar.saveFromResponse(url, listOf(session()))
        jar.saveFromResponse(url, listOf(Cookie.parse(url, "backhog_session=; Path=/; Max-Age=0")!!))
        assertFalse(jar.hasCookie("backhog_session"))
        assertTrue(store.saved.isEmpty())
    }

    @Test fun `clear forgets everything`() {
        val store = MemoryStore()
        val jar = PersistentCookieJar(store)
        jar.saveFromResponse(url, listOf(session()))
        jar.clear()
        assertFalse(PersistentCookieJar(store).hasCookie("backhog_session"))
    }

    @Test fun `cookies do not leak to another host`() {
        val jar = PersistentCookieJar(MemoryStore())
        jar.saveFromResponse(url, listOf(session()))
        assertTrue(jar.loadForRequest("https://other.example/api/auth/me".toHttpUrl()).isEmpty())
    }
}
