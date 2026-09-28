package com.collinpendleton.backhog.api

import kotlinx.coroutines.test.runTest
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.Cookie
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/** The Stage 3 endpoints against a mock server: shapes both ways. */
class Stage3ApiTest {
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

    @Test fun `smart fields describe the builder's whitelist`() = runTest {
        enqueue(
            """{"fields":[
               {"key":"status","label":"Status","type":"enum","ops":["eq","neq","in"],"enum":["backlog","playing"]},
               {"key":"hours_to_beat","label":"Hours to beat","type":"number","ops":["lt","gte"],"media":"game"}
             ]}""",
        )
        val fields = apiCall { api().smartFields() }.getOrThrow().fields
        assertEquals(2, fields.size)
        assertEquals("enum", fields[0].type)
        assertEquals(listOf("backlog", "playing"), fields[0].enum)
        assertEquals("game", fields[1].media)
        assertEquals("/api/lists/fields", server.takeRequest().target)
    }

    @Test fun `a smart list carries its compiled rules and resolved entries`() = runTest {
        enqueue(
            """
            {"list":{"id":"l1","name":"Short backlog","kind":"smart",
                     "rules":{"match":"all","rules":[{"field":"media_type","op":"eq","value":"game"}],
                              "sort":{"field":"added","dir":"desc"}},"count":2},
             "entries":[
               {"id":"e1","media_type":"game","status":"backlog",
                "game":{"id":1,"name":"Game One","time_to_beat_main":7200},
                "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
               {"id":"e2","media_type":"game","status":"backlog",
                "game":{"id":2,"name":"Game Two"},
                "created_at":"2026-01-02T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}]}
            """.trimIndent(),
        )
        val detail = apiCall { api().list("l1") }.getOrThrow()
        assertEquals("smart", detail.list.kind)
        assertEquals("game", SmartLists.ruleSetTarget(detail.list.rules))
        assertEquals(2, detail.entries.size)
        assertEquals("/api/lists/l1", server.takeRequest().target)
    }

    @Test fun `creating a list omits defaults and the server fills kind`() = runTest {
        enqueue("""{"id":"l2","name":"Cozy","kind":"manual","count":0}""", code = 201)
        apiCall { api().createList(CreateListRequest(name = "Cozy", description = "")) }
        assertEquals("""{"name":"Cozy"}""", server.takeRequest().body!!.utf8())
    }

    @Test fun `reordering a list posts the moved entry and its neighbours`() = runTest {
        enqueue("""{"ok":true}""")
        apiCall { api().reorderList("l1", ReorderRequest(entryId = "b", beforeId = "", afterId = "a")) }
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/lists/l1/reorder", request.target)
        assertEquals("""{"entry_id":"b","before_id":"","after_id":"a"}""", request.body!!.utf8())
    }

    @Test fun `a series detail decodes with members and dlc hours`() = runTest {
        enqueue(
            """
            {"id":"s1","name":"Mass Effect","play_order":"release",
             "members":[
               {"game":{"id":1,"name":"Mass Effect"},"kind":"game","status":"played","entry_id":"e1","logged_minutes":2400},
               {"game":{"id":2,"name":"ME2 Lair of the Shadow Broker","igdb_rating":86},"kind":"dlc","status":"backlog","logged_minutes":0},
               {"game":{"id":3,"name":"Mass Effect 2"},"kind":"game","status":"unowned","logged_minutes":0}],
             "owned_count":2,"played_count":1,"completion":50.0,"remaining_hours":42.5,"dlc_hours":3.5}
            """.trimIndent(),
        )
        val detail = apiCall { api().seriesDetail("s1") }.getOrThrow()
        assertEquals("release", detail.playOrder)
        assertEquals(3, detail.members.size)
        assertEquals("dlc", detail.members[1].kind)
        assertTrue(detail.members[0].owned)
        assertTrue(!detail.members[2].owned)
        assertNull(detail.members[2].entryId)
        assertEquals(3.5, detail.dlcHours, 0.001)
        assertEquals(42.5, detail.remainingHours, 0.001)
    }

    @Test fun `setting a play order puts the new order`() = runTest {
        enqueue("""{"ok":true}""")
        apiCall { api().setSeriesPlayOrder("s1", PlayOrderRequest("good_ones")) }
        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/api/series/s1/order", request.target)
        assertEquals("""{"play_order":"good_ones"}""", request.body!!.utf8())
    }

    @Test fun `the backfill reports running and its kick reports started`() = runTest {
        enqueue("""{"running":true}""")
        assertTrue(apiCall { api().seriesBackfillStatus() }.getOrThrow().running)
        enqueue("""{"started":true}""")
        assertTrue(apiCall { api().kickSeriesBackfill() }.getOrThrow().started)
        assertEquals("/api/series/backfill", server.takeRequest().target)
        assertEquals("POST", server.takeRequest().method)
    }

    @Test fun `a project detail decodes with progress and items`() = runTest {
        enqueue(
            """
            {"project":{"id":"p1","name":"Clear soulslikes","kind":"checklist","media_scope":"game",
                        "created_at":"2026-01-01T00:00:00Z",
                        "progress":{"target_count":4,"completed_count":2,"est_hours_total":400.0,
                                    "est_hours_done":180.0,"est_hours_remaining":220.0,"percent":50.0}},
             "items":[
               {"entry":{"id":"e1","media_type":"game","status":"played",
                         "game":{"id":1,"name":"Demon's Souls"},
                         "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},"done":null},
               {"entry":{"id":"e2","media_type":"game","status":"backlog",
                         "game":{"id":2,"name":"Elden Ring"},
                         "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},"done":true}]}
            """.trimIndent(),
        )
        val detail = apiCall { api().project("p1") }.getOrThrow()
        assertEquals("checklist", detail.project.kind)
        assertEquals(50.0, detail.project.progress.percent, 0.001)
        assertEquals(220.0, detail.project.progress.estHoursRemaining, 0.001)
        assertNull(detail.items[0].done)
        assertEquals(true, detail.items[1].done)
    }

    @Test fun `setting a project item done sends an explicit json null to reset`() = runTest {
        enqueue("""{"ok":true}""")
        apiCall {
            api().setProjectItemDone(
                "p1", "e1",
                kotlinx.serialization.json.buildJsonObject {
                    put("done", kotlinx.serialization.json.JsonNull)
                },
            )
        }
        assertEquals("""{"done":null}""", server.takeRequest().body!!.utf8())
    }

    @Test fun `insights carry the headline and superlatives`() = runTest {
        enqueue(
            """
            {"headline":{"games_owned":412,"unplayed_games":307,"hours_remaining":4310.5,"years_at_current_rate":5.8},
             "superlatives":[
               {"kind":"oldest_untouched","payload":{"game":{"id":9,"name":"Okami"},"entry_id":"e9","added_on":"2019-04-01","hours":40.0},
                "label":"sitting unplayed since 2019"},
               {"kind":"worst_platform","payload":{"name":"PC (Microsoft Windows)","owned":180,"played":31,"backlog_games":149,"backlog_hours":2100.0},
                "label":"149 unplayed games live here"}]}
            """.trimIndent(),
        )
        val insights = apiCall { api().insights() }.getOrThrow()
        assertEquals(412, insights.headline.gamesOwned)
        assertEquals(5.8, insights.headline.yearsAtCurrentRate!!, 0.001)
        assertEquals("Okami", insights.superlatives[0].payload.game?.name)
        assertEquals(149, insights.superlatives[1].payload.backlogGames)
    }

    @Test fun `the debt report carries pace and projections`() = runTest {
        enqueue(
            """
            {"total_hours":4310.5,"main_backlog_hours":3800.0,"started_hours":510.5,"short_games_hours":300.0,
             "wishlist_hours":null,"dlc_hours":140.0,
             "pace":{"hours_per_week_90d":8.2,"hours_per_week_all":5.1},
             "projection":{"current_pace":{"hours_per_week":8.2,"weeks":525.7,"clear_by":"2036-09-01"},
                            "scenarios":[{"hours_per_week":20.0,"weeks":215.5,"clear_by":"2030-09-01"}]}}
            """.trimIndent(),
        )
        val debt = apiCall { api().debt() }.getOrThrow()
        assertEquals(4310.5, debt.totalHours, 0.001)
        assertNull(debt.wishlistHours)
        assertEquals(140.0, debt.dlcHours!!, 0.001)
        assertEquals("2036-09-01", debt.projection.currentPace?.clearBy)
        assertEquals(1, debt.projection.scenarios.size)
    }

    @Test fun `tonight asks with minutes and the exclude list`() = runTest {
        enqueue(
            """
            {"continue":{"entry":{"id":"e1","media_type":"game","status":"playing",
                                  "game":{"id":1,"name":"Elden Ring"},
                                  "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
                          "score":42.0,"reason":"You're 40 hours in. Finish it."},
             "short_win":null,"wildcard":null,"rescue":null}
            """.trimIndent(),
        )
        val picks = apiCall { api().tonight(90, "e4,e5") }.getOrThrow()
        assertEquals("Elden Ring", picks.continuePick?.entry?.game?.name)
        assertEquals("You're 40 hours in. Finish it.", picks.continuePick?.reason)
        assertNull(picks.shortWin)
        val target = server.takeRequest().target
        assertEquals(true, "minutes=90" in target)
        assertEquals(true, "exclude=e4%2Ce5" in target || "exclude=e4,e5" in target)
    }

    @Test fun `the random pick returns a bare entry`() = runTest {
        enqueue(
            """{"id":"e7","media_type":"game","status":"backlog",
                "game":{"id":7,"name":"Hades"},
                "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}""",
        )
        assertEquals("Hades", apiCall { api().pick() }.getOrThrow().game?.name)
        assertEquals("/api/library/pick", server.takeRequest().target)
    }

    @Test fun `the steam preview reports matches and skips`() = runTest {
        enqueue(
            """
            {"steam_id":"7656119","total":3,"unmatched":1,
             "matches":[
               {"steam_name":"ELDEN RING","app_id":1245620,
                "game":{"id":1020,"name":"Elden Ring"},"in_library":false},
               {"steam_name":"Hades","app_id":1145360,
                "game":{"id":7,"name":"Hades"},"in_library":true},
               {"steam_name":"Some Indie","app_id":999,"game":null,"in_library":false}]}
            """.trimIndent(),
        )
        val preview = apiCall { api().steamPreview(SteamPreviewRequest("7656119")) }.getOrThrow()
        assertEquals(3, preview.total)
        assertEquals(1, preview.unmatched)
        assertEquals(false, preview.matches[0].inLibrary)
        assertNull(preview.matches[2].game)
        assertEquals("""{"steam_id":"7656119"}""", server.takeRequest().body!!.utf8())
    }

    @Test fun `the steam privacy failure surfaces the server message`() = runTest {
        enqueue(
            """{"error":"that profile's game details are private — set Game details to Public in Steam privacy settings"}""",
            code = 400,
        )
        val err = apiCall { api().steamPreview(SteamPreviewRequest("vanity")) }.exceptionOrNull() as ApiError
        assertEquals(400, err.status)
        assertEquals(true, err.message!!.contains("private"))
    }

    @Test fun `bulk add posts the selected game ids and status`() = runTest {
        enqueue("""{"added":41,"skipped":7}""")
        val result = apiCall { api().bulkAdd(BulkAddRequest(gameIds = listOf(1020, 7), status = EntryStatus.Backlog)) }.getOrThrow()
        assertEquals(41, result.added)
        assertEquals("""{"game_ids":[1020,7],"status":"backlog"}""", server.takeRequest().body!!.utf8())
    }

    @Test fun `achievements mask server-side and carry the triggering entry`() = runTest {
        enqueue(
            """
            {"achievements":[
              {"id":"first_blood","title":"First Blood","description":"Finish your first game.",
               "icon":"trophy","tier":"bronze","domain":"game","hidden":false,"egg":false,
               "unlocked_at":"2026-03-01T00:00:00Z",
               "entry":{"id":"e1","media_type":"game","status":"played",
                         "game":{"id":1,"name":"Demon's Souls"},
                         "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}},
              {"id":"night_owl","title":"???","description":"Log a play session between 3 and 5 in the morning.",
               "icon":"night-sleep","tier":"bronze","domain":"game","hidden":true,"egg":true},
              {"id":"hog_watcher","title":"???","description":"Click the Backhog logo 10 times in a row.",
               "icon":"eyeball","tier":"bronze","domain":"any","hidden":true,"egg":true}]}
            """.trimIndent(),
        )
        val achievements = apiCall { api().achievements() }.getOrThrow().achievements
        assertEquals(3, achievements.size)
        assertEquals("Demon's Souls", achievements[0].entry?.game?.name)
        assertNull(achievements[1].unlockedAt)
        assertEquals(true, achievements[2].egg)
        assertEquals("any", achievements[2].domain)
    }

    @Test fun `the season card rolls up the year`() = runTest {
        enqueue(
            """{"year":2026,"games_completed":18,"hours_played":214.5,"franchises_cleared":2,"rescues":5}""",
        )
        val season = apiCall { api().season() }.getOrThrow()
        assertEquals(2026, season.year)
        assertEquals(18, season.gamesCompleted)
        assertEquals(214.5, season.hoursPlayed, 0.001)
        assertEquals(5, season.rescues)
    }

    @Test fun `the egg endpoint answers with the reveal`() = runTest {
        enqueue(
            """{"unlocked":true,
                "achievement":{"id":"queue_shuffler","title":"Chaos Gremlin","tier":"bronze","domain":"any","egg":true}}""",
        )
        val egg = apiCall { api().egg("queue_shuffler") }.getOrThrow()
        assertEquals(true, egg.unlocked)
        assertEquals("Chaos Gremlin", egg.achievement?.title)
        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/achievements/queue_shuffler/egg", request.target)
    }
}
