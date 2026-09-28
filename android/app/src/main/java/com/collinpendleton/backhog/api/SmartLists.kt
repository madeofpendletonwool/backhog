package com.collinpendleton.backhog.api

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * The client-side half of smart lists, ported from web/src/lib/smartlists.ts
 * and SmartListBuilder.tsx: which arena a rule set targets, what the operators
 * are called, and how builder values become the JSON the server's compiler
 * expects. Pure functions, JVM-testable.
 */
object SmartLists {

    /** The operator labels the web renders in its dropdowns, verbatim. */
    val opLabels: Map<String, String> = mapOf(
        "eq" to "is",
        "neq" to "is not",
        "gt" to "is more than",
        "lt" to "is less than",
        "gte" to "is at least",
        "lte" to "is at most",
        "contains" to "contains",
        "in" to "is any of",
        "not_in" to "is none of",
        "is_null" to "is not set",
        "not_null" to "is set",
    )

    fun opLabel(op: String): String = opLabels[op] ?: op

    /** Operators that take no value operand. */
    val valuelessOps: Set<String> = setOf("is_null", "not_null")

    /**
     * The sort whitelist, mirroring the server's smartSorts map. `media` says
     * which arena a key orders meaningfully; the builder hides the other
     * arena's keys from a scoped set.
     */
    data class SortField(val value: String, val label: String, val media: String? = null)

    val sortFields: List<SortField> = listOf(
        SortField("added", "date added"),
        SortField("updated", "last updated"),
        SortField("name", "title"),
        SortField("user_rating", "my rating"),
        SortField("igdb_rating", "IGDB rating", "game"),
        SortField("hours_to_beat", "hours to beat", "game"),
        SortField("release_year", "release date", "game"),
        SortField("author", "author", "book"),
        SortField("published", "date published", "book"),
        SortField("pages", "page count", "book"),
    )

    /**
     * A fresh rule for a field, defaulting to the field's first operator and a
     * type-appropriate value — the web's `emptyRule`.
     */
    fun emptyRule(field: SmartField): Rule = Rule(
        field = field.key,
        op = field.ops.firstOrNull() ?: "eq",
        value = when {
            field.type == "enum" -> JsonPrimitive(field.enum.firstOrNull() ?: "")
            field.type == "ref" -> JsonArray(emptyList())
            else -> JsonPrimitive("")
        },
    )

    /** A smart list starts scoped to the arena it was built in — the web's `defaultRules`. */
    fun defaultRules(media: String): RuleSet = RuleSet(
        match = "all",
        rules = listOf(
            Rule("media_type", "eq", JsonPrimitive(media)),
            Rule("status", "eq", JsonPrimitive("backlog")),
        ),
        sort = RuleSort("added", "desc"),
    )

    /**
     * Which arena a rule set targets: a set scoped by its own media_type rules
     * belongs to that arena; null means unscoped (admits both). The port of
     * the web's `ruleSetTarget`, mirrored from store/smartlists.go.
     */
    fun ruleSetTarget(rs: RuleSet?): String? {
        if (rs == null) return null
        val admits = { rule: Rule, media: String ->
            rule.field == "media_type" && when (rule.op) {
                "eq" -> (rule.value as? JsonPrimitive)?.content == media
                "in" -> {
                    val values = (rule.value as? JsonArray)
                        ?.filterIsInstance<JsonPrimitive>()?.map { it.content } ?: emptyList()
                    values.isNotEmpty() && values.all { it == media }
                }
                else -> false
            }
        }
        rs.rules.firstOrNull { admits(it, "book") }?.let { return "book" }
        rs.rules.firstOrNull { admits(it, "game") }?.let { return "game" }
        return null
    }

    // --- value accessors --------------------------------------------------

    /** The rule's value as a plain string (text / enum); "" when absent. */
    fun stringValue(rule: Rule): String = (rule.value as? JsonPrimitive)?.content ?: ""

    /** The rule's value as a number, or null — text fields parse, numbers stay. */
    fun numberValue(rule: Rule): Double? {
        val primitive = rule.value as? JsonPrimitive ?: return null
        primitive.doubleOrNull?.let { return it }
        return primitive.content.toDoubleOrNull()
    }

    /** The rule's value as the multi-value list `in` / `not_in` carry. */
    fun listValue(rule: Rule): List<String> =
        (rule.value as? JsonArray)?.filterIsInstance<JsonPrimitive>()?.map { it.content } ?: emptyList()

    /**
     * A rule value from free text typed into the multi-value box: comma-split
     * and trimmed, exactly the web's split. Numbers must go over the wire as
     * numbers, so a field's own type decides.
     */
    fun listFromText(text: String): JsonArray = JsonArray(
        text.split(",").map { it.trim() }.filter { it.isNotEmpty() }.map { JsonPrimitive(it) },
    )

    /**
     * A rule value from free text for a number field: "" keeps the key absent
     * (null), anything parseable becomes a number primitive — the server
     * type-checks the operand against the column.
     */
    fun numberFromText(text: String): JsonPrimitive? {
        if (text.isBlank()) return null
        return text.toDoubleOrNull()?.let { JsonPrimitive(it) } ?: JsonPrimitive(text)
    }

    // --- wire encoding -----------------------------------------------------

    /** The JSON a PATCH body embeds for a rule set — hand-built, testable. */
    fun encodeRuleSet(rules: RuleSet): JsonObject = buildJsonObject {
        put("match", rules.match)
        put("rules", JsonArray(rules.rules.map { encodeRule(it) }))
        rules.sort?.let { sort ->
            put("sort", buildJsonObject {
                put("field", sort.field)
                put("dir", sort.dir)
            })
        }
        if (rules.limit > 0) put("limit", rules.limit)
    }

    private fun encodeRule(rule: Rule): JsonObject = buildJsonObject {
        put("field", rule.field)
        put("op", rule.op)
        rule.value?.let { put("value", it) }
    }
}
