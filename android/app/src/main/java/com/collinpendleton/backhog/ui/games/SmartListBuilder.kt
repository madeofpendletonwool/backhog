package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.Rule
import com.collinpendleton.backhog.api.RuleSet
import com.collinpendleton.backhog.api.SmartField
import com.collinpendleton.backhog.api.SmartLists
import kotlinx.serialization.json.JsonPrimitive

/**
 * The smart rule builder — the port of the web's SmartListBuilder. Fields and
 * operators come from the API so the builder can never offer something the
 * server-side whitelist would reject. The arena split hides the other arena's
 * fields from a scoped set; saved rules stay visible even when out of scope,
 * exactly like the web.
 */
@Composable
fun SmartListBuilder(
    value: RuleSet,
    fields: List<SmartField>,
    /** The arena the set is being built for — its fields when the set is unscoped. */
    arenaMedia: String,
    onChange: (RuleSet) -> Unit,
) {
    val p = com.collinpendleton.backhog.ui.theme.Backhog.palette
    val byKey = fields.associateBy { it.key }
    val target = SmartLists.ruleSetTarget(value) ?: arenaMedia
    val scoped = fields.filter { it.media == null || it.media == target }
    var newFieldKey by remember { mutableStateOf("") }

    fun patchRule(index: Int, rule: Rule) {
        onChange(value.copy(rules = value.rules.mapIndexed { i, r -> if (i == index) rule else r }))
    }

    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        if (fields.isEmpty()) {
            Text("Loading fields…", style = MaterialTheme.typography.bodySmall, color = p.c500)
            return
        }

        // Match all/any of these conditions.
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("Match", style = MaterialTheme.typography.bodyMedium, color = p.c400)
            SelectPill(
                selected = value.match,
                options = listOf("all", "any"),
                onPick = { onChange(value.copy(match = it)) },
            )
            Text("of these conditions:", style = MaterialTheme.typography.bodyMedium, color = p.c400)
        }

        if (value.rules.isEmpty()) {
            Text(
                "No conditions yet — this list would match every ${if (target == "book") "book" else "game"} in your library.",
                style = MaterialTheme.typography.labelMedium,
                color = p.c500,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .border(1.dp, p.edgeStrong, MaterialTheme.shapes.small)
                    .padding(horizontal = 14.dp, vertical = 18.dp),
            )
        }

        value.rules.forEachIndexed { index, rule ->
            val field = byKey[rule.field] ?: return@forEachIndexed
            RuleRow(
                rule = rule,
                field = field,
                // The rule's own field stays selectable even when out of scope.
                offered = if (scoped.none { it.key == rule.field }) listOf(field) + scoped else scoped,
                media = target,
                onPatch = { patchRule(index, it) },
                onRemove = { onChange(value.copy(rules = value.rules.filterIndexed { i, _ -> i != index })) },
            )
        }

        // Add a condition.
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Box(Modifier.weight(1f)) {
                var open by remember { mutableStateOf(false) }
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clip(MaterialTheme.shapes.small)
                        .border(1.dp, p.edgeStrong, MaterialTheme.shapes.small)
                        .clickable { open = true }
                        .padding(horizontal = 12.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        byKey[newFieldKey]?.label ?: "Add a condition…",
                        style = MaterialTheme.typography.bodySmall,
                        color = if (newFieldKey.isEmpty()) p.c500 else p.c200,
                        modifier = Modifier.weight(1f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Icon(Icons.Filled.KeyboardArrowDown, contentDescription = null, tint = p.c500)
                }
                DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                    scoped.forEach { option ->
                        DropdownMenuItem(
                            text = { Text(option.label) },
                            onClick = {
                                open = false
                                newFieldKey = option.key
                            },
                        )
                    }
                }
            }
            OutlinedButton(
                onClick = {
                    val field = byKey[newFieldKey] ?: scoped.firstOrNull() ?: return@OutlinedButton
                    onChange(value.copy(rules = value.rules + SmartLists.emptyRule(field)))
                    newFieldKey = ""
                },
                enabled = newFieldKey.isNotEmpty(),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 12.dp, vertical = 6.dp),
            ) {
                Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(14.dp))
                Text("Add")
            }
        }

        // Sort by field + direction. A saved sort key survives a scope change that hides it.
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("Sort by", style = MaterialTheme.typography.labelMedium, color = p.c400)
            val sorts = SmartLists.sortFields.filter { it.media == null || it.media == target }
            val options = buildList {
                addAll(sorts.map { it.value to it.label })
                if (value.sort != null && sorts.none { it.value == value.sort!!.field }) {
                    add(value.sort!!.field to value.sort!!.field)
                }
            }
            SelectPill(
                selected = value.sort?.field ?: "added",
                options = options.map { it.second },
                labelOf = { picked -> options.firstOrNull { it.second == picked }?.first ?: picked },
                onPick = { picked ->
                    val key = options.firstOrNull { it.second == picked }?.first ?: picked
                    onChange(value.copy(sort = value.sort?.copy(field = key) ?: com.collinpendleton.backhog.api.RuleSort(key)))
                },
                modifier = Modifier.weight(1f),
            )
            SelectPill(
                selected = value.sort?.dir ?: "desc",
                options = listOf("asc", "desc"),
                onPick = { dir ->
                    onChange(value.copy(sort = (value.sort ?: com.collinpendleton.backhog.api.RuleSort("added")).copy(dir = dir)))
                },
            )
        }
    }
}

@Composable
private fun SelectPill(
    selected: String,
    options: List<String>,
    onPick: (String) -> Unit,
    modifier: Modifier = Modifier,
    labelOf: (String) -> String = { it },
) {
    val p = com.collinpendleton.backhog.ui.theme.Backhog.palette
    Box(modifier) {
        var open by remember { mutableStateOf(false) }
        Row(
            Modifier
                .fillMaxWidth()
                .clip(MaterialTheme.shapes.small)
                .border(1.dp, p.edgeStrong, MaterialTheme.shapes.small)
                .clickable { open = true }
                .padding(horizontal = 10.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                labelOf(selected),
                style = MaterialTheme.typography.labelMedium,
                color = p.c200,
                modifier = Modifier.weight(1f),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Icon(Icons.Filled.KeyboardArrowDown, contentDescription = null, tint = p.c500, modifier = Modifier.size(16.dp))
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            options.forEach { option ->
                DropdownMenuItem(
                    text = { Text(labelOf(option)) },
                    trailingIcon = if (option == selected) {
                        { Icon(Icons.Filled.Check, contentDescription = null, tint = p.hlBright) }
                    } else null,
                    onClick = {
                        open = false
                        onPick(option)
                    },
                )
            }
        }
    }
}

@Composable
private fun RuleRow(
    rule: Rule,
    field: SmartField,
    offered: List<SmartField>,
    media: String,
    onPatch: (Rule) -> Unit,
    onRemove: () -> Unit,
) {
    val p = com.collinpendleton.backhog.ui.theme.Backhog.palette

    Column(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.small)
            .background(p.c850)
            .padding(8.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Box(Modifier.weight(1f)) {
                SelectPill(
                    selected = field.key,
                    options = offered.map { it.label },
                    onPick = { label ->
                        offered.firstOrNull { it.label == label }?.let { next ->
                            onPatch(SmartLists.emptyRule(next))
                        }
                    },
                )
            }
            SelectPill(
                selected = rule.op,
                options = field.ops,
                labelOf = { SmartLists.opLabel(it) },
                onPick = { op -> onPatch(rule.copy(op = op)) },
            )
            IconButton(onClick = onRemove, modifier = Modifier.size(30.dp)) {
                Icon(Icons.Filled.Close, contentDescription = "Remove condition", tint = p.c600, modifier = Modifier.size(15.dp))
            }
        }

        if (rule.op !in SmartLists.valuelessOps) {
            ValueInput(rule, field, media, onPatch)
        }
    }
}

@Composable
private fun ValueInput(rule: Rule, field: SmartField, media: String, onPatch: (Rule) -> Unit) {
    val p = com.collinpendleton.backhog.ui.theme.Backhog.palette

    // Multi-value operators send an array; the server expects names for refs.
    if (rule.op == "in" || rule.op == "not_in") {
        if (field.type == "enum") {
            val values = SmartLists.listValue(rule)
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                field.enum.forEach { option ->
                    val active = option in values
                    Text(
                        if (field.key == "status") statusLabelFor(option, media) else option,
                        style = MaterialTheme.typography.labelSmall,
                        color = if (active) p.hlInk else p.c400,
                        modifier = Modifier
                            .clip(MaterialTheme.shapes.extraSmall)
                            .background(if (active) p.hlMid else p.c800)
                            .clickable {
                                val next = if (active) values - option else values + option
                                onPatch(
                                    rule.copy(
                                        value = kotlinx.serialization.json.JsonArray(next.map { JsonPrimitive(it) }),
                                    ),
                                )
                            }
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
        } else {
            OutlinedTextField(
                value = SmartLists.listValue(rule).joinToString(", "),
                onValueChange = { text -> onPatch(rule.copy(value = SmartLists.listFromText(text))) },
                placeholder = { Text("RPG, Indie") },
                singleLine = true,
                textStyle = MaterialTheme.typography.labelMedium,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        return
    }

    when (field.type) {
        "enum" -> SelectPill(
            selected = SmartLists.stringValue(rule),
            options = field.enum.map { if (field.key == "status") statusLabelFor(it, media) else it },
            onPick = { label ->
                val value = if (field.key == "status") {
                    field.enum.firstOrNull { statusLabelFor(it, media) == label } ?: label
                } else label
                onPatch(rule.copy(value = JsonPrimitive(value)))
            },
        )
        "number" -> OutlinedTextField(
            value = SmartLists.numberValue(rule)?.let { if (it == it.toLong().toDouble()) it.toLong().toString() else it.toString() } ?: "",
            onValueChange = { text ->
                onPatch(rule.copy(value = SmartLists.numberFromText(text)))
            },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            textStyle = MaterialTheme.typography.labelMedium,
            modifier = Modifier.width(140.dp),
        )
        else -> OutlinedTextField(
            value = SmartLists.stringValue(rule),
            onValueChange = { text -> onPatch(rule.copy(value = JsonPrimitive(text))) },
            singleLine = true,
            textStyle = MaterialTheme.typography.labelMedium,
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

private fun statusLabelFor(key: String, media: String): String =
    EntryStatus.fromKey(key)?.label(media == "book") ?: key
