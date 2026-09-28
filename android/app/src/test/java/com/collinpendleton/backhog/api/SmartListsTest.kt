package com.collinpendleton.backhog.api

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The smart-list helpers: scoping, defaults, and the wire encoding. */
class SmartListsTest {
    @Test fun `a set is scoped by its media_type rules`() {
        val book = RuleSet(
            rules = listOf(
                Rule("media_type", "eq", JsonPrimitive("book")),
                Rule("status", "eq", JsonPrimitive("backlog")),
            ),
        )
        assertEquals("book", SmartLists.ruleSetTarget(book))

        val game = RuleSet(
            rules = listOf(Rule("media_type", "in", JsonArray(listOf(JsonPrimitive("game"))))),
        )
        assertEquals("game", SmartLists.ruleSetTarget(game))

        assertNull(SmartLists.ruleSetTarget(RuleSet(rules = listOf(Rule("status", "eq", JsonPrimitive("backlog"))))))
        assertNull(SmartLists.ruleSetTarget(null))
    }

    @Test fun `mixed in-lists are unscoped`() {
        val mixed = RuleSet(
            rules = listOf(
                Rule("media_type", "in", JsonArray(listOf(JsonPrimitive("game"), JsonPrimitive("book")))),
            ),
        )
        assertNull(SmartLists.ruleSetTarget(mixed))
    }

    @Test fun `default rules scope to the arena they were built in`() {
        val rules = SmartLists.defaultRules("game")
        assertEquals("all", rules.match)
        assertEquals(2, rules.rules.size)
        assertEquals("media_type", rules.rules[0].field)
        assertEquals("game", rules.rules[0].value!!.jsonPrimitive.content)
        assertEquals("added", rules.sort?.field)
        assertEquals("desc", rules.sort?.dir)
    }

    @Test fun `empty rules default to the field's first operator and a type-appropriate value`() {
        val enumField = SmartField(key = "status", label = "Status", type = "enum", ops = listOf("eq", "neq"), enum = listOf("backlog", "playing"))
        val rule = SmartLists.emptyRule(enumField)
        assertEquals("eq", rule.op)
        assertEquals("backlog", rule.value!!.jsonPrimitive.content)

        val refField = SmartField(key = "genre", label = "Genre", type = "ref", ops = listOf("in", "not_in"))
        assertEquals(0, SmartLists.emptyRule(refField).value!!.jsonArray.size)

        val numberField = SmartField(key = "hours_logged", label = "Hours", type = "number", ops = listOf("gt", "lt"))
        assertEquals("", SmartLists.emptyRule(numberField).value!!.jsonPrimitive.content)
    }

    @Test fun `values read back by type`() {
        val text = Rule("name", "contains", JsonPrimitive("elden"))
        assertEquals("elden", SmartLists.stringValue(text))

        val number = Rule("release_year", "gt", JsonPrimitive(2018))
        assertEquals(2018.0, SmartLists.numberValue(number)!!, 0.001)
        // A text value that parses still reads as a number.
        val numericText = Rule("release_year", "gt", JsonPrimitive("2018"))
        assertEquals(2018.0, SmartLists.numberValue(numericText)!!, 0.001)

        val list = Rule("genre", "in", JsonArray(listOf(JsonPrimitive("RPG"), JsonPrimitive("Indie"))))
        assertEquals(listOf("RPG", "Indie"), SmartLists.listValue(list))
    }

    @Test fun `multi-value text splits and trims like the web`() {
        val value = SmartLists.listFromText("RPG, Indie, , Tactics ")
        assertEquals(listOf("RPG", "Indie", "Tactics"), value.jsonArray.map { it.jsonPrimitive.content })
    }

    @Test fun `numbers go over the wire as numbers`() {
        assertEquals(20.0, (SmartLists.numberFromText("20") as JsonPrimitive).content.toDouble(), 0.001)
        assertNull(SmartLists.numberFromText(""))
    }

    @Test fun `the rule set encodes exactly what the server compiler expects`() {
        val encoded = SmartLists.encodeRuleSet(
            RuleSet(
                match = "any",
                rules = listOf(
                    Rule("media_type", "eq", JsonPrimitive("game")),
                    Rule("release_year", "gte", JsonPrimitive(2015)),
                    Rule("genre", "in", JsonArray(listOf(JsonPrimitive("RPG")))),
                ),
                sort = com.collinpendleton.backhog.api.RuleSort("added", "asc"),
            ),
        ).toString()
        // Key order follows the builder, values keep their JSON types.
        assertEquals(
            """{"match":"any","rules":[""" +
                """{"field":"media_type","op":"eq","value":"game"},""" +
                """{"field":"release_year","op":"gte","value":2015},""" +
                """{"field":"genre","op":"in","value":["RPG"]}""" +
                """],"sort":{"field":"added","dir":"asc"}}""",
            encoded,
        )
    }

    @Test fun `an empty set omits sort and limit`() {
        val encoded = SmartLists.encodeRuleSet(RuleSet(match = "all"))
        assertEquals("""{"match":"all","rules":[]}""", encoded.toString())
    }

    @Test fun `operator labels match the web dropdowns`() {
        assertEquals("is", SmartLists.opLabel("eq"))
        assertEquals("is none of", SmartLists.opLabel("not_in"))
        assertEquals("is not set", SmartLists.opLabel("is_null"))
        assertEquals("wat", SmartLists.opLabel("wat"))
        assertTrue("is_null" in SmartLists.valuelessOps)
    }
}
