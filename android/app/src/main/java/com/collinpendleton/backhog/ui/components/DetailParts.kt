package com.collinpendleton.backhog.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.ui.theme.Backhog

/*
 * The pieces the book and game detail pages share. On a phone a detail page
 * reads as one scrolling sheet broken by headings and hairlines — not the
 * web's stack of bordered panels, which on a narrow screen spends a third of
 * the width on padding and borders.
 */

/** A titled stretch of a detail page, set off from the one above by a hairline. */
@Composable
fun DetailSection(
    title: String,
    modifier: Modifier = Modifier,
    action: (@Composable () -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val p = Backhog.palette
    Column(modifier.fillMaxWidth()) {
        HorizontalDivider(color = p.edge)
        Row(
            Modifier
                .fillMaxWidth()
                .padding(top = 16.dp, bottom = 8.dp)
                .height(if (action != null) 40.dp else 24.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                title,
                style = MaterialTheme.typography.titleMedium,
                color = p.c100,
                modifier = Modifier.weight(1f),
            )
            action?.invoke()
        }
        Column(
            Modifier
                .fillMaxWidth()
                .padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            content = content,
        )
    }
}

/**
 * The entry's status as one button — the current state, in its colour — that
 * opens the full list. Six options don't fit a phone's width as chips or as
 * a labelled segmented row, and unlabelled icons make you guess.
 */
@Composable
fun StatusPicker(
    current: EntryStatus,
    label: (EntryStatus) -> String,
    tone: (EntryStatus) -> Color,
    onPick: (EntryStatus) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    options: List<EntryStatus> = EntryStatus.all,
) {
    val p = Backhog.palette
    var open by remember { mutableStateOf(false) }
    val shape = MaterialTheme.shapes.small
    val currentTone = tone(current)
    Box(modifier) {
        Row(
            Modifier
                .fillMaxWidth()
                .height(48.dp)
                .clip(shape)
                .background(currentTone.copy(alpha = 0.14f))
                .border(1.dp, currentTone.copy(alpha = 0.4f), shape)
                .clickable(enabled = enabled) { open = true }
                .alpha(if (enabled) 1f else 0.6f)
                .padding(start = 14.dp, end = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            StatusDot(currentTone)
            Spacer(Modifier.width(10.dp))
            Text(
                label(current),
                style = MaterialTheme.typography.labelLarge,
                color = p.toneInk(currentTone),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Icon(Icons.Filled.ArrowDropDown, contentDescription = "Change status", tint = p.c300)
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            options.forEach { status ->
                DropdownMenuItem(
                    text = { Text(label(status)) },
                    leadingIcon = { StatusDot(tone(status)) },
                    trailingIcon = if (status == current) {
                        { Icon(Icons.Filled.Check, contentDescription = null, tint = p.hlBright) }
                    } else null,
                    onClick = {
                        open = false
                        if (status != current) onPick(status)
                    },
                )
            }
        }
    }
}

@Composable
private fun StatusDot(tone: Color) {
    Box(
        Modifier
            .size(10.dp)
            .clip(CircleShape)
            .background(tone),
    )
}

/**
 * 1–10 as one row of equal cells that always fits the width; the cells up to
 * the score light, and tapping the current score clears it.
 */
@Composable
fun RatingBar(current: Int?, onPick: (Int?) -> Unit, enabled: Boolean = true) {
    val p = Backhog.palette
    val lit = com.collinpendleton.backhog.ui.theme.Tones.Wishlist
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            (1..10).forEach { score ->
                val active = score <= (current ?: 0)
                Box(
                    Modifier
                        .weight(1f)
                        .height(40.dp)
                        .clip(MaterialTheme.shapes.extraSmall)
                        .background(if (active) lit.copy(alpha = 0.9f) else p.c800)
                        .clickable(enabled = enabled) { onPick(if (current == score) null else score) },
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "$score",
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = if (active) Color(0xFF141019) else p.c500,
                    )
                }
            }
        }
        Text(
            if (current != null) "$current / 10 — tap it again to clear" else "Not rated yet",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )
    }
}

/** Long prose, clamped, with a Read more that only appears when something was cut. */
@Composable
fun ExpandableText(text: String, collapsedLines: Int = 5) {
    val p = Backhog.palette
    var expanded by remember { mutableStateOf(false) }
    var clipped by remember(text) { mutableStateOf(false) }
    Column {
        Text(
            text,
            style = MaterialTheme.typography.bodyMedium,
            color = p.c300,
            maxLines = if (expanded) Int.MAX_VALUE else collapsedLines,
            overflow = TextOverflow.Ellipsis,
            onTextLayout = { if (!expanded) clipped = it.hasVisualOverflow },
        )
        if (clipped || expanded) {
            TextButton(
                onClick = { expanded = !expanded },
                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 0.dp),
            ) { Text(if (expanded) "Show less" else "Read more") }
        }
    }
}

/** Label on the left, value on the right — the dates and small facts. */
@Composable
fun FactLine(label: String, value: String) {
    val p = Backhog.palette
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = p.c500, modifier = Modifier.weight(1f))
        Text(value, style = MaterialTheme.typography.bodyMedium, color = p.c300)
    }
}
