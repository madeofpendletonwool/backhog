package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CardGiftcard
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.Game
import com.collinpendleton.backhog.api.SearchResult
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/**
 * The add-a-game sheet: debounced IGDB search, one row per result with cover
 * and summary, and the status choice the issue calls for — backlog (default)
 * or wishlist. When the server has no IGDB credentials the sheet says the
 * same thing the web says, and search stays disabled.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddGameSheet(container: AppContainer, baseUrl: String, onDismiss: () -> Unit) {
    val vm: AddGameViewModel = viewModel(key = "addgame|$baseUrl") {
        AddGameViewModel(container.session, baseUrl)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val focus = remember { FocusRequester() }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = p.c900,
    ) {
        Column(Modifier.padding(horizontal = 16.dp)) {
            OutlinedTextField(
                value = ui.term,
                onValueChange = vm::setTerm,
                placeholder = { Text("Search for a game…") },
                singleLine = true,
                enabled = !ui.degraded,
                leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null, tint = p.c500) },
                trailingIcon = {
                    if (ui.searching) CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp)
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(focus),
            )
            LaunchedEffect(Unit) { focus.requestFocus() }

            ErrorText(ui.error)

            Box(Modifier.heightIn(min = 200.dp)) {
                when {
                    ui.degraded -> SheetMessage(
                        title = "Game search isn't configured",
                        body = "Set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET in your .env file, then restart the stack.",
                    )
                    ui.error != null -> SheetMessage(title = "Search failed", body = ui.error ?: "", retry = vm::retry)
                    ui.term.trim().length < 2 -> SheetMessage(
                        title = "Find something to play",
                        body = "Type at least two characters to search IGDB.",
                    )
                    ui.searching && ui.results.isEmpty() -> Box(
                        Modifier.fillMaxWidth().padding(vertical = 40.dp),
                        contentAlignment = Alignment.Center,
                    ) { CircularProgressIndicator() }
                    ui.results.isEmpty() -> SheetMessage(
                        title = "No matches",
                        body = "Nothing found for \"${ui.term}\".",
                    )
                    else -> LazyColumn(
                        modifier = Modifier.heightIn(max = 420.dp),
                        verticalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        items(ui.results, key = { it.game.id }) { result ->
                            SearchResultRow(baseUrl, result, ui, vm::add)
                        }
                        item { Spacer(Modifier.height(12.dp)) }
                    }
                }
            }
            Spacer(Modifier.height(20.dp))
        }
    }
}

@Composable
private fun SheetMessage(title: String, body: String, retry: (() -> Unit)? = null) {
    val p = Backhog.palette
    Column(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(title, style = MaterialTheme.typography.titleSmall, color = p.c100)
        Text(
            body,
            style = MaterialTheme.typography.bodySmall,
            color = p.c400,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
        if (retry != null) {
            OutlinedButton(onClick = retry) { Text("Try again") }
        }
    }
}

@Composable
private fun SearchResultRow(
    baseUrl: String,
    result: SearchResult,
    ui: AddGameUiState,
    onAdd: (Long, EntryStatus) -> Unit,
) {
    val p = Backhog.palette
    val game = result.game
    val owned = ui.owned(result)
    val pending = ui.adding == game.id
    val accent = game.accentHexOrNull?.let { parseHex(it) } ?: AccentFallback

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .background(if (owned) p.c850 else p.c900)
            .clickable(enabled = !owned && !pending) { onAdd(game.id, EntryStatus.Backlog) }
            .padding(10.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            SearchCover(baseUrl, game, accent)
            Column(Modifier.weight(1f)) {
                Text(
                    game.name,
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.Medium,
                    color = p.c100,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val meta = listOfNotNull(
                    Format.releaseYear(game.firstReleaseDate).takeIf { it.isNotEmpty() },
                    game.igdbRating?.let { "★ ${Math.round(it)}" },
                    game.timeToBeatMain?.let { Format.duration(it) }?.takeIf { it != "—" },
                    game.genres.take(2).joinToString(", ").takeIf { it.isNotEmpty() },
                ).joinToString(" · ")
                if (meta.isNotEmpty()) {
                    Text(meta, style = MaterialTheme.typography.labelSmall, color = p.c400, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                if (game.summary.isNotBlank()) {
                    Text(
                        game.summary,
                        style = MaterialTheme.typography.labelSmall,
                        color = p.c500,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
            when {
                pending -> Box(Modifier.fillMaxWidth().padding(vertical = 8.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp)
                }
                owned -> Row(
                    modifier = Modifier.fillMaxWidth().padding(vertical = 6.dp),
                    horizontalArrangement = Arrangement.Center,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(Icons.Filled.Check, contentDescription = null, tint = Tones.Played, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("In library", style = MaterialTheme.typography.labelMedium, color = Tones.Played)
                }
                else -> {
                    Button(onClick = { onAdd(game.id, EntryStatus.Backlog) }, modifier = Modifier.weight(1f)) {
                        Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(16.dp))
                        Spacer(Modifier.width(4.dp))
                        Text("Add — Backlog")
                    }
                    OutlinedButton(onClick = { onAdd(game.id, EntryStatus.Wishlist) }, modifier = Modifier.weight(1f)) {
                        Icon(Icons.Filled.CardGiftcard, contentDescription = null, modifier = Modifier.size(16.dp))
                        Spacer(Modifier.width(4.dp))
                        Text("Wishlist")
                    }
                }
            }
        }
    }
}

@Composable
private fun SearchCover(baseUrl: String, game: Game, accent: androidx.compose.ui.graphics.Color) {
    Box(
        Modifier
            .size(width = 44.dp, height = 58.dp)
            .clip(MaterialTheme.shapes.small)
            .background(accent.copy(alpha = 0.15f)),
        contentAlignment = Alignment.Center,
    ) {
        coil3.compose.AsyncImage(
            model = com.collinpendleton.backhog.data.ServerUrl.gameCoverUrl(baseUrl, game.id),
            contentDescription = null,
            contentScale = androidx.compose.ui.layout.ContentScale.Crop,
            modifier = Modifier.size(width = 44.dp, height = 58.dp),
        )
    }
}
