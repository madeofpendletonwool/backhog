package com.collinpendleton.backhog.api

import kotlinx.coroutines.test.runTest
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.Cookie
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/** The Stage 2 endpoints against a mock server: shapes both ways. */
class GamesApiTest {
    private val server = MockWebServer()

    private class MemoryStore : CookieStore {
        override fun load(): List<Cookie> = emptyList()
        override fun save(cookies: List<Cookie>) = Unit
    }

    @Before fun setUp() = server.start()
    @After fun tearDown() = server.close()

    private fun api(): BackhogApi {
        val base = server.url("/").toString().trimEnd('/')
        return ApiClient.create(base, PersistentCookieJar(MemoryStore())) {}
    }

    private fun enqueue(body: String, code: Int = 200) {
        server.enqueue(MockResponse.Builder().code(code).body(body).build())
    }

    @Test fun `the library carries status sort and facets as query params`() = runTest {
        enqueue("""{"entries":[],"total":0}""")
        api().library(status = "backlog", q = "eld", sort = "name", platform = 6, genre = 31, limit = 60, offset = 60)
        val target = server.takeRequest().target
        assertEquals(true, target.startsWith("/api/library?"))
        assertEquals(true, "media=game" in target)
        assertEquals(true, "status=backlog" in target)
        assertEquals(true, "q=eld" in target)
        assertEquals(true, "sort=name" in target)
        assertEquals(true, "platform=6" in target)
        assertEquals(true, "genre=31" in target)
        assertEquals(true, "limit=60" in target)
        assertEquals(true, "offset=60" in target)
    }

    @Test fun `a game entry decodes with snake_case fields and hours`() = runTest {
        enqueue(
            """
            {"entries":[{"id":"e1","media_type":"game",
              "game":{"id":1020,"name":"Elden Ring","slug":"elden-ring",
                      "cover_url":"https://img/c.jpg","accent_hex":"#8b5cf6",
                      "first_release_date":1648168200,"igdb_rating":95.7,
                      "time_to_beat_main":54000,
                      "genres":[{"id":31,"name":"RPG"}],
                      "platforms":[{"id":6,"name":"PC (Microsoft Windows)","handheld":false}],
                      "extras":{"developer":"FromSoftware","publisher":"Bandai Namco",
                                "game_modes":["Single player"],"themes":["Action"],
                                "screenshot_image_ids":["s1"],"videos":[{"video_id":"E3Huy2cdx0s","name":"Launch Trailer"}],
                                "similar_games":[{"id":7,"name":"Dark Souls"}],"dlcs":[],"expansions":[]}},
              "status":"playing","platform_id":6,"user_rating":9,"notes":"beat messmer",
              "queue_position":null,"logged_minutes":420,
              "started_at":"2026-01-02T00:00:00Z","finished_at":null,
              "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-02-01T00:00:00Z"}],
             "total":1}
            """.trimIndent(),
        )
        val page = apiCall { api().library() }.getOrThrow()
        val entry = page.entries.single()
        assertEquals(EntryStatus.Playing, entry.status)
        assertEquals("Elden Ring", entry.game?.name)
        assertEquals(95.7, entry.game?.igdbRating ?: 0.0, 0.001)
        assertEquals(15.0, entry.hours, 0.001)
        assertEquals("FromSoftware", entry.game?.extras?.developer)
        assertEquals("Launch Trailer", entry.game?.extras?.videos?.single()?.name)
        assertEquals("Dark Souls", entry.game?.extras?.similarGames?.single()?.name)
        assertEquals(9, entry.userRating)
        assertTrue(entry.isGame)
    }

    @Test fun `the queue holds both arenas and a book decodes`() = runTest {
        enqueue(
            """
            {"entries":[
              {"id":"g1","media_type":"game","status":"backlog",
               "game":{"id":1,"name":"Game","time_to_beat_main":3600},
               "queue_position":1024.5,"logged_minutes":0,
               "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
              {"id":"b1","media_type":"book","status":"backlog",
               "book":{"id":"OL1W","title":"A Book","authors":["Le Guin"],"first_publish_year":1968},
               "queue_position":2048.0,"logged_minutes":0,
               "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}
            """.trimIndent(),
        )
        val queue = apiCall { api().queue() }.getOrThrow()
        assertEquals(2, queue.entries.size)
        val book = queue.entries[1]
        assertTrue(book.isBook)
        assertEquals("A Book", book.title)
        assertEquals("Le Guin", book.book?.authors?.single())
        assertEquals(1.0, queue.entries.sumOf { it.hours }, 0.001)
    }

    @Test fun `adding sends game id and status and omits the rest`() = runTest {
        enqueue("""{"id":"e9","media_type":"game","status":"wishlist","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}""", code = 201)
        apiCall { api().addEntry(AddEntryRequest(gameId = 1020, status = EntryStatus.Wishlist)) }
        val body = server.takeRequest().body!!.utf8()
        assertEquals("""{"game_id":1020,"status":"wishlist"}""", body)
    }

    @Test fun `a duplicate add surfaces the server message`() = runTest {
        enqueue("""{"error":"that game is already in your library"}""", code = 409)
        val err = apiCall { api().addEntry(AddEntryRequest(gameId = 1020)) }.exceptionOrNull() as ApiError
        assertEquals(409, err.status)
        assertEquals("that game is already in your library", err.message)
    }

    @Test fun `reorder posts the moved entry and its neighbours`() = runTest {
        enqueue("""{"ok":true}""")
        apiCall { api().reorder(ReorderRequest(entryId = "c", beforeId = "", afterId = "a")) }
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/library/reorder", request.target)
        assertEquals("""{"entry_id":"c","before_id":"","after_id":"a"}""", request.body!!.utf8())
    }

    @Test fun `a patch returns the entry and any unlocks`() = runTest {
        enqueue(
            """
            {"entry":{"id":"e1","media_type":"game","status":"played",
                      "game":{"id":1,"name":"Game"},"finished_at":"2026-03-01T00:00:00Z",
                      "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"},
             "unlocks":[{"id":"first_blood","title":"First Blood","tier":"bronze","domain":"game","hidden":false,"egg":false}]}
            """.trimIndent(),
        )
        val response = apiCall {
            api().patchEntry("e1", entryPatch { status(EntryStatus.Played) })
        }.getOrThrow()
        assertEquals(EntryStatus.Played, response.entry.status)
        assertEquals("first_blood", response.unlocks.single().id)
        assertEquals("""{"status":"played"}""", server.takeRequest().body!!.utf8())
    }

    @Test fun `a session post omits empty fields so the server defaults the day`() = runTest {
        enqueue(
            """{"session":{"id":"s1","entry_id":"e1","played_on":"2026-07-20","minutes":90,"note":"","created_at":"2026-07-20T01:00:00Z"},"unlocks":[]}""",
            code = 201,
        )
        apiCall { api().addSession("e1", AddSessionRequest(minutes = 90)) }
        assertEquals("""{"minutes":90}""", server.takeRequest().body!!.utf8())
    }

    @Test fun `search results carry the in-library flag`() = runTest {
        enqueue(
            """{"results":[{"game":{"id":1020,"name":"Elden Ring"},"in_library":true},
                          {"game":{"id":570,"name":"Dota 2"},"in_library":false}]}""",
        )
        val results = apiCall { api().searchGames("elden") }.getOrThrow().results
        assertEquals(true, results[0].inLibrary)
        assertEquals(false, results[1].inLibrary)
        assertEquals("/api/games/search?q=elden", server.takeRequest().target)
    }

    @Test fun `the degraded no-IGDB server is a 503 with its message`() = runTest {
        enqueue(
            """{"error":"game search is unavailable: set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET"}""",
            code = 503,
        )
        val err = apiCall { api().searchGames("elden") }.exceptionOrNull() as ApiError
        assertEquals(503, err.status)
        assertEquals(true, err.message!!.contains("IGDB_CLIENT_ID"))
    }

    @Test fun `entry lists and the lists index compose into names`() = runTest {
        enqueue("""{"list_ids":["l2","l9"]}""")
        enqueue("""{"lists":[{"id":"l1","name":"Cozy","kind":"manual","count":3},{"id":"l2","name":"Soulslikes","kind":"manual","count":8}]}""")
        val api = api()
        val memberships = apiCall { api.entryLists("e1") }.getOrThrow().listIds
        val index = apiCall { api.lists() }.getOrThrow().lists
        val names = index.filter { it.id in memberships }.map { it.name }
        assertEquals(listOf("Soulslikes"), names)
    }

    @Test fun `the egg endpoint posts by id`() = runTest {
        enqueue("""{"unlocked":true,"achievement":{"id":"night_owl","title":"Night Owl","tier":"silver","egg":true}}""")
        apiCall { api().egg("night_owl") }
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/achievements/night_owl/egg", request.target)
    }

    @Test fun `a 204 delete reads as success`() = runTest {
        server.enqueue(MockResponse.Builder().code(204).build())
        val response = apiCall { api().deleteEntry("e1") }.getOrThrow()
        assertEquals(204, response.code())
    }
}
