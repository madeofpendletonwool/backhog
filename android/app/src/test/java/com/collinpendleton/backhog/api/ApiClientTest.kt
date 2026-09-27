package com.collinpendleton.backhog.api

import kotlinx.coroutines.test.runTest
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class ApiClientTest {
    private val server = MockWebServer()
    private var unauthorized = 0

    private class MemoryStore : CookieStore {
        override fun load(): List<Cookie> = emptyList()
        override fun save(cookies: List<Cookie>) = Unit
    }

    @Before fun setUp() = server.start()
    @After fun tearDown() = server.close()

    private fun api(): BackhogApi {
        val base = server.url("/").toString().trimEnd('/')
        return ApiClient.create(base, PersistentCookieJar(MemoryStore())) { unauthorized++ }
    }

    @Test fun `server error body becomes the message`() = runTest {
        server.enqueue(MockResponse.Builder().code(400).body("""{"error":"invalid email or password"}""").build())
        val result = apiCall { api().login(LoginRequest("a@b.c", "nope")) }
        val err = result.exceptionOrNull() as ApiError
        assertEquals(400, err.status)
        assertEquals("invalid email or password", err.message)
    }

    @Test fun `a 401 on a data call routes to sign-in`() = runTest {
        server.enqueue(MockResponse.Builder().code(401).body("""{"error":"unauthorized"}""").build())
        val err = apiCall { api().me() }.exceptionOrNull() as ApiError
        assertTrue(err.isUnauthorized)
        assertEquals(1, unauthorized)
    }

    @Test fun `a 401 at the login form is just a wrong password`() = runTest {
        server.enqueue(MockResponse.Builder().code(401).body("""{"error":"invalid email or password"}""").build())
        apiCall { api().login(LoginRequest("a@b.c", "nope")) }
        assertEquals(0, unauthorized)
    }

    @Test fun `unknown fields are ignored and roles decode`() = runTest {
        server.enqueue(
            MockResponse.Builder().body(
                """{"id":"u1","email":"a@b.c","username":"al","role":"reader","created_at":"2026-01-01T00:00:00Z","future":1}""",
            ).build(),
        )
        val user = apiCall { api().me() }.getOrThrow()
        assertEquals(Role.Reader, user.role)
        assertEquals(false, user.canManageMedia)
    }

    @Test fun `auth config resolves an invite`() = runTest {
        server.enqueue(
            MockResponse.Builder().body(
                """{"registration_enabled":false,"setup":false,"invite":{"email":"x@y.z","role":"member","invited_by":"collin","expires_at":"2026-10-01T00:00:00Z"}}""",
            ).build(),
        )
        val config = apiCall { api().authConfig("tok") }.getOrThrow()
        assertEquals(Role.Member, config.invite?.role)
        assertEquals("/api/auth/config?invite=tok", server.takeRequest().target)
    }
}
