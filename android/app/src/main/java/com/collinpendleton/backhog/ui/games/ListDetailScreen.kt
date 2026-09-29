package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectDragGesturesAfterLongPress
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DeleteOutline
import androidx.compose.material.icons.filled.DragIndicator
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/**
 * One list, manual or smart — the web's ListDetailPage. Manual lists drag to
 * reorder and take their members by picker; smart lists show the rule editor.
 */
@Composable
fun ListDetailScreen(
    container: AppContainer,
    baseUrl: String,
    listId: String,
    onBack: () -> Unit,
    onRemoved: () -> Unit,
    onOpenGame: (String) -> Unit,
) {
    val vm: ListDetailViewModel = viewModel(key = "list|$baseUrl|$listId") {
        ListDetailViewModel(container.session, baseUrl, listId)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette

    // Navigating is a side effect: from composition it fires on every
    // recomposition until the screen leaves, popping more than this page.
    androidx.compose.runtime.LaunchedEffect(ui.deleted) { if (ui.deleted) onRemoved() }
    if (ui.deleted) return

    Column(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
        Row(
            Modifier.fillMaxWidth().padding(top = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack, modifier = Modifier.size(36.dp)) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back", tint = p.c300)
            }
            Column(Modifier.weight(1f)) {
                Text(
                    ui.list?.name ?: "List",
                    style = MaterialTheme.typography.headlineSmall,
                    color = p.c100,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val count = ui.entries.size
                Text(
                    buildString {
                        append("$count ${if (count == 1) "item" else "items"}")
                        if (ui.totalHours > 0) append(" · ${Format.hours(ui.totalHours)}")
                        if (!ui.isSmart) append(" · drag to reorder")
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = p.c400,
                )
            }
            IconButton(onClick = vm::startEdit, enabled = !ui.busy) {
                Icon(Icons.Filled.Edit, contentDescription = "Edit list", tint = p.c300)
            }
            if (!ui.isSmart) {
                IconButton(onClick = vm::openAdd) {
                    Icon(Icons.Filled.Add, contentDescription = "Add games", tint = p.hlBright)
                }
            }
        }

        ErrorText(ui.actionError)

        when {
            ui.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            ui.error != null -> Column(
                Modifier.fillMaxSize(),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterVertically),
            ) {
                Text("That list doesn't exist.", color = p.c300)
                TextButton(onClick = onBack) { Text("Back to lists") }
            }
            ui.entries.isEmpty() -> Column(
                Modifier.fillMaxSize().padding(vertical = 48.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("Nothing in this list yet", style = MaterialTheme.typography.titleMedium, color = p.c100)
                Text(
                    if (ui.isSmart) "No games match these rules right now — loosen them, or add more to your library."
                    else "Add games from the button above.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = p.c400,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
            }
            else -> ManualEntryList(ui, baseUrl, vm, onOpenGame)
        }
    }

    if (ui.editing) {
        EditListSheet(vm, onDismiss = vm::cancelEdit)
    }
    if (ui.adding) {
        AddEntriesSheet(vm, baseUrl, onOpenGame)
    }
}

/** Manual lists: dense rows with drag handles; smart lists render the same rows, minus the handle. */
@Composable
private fun ManualEntryList(
    ui: ListDetailUiState,
    baseUrl: String,
    vm: ListDetailViewModel,
    onOpenGame: (String) -> Unit,
) {
    val listState = rememberLazyListState()
    val drag = remember {
        QueueDragState(
            listState = listState,
            onMoveLocal = { from, to -> vm.moveLocal(from, to) },
            onCommit = { id -> vm.commit(id) },
        )
    }

    LazyColumn(state = listState, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        items(ui.entries, key = { it.id }) { entry ->
            val dragging = drag.draggingKey == entry.id
            ListEntryRow(
                entry = entry,
                baseUrl = baseUrl,
                draggable = !ui.isSmart,
                dragging = dragging,
                dragOffset = drag.dragOffset,
                onDragStart = { drag.start(entry.id) },
                onDrag = drag::drag,
                onDragEnd = drag::end,
                onOpen = { onOpenGame(entry.id) },
                onRemove = { vm.removeEntry(entry.id) },
                canRemove = !ui.isSmart,
            )
        }
        item { Spacer(Modifier.height(20.dp)) }
    }
}

@Composable
private fun ListEntryRow(
    entry: Entry,
    baseUrl: String,
    draggable: Boolean,
    dragging: Boolean,
    dragOffset: Float,
    onDragStart: () -> Unit,
    onDrag: (Float) -> Unit,
    onDragEnd: () -> Unit,
    onOpen: () -> Unit,
    onRemove: () -> Unit,
    canRemove: Boolean,
) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.medium

    Row(
        Modifier
            .fillMaxWidth()
            .let { m ->
                if (dragging) {
                    m.graphicsLayer { translationY = dragOffset }
                        .zIndex(10f)
                        .border(1.dp, p.hlMid.copy(alpha = 0.5f), shape)
                } else {
                    m.border(1.dp, p.edgeStrong, shape)
                }
            }
            .clip(shape)
            .background(p.c900)
            .padding(horizontal = 4.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        if (draggable) {
            Icon(
                Icons.Filled.DragIndicator,
                contentDescription = "Reorder ${entry.title}",
                tint = p.c600,
                modifier = Modifier
                    .pointerInput(Unit) {
                        detectDragGesturesAfterLongPress(
                            onDragStart = { onDragStart() },
                            onDrag = { change, amount ->
                                change.consume()
                                onDrag(amount.y)
                            },
                            onDragEnd = { onDragEnd() },
                            onDragCancel = { onDragEnd() },
                        )
                    }
                    .size(26.dp),
            )
        }
        CoverImage(entry, baseUrl, Modifier.size(width = 40.dp, height = 52.dp).clip(MaterialTheme.shapes.small))
        Column(
            Modifier
                .weight(1f)
                .clickable(onClick = onOpen),
        ) {
            Text(
                entry.title,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Medium,
                color = p.c100,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            val meta = buildList {
                Format.releaseYear(entry.game?.firstReleaseDate).takeIf { it.isNotEmpty() }?.let { add(it) }
                entry.game?.timeToBeatMain?.let { add(Format.duration(it)) }
                entry.game?.igdbRating?.let { add("★ ${Math.round(it)}") }
            }.joinToString(" · ")
            if (meta.isNotEmpty()) {
                Text(meta, style = MaterialTheme.typography.labelSmall, color = p.c500, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        ToneChip(entry.status.gameLabel, Tones.forStatus(entry.status))
        if (canRemove) {
            IconButton(onClick = onRemove, modifier = Modifier.size(30.dp)) {
                Icon(Icons.Filled.Close, contentDescription = "Remove ${entry.title}", tint = p.c600, modifier = Modifier.size(15.dp))
            }
        }
    }
}

/** Rename (both kinds) and re-rule (smart) — the web's edit dialog. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun EditListSheet(vm: ListDetailViewModel, onDismiss: () -> Unit) {
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette
    var confirmDelete by remember { mutableStateOf(false) }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = p.c950,
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 20.dp)
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text("Edit list", style = MaterialTheme.typography.titleLarge, color = p.c100)
            OutlinedTextField(
                value = ui.draftName,
                onValueChange = vm::setDraftName,
                label = { Text("Name") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            if (ui.isSmart && ui.draftRules != null) {
                Column(
                    Modifier
                        .fillMaxWidth()
                        .clip(MaterialTheme.shapes.medium)
                        .background(p.c900)
                        .border(1.dp, p.edgeStrong, MaterialTheme.shapes.medium)
                        .padding(10.dp),
                ) {
                    SmartListBuilder(
                        value = ui.draftRules!!,
                        fields = ui.fields,
                        arenaMedia = "game",
                        onChange = vm::setDraftRules,
                    )
                }
            }
            ErrorText(ui.actionError)
            OutlinedButton(onClick = { confirmDelete = true }, modifier = Modifier.fillMaxWidth()) {
                Icon(Icons.Filled.DeleteOutline, contentDescription = null, tint = Tones.Dropped, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(6.dp))
                Text("Delete list", color = Tones.Dropped)
            }
            Row(Modifier.fillMaxWidth().padding(bottom = 24.dp), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onDismiss) { Text("Cancel") }
                Spacer(Modifier.width(8.dp))
                Button(onClick = vm::saveEdit, enabled = ui.draftName.isNotBlank() && !ui.busy) {
                    if (ui.busy) CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    else Text("Save")
                }
            }
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("Delete \"${ui.list?.name ?: ""}\"?") },
            text = { Text("The list goes away, but everything in it stays in your library.") },
            confirmButton = {
                TextButton(onClick = { confirmDelete = false; vm.delete() }) { Text("Delete list", color = Tones.Dropped) }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text("Cancel") } },
        )
    }
}

/** Pick games to add: the library minus current members and wishlisted games. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun AddEntriesSheet(vm: ListDetailViewModel, baseUrl: String, onOpenGame: (String) -> Unit) {
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette
    val selected = remember { mutableStateOf(setOf<String>()) }
    var query by rememberSaveable { mutableStateOf("") }

    ModalBottomSheet(
        onDismissRequest = vm::closeAdd,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = p.c950,
    ) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp)) {
            Text("Add games", style = MaterialTheme.typography.titleLarge, color = p.c100)
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                placeholder = { Text("Filter by title…") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))

            val visible = ui.candidates.filter { query.isBlank() || it.title.contains(query, ignoreCase = true) }
            LazyColumn(
                modifier = Modifier.fillMaxWidth().height(420.dp),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                items(visible, key = { it.id }) { entry ->
                    val checked = entry.id in selected.value
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clip(MaterialTheme.shapes.small)
                            .background(if (checked) p.fillActive else Color.Transparent)
                            .clickable {
                                selected.value =
                                    if (checked) selected.value - entry.id
                                    else selected.value + entry.id
                            }
                            .padding(vertical = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Checkbox(checked = checked, onCheckedChange = null)
                        CoverImage(entry, baseUrl, Modifier.size(width = 32.dp, height = 42.dp).clip(MaterialTheme.shapes.extraSmall))
                        Column {
                            Text(entry.title, style = MaterialTheme.typography.bodyMedium, color = p.c100, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(entry.status.gameLabel, style = MaterialTheme.typography.labelSmall, color = p.c500)
                        }
                    }
                }
            }

            Row(Modifier.fillMaxWidth().padding(vertical = 16.dp), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = vm::closeAdd) { Text("Cancel") }
                Spacer(Modifier.width(8.dp))
                Button(onClick = { vm.addEntries(selected.value.toList()) }, enabled = selected.value.isNotEmpty()) {
                    Text("Add ${selected.value.size}")
                }
            }
        }
    }
}
