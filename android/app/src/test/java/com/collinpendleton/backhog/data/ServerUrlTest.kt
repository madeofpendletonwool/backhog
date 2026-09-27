package com.collinpendleton.backhog.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ServerUrlTest {
    @Test fun `bare host assumes https`() {
        assertEquals(ServerInput("https://books.example.com", null), ServerUrl.parse("books.example.com"))
    }

    @Test fun `trailing slash and api suffix are dropped`() {
        assertEquals("https://h.example", ServerUrl.parse("https://h.example/")?.baseUrl)
        assertEquals("https://h.example", ServerUrl.parse("https://h.example/api")?.baseUrl)
        assertEquals("https://h.example", ServerUrl.parse("https://h.example/api/healthz")?.baseUrl)
    }

    @Test fun `a sub-path mount is kept`() {
        assertEquals("https://h.example/backhog", ServerUrl.parse("https://h.example/backhog/login")?.baseUrl)
    }

    @Test fun `a web invite link carries server and token`() {
        assertEquals(
            ServerInput("https://h.example:8443", "abc123"),
            ServerUrl.parse("https://h.example:8443/register?invite=abc123"),
        )
    }

    @Test fun `garbage is rejected`() {
        assertNull(ServerUrl.parse(""))
        assertNull(ServerUrl.parse("not a url"))
    }

    @Test fun `api root has the trailing slash retrofit needs`() {
        assertEquals("https://h.example/api/", ServerUrl.apiRoot("https://h.example"))
        assertEquals("https://h.example/sub/api/", ServerUrl.apiRoot("https://h.example/sub/"))
    }
}
