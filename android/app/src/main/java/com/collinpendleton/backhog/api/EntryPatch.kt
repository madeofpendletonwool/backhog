package com.collinpendleton.backhog.api

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/**
 * Builds the PATCH body for `PATCH /api/library/{entryID}`. The server reads
 * raw JSON: an omitted key means "leave alone", an explicit null means "clear"
 * (where the field is nullable). Retrofit's converter would happily serialize
 * a data class of nullables too — but with `explicitNulls = false` it cannot
 * tell "clear the rating" from "don't touch the rating", so the patch is
 * assembled by hand as a JSON object with exactly the keys meant.
 */
class EntryPatch internal constructor(private val fields: MutableList<Pair<String, JsonElement>>) {
    fun status(status: EntryStatus) {
        fields += "status" to JsonPrimitive(status.key)
    }

    /** Null clears the platform ("Not set"); the played-platform prompt sends a real id. */
    fun platform(id: Long?) {
        fields += "platform_id" to (id?.let(::JsonPrimitive) ?: JsonNull)
    }

    /** Null clears the rating; 1–10 otherwise (the server enforces the range). */
    fun rating(rating: Int?) {
        fields += "user_rating" to (rating?.let(::JsonPrimitive) ?: JsonNull)
    }

    fun notes(notes: String) {
        fields += "notes" to JsonPrimitive(notes)
    }

    internal fun build(): JsonObject = JsonObject(fields.toMap())
}

/** The allowed keys are exactly these; the server 400s on anything else. */
fun entryPatch(block: EntryPatch.() -> Unit): JsonObject =
    EntryPatch(mutableListOf()).apply(block).build()
