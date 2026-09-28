package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Casino
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.TonightPick
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.theme.Backhog

private val BUDGET_PRESETS = listOf(30, 60, 90, 120, 180)

/**
 * The web's PickDialog as a sheet: say how long tonight is, get four picks
 * with their human reasons, re-roll a category, or stop thinking entirely
 * with the random roll.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TonightSheet(
    container: AppContainer,
    baseUrl: String,
    onDismiss: () -> Unit,
    onOpenGame: (String) -> Unit,
) {
    val vm: TonightViewModel = viewModel(key = "tonight|$baseUrl") {
        TonightViewModel(container.session, baseUrl)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = p.c950,
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Filled.Casino, contentDescription = null, tint = p.hlBright)
                Text("What should I play?", style = MaterialTheme.typography.titleLarge, color = p.c100)
            }
            Text("Tonight's picks for ${Format.minutes(ui.minutes)}.", style = MaterialTheme.typography.bodyMedium, color = p.c400)

            // The budget row: presets plus a custom field.
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                BUDGET_PRESETS.forEach { preset ->
                    val selected = ui.minutes == preset
                    Text(
                        Format.minutes(preset),
                        style = MaterialTheme.typography.labelMedium,
                        color = if (selected) p.hlInk else p.c300,
                        modifier = Modifier
                            .clip(MaterialTheme.shapes.extraSmall)
                            .background(if (selected) p.hlMid else p.c850)
                            .clickable { vm.setMinutes(preset) }
                            .padding(horizontal = 10.dp, vertical = 8.dp),
                    )
                }
                CustomBudgetField(ui.minutes, vm::setMinutes)
            }

            ErrorText(ui.error)

            val playAndOpen: (Entry) -> Unit = { entry ->
                vm.play(entry) { played -> onDismiss(); onOpenGame(played.id) }
            }
            if (ui.loading && ui.picks == null) {
                Box(Modifier.fillMaxWidth().height(200.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
            } else {
                val picks = ui.picks
                if (picks != null) {
                    PickCard("Continue", "Something in progress", picks.continuePick, ui.playing, "continue", vm::reroll, playAndOpen, baseUrl)
                    PickCard("Short win", "Fits in your budget", picks.shortWin, ui.playing, "short_win", vm::reroll, playAndOpen, baseUrl)
                    PickCard("Wildcard", "Never even started", picks.wildcard, ui.playing, "wildcard", vm::reroll, playAndOpen, baseUrl)
                    PickCard("Backlog rescue", "Owned long, played little", picks.rescue, ui.playing, "rescue", vm::reroll, playAndOpen, baseUrl)
                }
            }

            ui.rolled?.let { rolled ->
                PickCard(
                    "Just rolled",
                    "Straight from the whole backlog",
                    TonightPick(entry = rolled, reason = "Picked at random — no thinking involved."),
                    ui.playing,
                    null,
                    null,
                    playAndOpen,
                    baseUrl,
                )
            }

            Row(
                Modifier.fillMaxWidth().padding(bottom = 24.dp),
                horizontalArrangement = Arrangement.End,
            ) {
                TextButton(onClick = onDismiss) { Text("Close") }
                Spacer(Modifier.width(8.dp))
                OutlinedButton(onClick = vm::roll, enabled = !ui.rolling && ui.rolled == null) {
                    if (ui.rolling) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    } else {
                        Icon(Icons.Filled.Refresh, contentDescription = null, modifier = Modifier.size(16.dp))
                        Spacer(Modifier.width(6.dp))
                        Text("Just roll one")
                    }
                }
            }
        }
    }
}

@Composable
private fun CustomBudgetField(minutes: Int, onSet: (Int) -> Unit) {
    var text by rememberSaveable { mutableStateOf(if (minutes in BUDGET_PRESETS) "" else minutes.toString()) }
    OutlinedTextField(
        value = text,
        onValueChange = { value ->
            text = value
            value.toIntOrNull()?.let { if (it in 10..1440) onSet(it) }
        },
        placeholder = { Text("custom") },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
        modifier = Modifier.width(104.dp),
        textStyle = MaterialTheme.typography.labelMedium,
    )
}

@Composable
private fun PickCard(
    label: String,
    blurb: String,
    pick: TonightPick?,
    playing: String?,
    category: String?,
    onReroll: ((String) -> Unit)?,
    onPlay: (Entry) -> Unit,
    baseUrl: String,
) {
    val p = Backhog.palette
    val accent = pick?.entry?.game?.accentHexOrNull?.let { parseHex(it) } ?: AccentFallback

    Column(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .background(p.c900)
            .border(1.dp, accent.copy(alpha = 0.3f), MaterialTheme.shapes.medium)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                label.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                fontWeight = FontWeight.SemiBold,
                color = p.c300,
            )
            Spacer(Modifier.width(6.dp))
            Text(blurb, style = MaterialTheme.typography.labelSmall, color = p.c500, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }

        if (pick == null) {
            Box(
                Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .border(1.dp, p.edgeStrong, MaterialTheme.shapes.small)
                    .padding(vertical = 22.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text("Nothing to suggest here.", style = MaterialTheme.typography.labelMedium, color = p.c600)
            }
        } else {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                CoverImage(pick.entry, baseUrl, Modifier.size(width = 56.dp, height = 74.dp).clip(MaterialTheme.shapes.small))
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(
                        pick.entry.title,
                        style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.SemiBold,
                        color = p.c100,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        pick.reason,
                        style = MaterialTheme.typography.bodySmall,
                        color = p.c300,
                        maxLines = 3,
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                        Button(
                            onClick = { onPlay(pick.entry) },
                            enabled = playing == null,
                            contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 14.dp, vertical = 6.dp),
                        ) {
                            if (playing == pick.entry.id) {
                                CircularProgressIndicator(modifier = Modifier.size(14.dp), strokeWidth = 2.dp)
                            } else {
                                Text("Play it")
                            }
                        }
                        if (onReroll != null && category != null) {
                            Text(
                                "Reroll",
                                style = MaterialTheme.typography.labelMedium,
                                color = p.c400,
                                modifier = Modifier
                                    .clip(CircleShape)
                                    .clickable { onReroll(category) }
                                    .padding(horizontal = 10.dp, vertical = 6.dp),
                            )
                        }
                    }
                }
            }
        }
    }
}
