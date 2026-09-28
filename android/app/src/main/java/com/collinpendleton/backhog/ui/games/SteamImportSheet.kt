package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.CloudDone
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.SteamMatch
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/**
 * Bulk-import a Steam library — the web's SteamImportDialog. Steam appids map
 * to IGDB through the external_games table: an exact join, not a name guess.
 * Unmatched rows are shown as skipped; the server's no-key and
 * private-profile failures surface readably.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SteamImportSheet(
    container: AppContainer,
    baseUrl: String,
    onDismiss: () -> Unit,
    onLibraryChanged: () -> Unit,
) {
    val vm: SteamImportViewModel = viewModel(key = "steam|$baseUrl") {
        SteamImportViewModel(container.session, baseUrl)
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
                .padding(horizontal = 20.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            when {
                // The server has no STEAM_API_KEY: say exactly that, like the web.
                ui.unavailable -> {
                    Text("Steam import isn't configured", style = MaterialTheme.typography.titleLarge, color = p.c100)
                    Text(
                        "Steam import is not configured: set STEAM_API_KEY and restart the server. You can get one free at steamcommunity.com/dev/apikey.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = p.c400,
                    )
                    Button(onClick = onDismiss, modifier = Modifier.align(Alignment.End).padding(bottom = 24.dp)) { Text("Close") }
                }

                ui.result != null -> {
                    Column(
                        Modifier.fillMaxWidth().padding(vertical = 24.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Icon(Icons.Filled.CloudDone, contentDescription = null, tint = Tones.Played, modifier = Modifier.size(44.dp))
                        val added = ui.result!!.added
                        Text(
                            "Added $added game${if (added == 1) "" else "s"}",
                            style = MaterialTheme.typography.titleMedium,
                            color = p.c100,
                        )
                        if (ui.result!!.skipped > 0) {
                            Text(
                                "${ui.result!!.skipped} skipped (already in your library)",
                                style = MaterialTheme.typography.bodySmall,
                                color = p.c500,
                            )
                        }
                        Button(onClick = onDismiss) { Text("Done") }
                    }
                }

                else -> {
                    Text("Import from Steam", style = MaterialTheme.typography.titleLarge, color = p.c100)
                    Text(
                        "Pull in the games you own. Your Steam profile's game details must be public.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = p.c400,
                    )

                    OutlinedTextField(
                        value = ui.steamId,
                        onValueChange = vm::setSteamId,
                        label = { Text("SteamID, vanity name or profile URL") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )

                    ErrorText(ui.previewError)

                    Button(onClick = vm::preview, enabled = ui.steamId.isNotBlank() && !ui.previewing) {
                        if (ui.previewing) {
                            CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                        } else {
                            Text("Preview")
                        }
                    }

                    ui.preview?.let { preview ->
                        Text(
                            "${preview.total} games on that profile · ${preview.matches.size - preview.unmatched} matched to IGDB · ${preview.unmatched} skipped",
                            style = MaterialTheme.typography.labelMedium,
                            color = p.c500,
                        )
                        StatusPicker(ui.status, vm::setStatus)

                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            TextButton(onClick = vm::selectAll) { Text("Select all") }
                            TextButton(onClick = vm::selectNone) { Text("Select none") }
                        }

                        LazyColumn(
                            modifier = Modifier.fillMaxWidth().height(380.dp),
                            verticalArrangement = Arrangement.spacedBy(2.dp),
                        ) {
                            items(preview.matches, key = { it.appId }) { match ->
                                SteamMatchRow(match, ui.selected, vm::toggle)
                            }
                        }

                        Row(Modifier.fillMaxWidth().padding(bottom = 24.dp), horizontalArrangement = Arrangement.End) {
                            TextButton(onClick = onDismiss) { Text("Cancel") }
                            Spacer(Modifier.width(8.dp))
                            Button(
                                onClick = { vm.runImport(onLibraryChanged) },
                                enabled = ui.selected.isNotEmpty() && !ui.importing,
                            ) {
                                if (ui.importing) {
                                    CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                                } else {
                                    Text("Import ${ui.selected.size}")
                                }
                            }
                        }
                    }

                    if (ui.preview == null && !ui.previewing) {
                        Spacer(Modifier.height(24.dp))
                    }
                }
            }
        }
    }
}

/** The quick status set the import can land games in. */
@Composable
private fun StatusPicker(selected: EntryStatus, onPick: (EntryStatus) -> Unit) {
    val p = Backhog.palette
    Row(
        Modifier.horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text("Add as", style = MaterialTheme.typography.labelMedium, color = p.c400)
        listOf(EntryStatus.Backlog, EntryStatus.Playing, EntryStatus.Played, EntryStatus.Wishlist).forEach { status ->
            val active = status == selected
            Text(
                status.gameLabel,
                style = MaterialTheme.typography.labelSmall,
                color = if (active) Tones.forStatus(status) else p.c500,
                modifier = Modifier
                    .clip(MaterialTheme.shapes.extraSmall)
                    .background(if (active) Tones.forStatus(status).copy(alpha = 0.15f) else p.c850)
                    .clickable { onPick(status) }
                    .padding(horizontal = 10.dp, vertical = 6.dp),
            )
        }
    }
}

@Composable
private fun SteamMatchRow(match: SteamMatch, selected: Set<Long>, onToggle: (Long) -> Unit) {
    val p = Backhog.palette
    val game = match.game
    val importable = game != null && !match.inLibrary
    val checked = game?.id in selected

    Row(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.small)
            .background(if (checked) p.fillActive else androidx.compose.ui.graphics.Color.Transparent)
            .let { m -> if (importable) m.clickable { game?.let { onToggle(it.id) } } else m.alpha(0.55f) }
            .padding(horizontal = 4.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        if (importable) {
            Checkbox(checked = checked, onCheckedChange = null)
        } else {
            Icon(
                Icons.Filled.CheckCircle,
                contentDescription = null,
                tint = if (match.inLibrary) Tones.Played.copy(alpha = 0.5f) else p.c700,
                modifier = Modifier.size(20.dp).padding(2.dp),
            )
        }
        Column(Modifier.weight(1f)) {
            Text(
                game?.name ?: match.steamName,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Medium,
                color = p.c100,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                when {
                    match.inLibrary -> "already in your library"
                    game != null -> "matched by Steam appid"
                    else -> "no IGDB match — will be skipped"
                },
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
    }
}
