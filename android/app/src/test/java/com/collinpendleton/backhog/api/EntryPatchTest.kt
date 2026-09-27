package com.collinpendleton.backhog.api

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.assertEquals
import org.junit.Test

class EntryPatchTest {
    @Test fun `a status patch is exactly one key`() {
        val body = entryPatch { status(EntryStatus.Playing) }
        assertEquals("""{"status":"playing"}""", body.toString())
    }

    @Test fun `an explicit null platform clears it`() {
        val body = entryPatch { platform(null) }
        assertEquals(JsonNull, body["platform_id"])
        assertEquals(1, body.size)
    }

    @Test fun `a platform id is sent as a number`() {
        val body = entryPatch { platform(6L) }
        assertEquals(JsonPrimitive(6L), body["platform_id"])
    }

    @Test fun `a rating patch sends the number and null clears it`() {
        assertEquals(JsonPrimitive(9), entryPatch { rating(9) }["user_rating"])
        assertEquals(JsonNull, entryPatch { rating(null) }["user_rating"])
    }

    @Test fun `notes are sent as a string`() {
        val body = entryPatch { notes("bounced off the tutorial") }
        assertEquals(JsonPrimitive("bounced off the tutorial"), body["notes"])
    }

    @Test fun `the played-with-platform prompt sends both fields`() {
        val body = entryPatch {
            status(EntryStatus.Played)
            platform(48L)
        }
        assertEquals(JsonPrimitive("played"), body["status"])
        assertEquals(JsonPrimitive(48L), body["platform_id"])
        assertEquals(2, body.size)
    }

    @Test fun `the patch serializes through the wire format`() {
        // What the server actually receives: keys present, nothing else.
        val json = entryPatch {
            status(EntryStatus.Played)
            platform(null)
        }
        val parsed: JsonObject = ApiClient.json.parseToJsonElement(json.toString()).jsonObject
        assertEquals("played", (parsed["status"] as JsonPrimitive).content)
        assertEquals(JsonNull, parsed["platform_id"])
        assertEquals(2, parsed.size)
    }
}
