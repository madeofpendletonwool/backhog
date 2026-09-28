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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.KeyboardDoubleArrowDown
import androidx.compose.material.icons.filled.KeyboardDoubleArrowUp
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.DragIndicator
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/**
 * The drag gesture's working state, hoisted out of composition so the pointer
 * callbacks always read fresh data: the captured list inside a pointerInput
 * block goes stale the moment an optimistic reorder recomposes the list.
 */
internal class QueueDragState(
    private val listState: LazyListState,
    private val onMoveLocal: (Int, Int) -> Unit,
    private val onCommit: (String) -> Unit,
) {
    var draggingKey: String? by mutableStateOf(null)
    var dragOffset: Float by mutableFloatStateOf(0f)

    fun start(key: String) {
        draggingKey = key
        dragOffset = 0f
    }

    fun drag(deltaY: Float) {
        val key = draggingKey ?: return
        dragOffset += deltaY
        val visible = listState.layoutInfo.visibleItemsInfo
        val info = visible.firstOrNull { it.key == key } ?: return
        val naturalCenter = info.offset + info.size / 2f
        val draggedCenter = naturalCenter + dragOffset
        // Swap when the dragged centre passes a neighbour's centre; the offset
        // rebases so the row stays under the finger in its new slot.
        val next = visible.firstOrNull { it.index == info.index + 1 }
        val prev = visible.firstOrNull { it.index == info.index - 1 }
        if (next != null && draggedCenter > next.offset + next.size / 2f) {
            onMoveLocal(info.index, next.index)
            dragOffset = draggedCenter - (next.offset + next.size / 2f)
        } else if (prev != null && draggedCenter < prev.offset + prev.size / 2f) {
            onMoveLocal(info.index, prev.index)
            dragOffset = draggedCenter - (prev.offset + prev.size / 2f)
        }
    }

    fun end() {
        val key = draggingKey
        draggingKey = null
        dragOffset = 0f
        if (key != null) onCommit(key)
    }
}

/**
 * The play queue: the backlog in the order you plan to play it. Drag the
 * handle (long-press, then move) or tap the quick-move buttons; Start leaves
 * the queue for "playing", Done marks it finished — the web's next_up path.
 */
@Composable
fun QueueScreen(
    container: AppContainer,
    baseUrl: String,
    onOpenGame: (String) -> Unit,
    onBack: () -> Unit,
) {
    val vm: QueueViewModel = viewModel(key = "queue|$baseUrl") {
        QueueViewModel(container.session, baseUrl, container.unlocks)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette
    val listState = rememberLazyListState()
    val drag = remember {
        QueueDragState(
            listState = listState,
            onMoveLocal = { from, to -> vm.moveLocal(from, to) },
            onCommit = { id -> vm.commit(id) },
        )
    }

    Column(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
        Row(
            Modifier.fillMaxWidth().padding(top = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text("Play Queue", style = MaterialTheme.typography.headlineSmall, color = p.c100)
                val count = ui.entries.size
                val noun = if (ui.entries.any { it.isBook }) "item" else "game"
                val sub = when {
                    ui.loading && ui.entries.isEmpty() -> "Loading…"
                    count == 0 -> "The order you plan to play things in."
                    else -> buildString {
                        append("$count ${noun}${if (count == 1) "" else "s"}")
                        if (ui.totalHours > 0) append(" · ${Format.hours(ui.totalHours)} deep")
                        append(" · drag to reorder")
                    }
                }
                Text(sub, style = MaterialTheme.typography.bodySmall, color = p.c400)
            }
        }

        if (ui.moveError) {
            Row(
                Modifier
                    .fillMaxWidth()
                    .padding(vertical = 6.dp)
                    .clip(MaterialTheme.shapes.small)
                    .background(Tones.Dropped.copy(alpha = 0.12f))
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "Couldn't save that move — the queue has been restored.",
                    style = MaterialTheme.typography.bodySmall,
                    color = Tones.Dropped,
                    modifier = Modifier.weight(1f),
                )
                Text("OK", color = Tones.Dropped, style = MaterialTheme.typography.labelLarge,
                    modifier = Modifier.clickable(onClick = vm::dismissMoveError).padding(4.dp))
            }
        }

        if (ui.error != null && ui.entries.isEmpty()) {
            val message = ui.error
            Column(
                Modifier.fillMaxSize(),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterVertically),
            ) {
                Text(message ?: "", color = Tones.Dropped, style = MaterialTheme.typography.bodyMedium)
                Button(onClick = vm::load) {
                    Icon(Icons.Filled.Refresh, contentDescription = null, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("Try again")
                }
            }
        } else if (ui.loading && ui.entries.isEmpty()) {
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        } else if (ui.entries.isEmpty()) {
            Column(
                Modifier.fillMaxSize().padding(vertical = 48.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("Nothing queued up", style = MaterialTheme.typography.titleMedium, color = p.c100)
                Text(
                    "Games in your backlog appear here. Marking one as playing or played takes it out of the queue.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = p.c400,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
            }
        } else {
            LazyColumn(state = listState, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                items(ui.entries, key = { it.id }) { entry ->
                    val index = ui.entries.indexOfFirst { it.id == entry.id }
                    val dragging = drag.draggingKey == entry.id
                    val marking = ui.marking == entry.id
                    QueueRow(
                        entry = entry,
                        baseUrl = baseUrl,
                        position = index + 1,
                        cumulativeHours = ui.cumulativeHours(index),
                        isFirst = index == 0,
                        isLast = index == ui.entries.lastIndex,
                        dragging = dragging,
                        dragOffset = drag.dragOffset,
                        marking = marking,
                        onDragStart = { drag.start(entry.id) },
                        onDrag = drag::drag,
                        onDragEnd = drag::end,
                        onMove = { kind ->
                            vm.move(index, QueueMoves.targetIndex(index, ui.entries.size, kind), kind)
                        },
                        onStart = { vm.markPlaying(entry.id) },
                        onDone = { vm.markFinished(entry.id) },
                        onOpen = { onOpenGame(entry.id) },
                    )
                }
                item { Spacer(Modifier.height(20.dp)) }
            }
        }
    }
}

@Composable
private fun QueueRow(
    entry: Entry,
    baseUrl: String,
    position: Int,
    cumulativeHours: Double,
    isFirst: Boolean,
    isLast: Boolean,
    dragging: Boolean,
    dragOffset: Float,
    marking: Boolean,
    onDragStart: () -> Unit,
    onDrag: (Float) -> Unit,
    onDragEnd: () -> Unit,
    onMove: (QueueMoves.Kind) -> Unit,
    onStart: () -> Unit,
    onDone: () -> Unit,
    onOpen: () -> Unit,
) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.medium

    Column(
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
            .padding(horizontal = 6.dp, vertical = 8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
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
                    .size(28.dp),
            )
            Text(
                "$position",
                style = MaterialTheme.typography.labelMedium,
                color = p.c500,
                modifier = Modifier.width(18.dp),
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
            CoverImage(entry, baseUrl, Modifier.size(width = 40.dp, height = 52.dp).clip(MaterialTheme.shapes.small))
            Column(
                Modifier
                    .weight(1f)
                    .clickable(onClick = onOpen),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        entry.title,
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.Medium,
                        color = p.c100,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    if (cumulativeHours > 0) {
                        Text(
                            Format.hours(cumulativeHours),
                            style = MaterialTheme.typography.labelSmall,
                            color = p.c500,
                        )
                    }
                }
                val meta = buildList {
                    if (entry.isBook) {
                        entry.book?.authors?.firstOrNull()?.let { add(it) }
                        entry.book?.firstPublishYear?.let { add(it.toString()) }
                    } else {
                        Format.releaseYear(entry.game?.firstReleaseDate).takeIf { it.isNotEmpty() }?.let { add(it) }
                        entry.game?.timeToBeatMain?.let { add(Format.duration(it)) }
                        entry.game?.igdbRating?.let { add("★ ${Math.round(it)}") }
                    }
                }.joinToString(" · ")
                if (meta.isNotEmpty()) {
                    Text(meta, style = MaterialTheme.typography.labelSmall, color = p.c500, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }

        Row(
            Modifier.fillMaxWidth().padding(start = 46.dp, top = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            MoveIcon("Move ${entry.title} to top", isFirst, Icons.Filled.KeyboardDoubleArrowUp) { onMove(QueueMoves.Kind.Top) }
            MoveIcon("Move ${entry.title} up", isFirst, Icons.Filled.KeyboardArrowUp) { onMove(QueueMoves.Kind.Up) }
            MoveIcon("Move ${entry.title} down", isLast, Icons.Filled.KeyboardArrowDown) { onMove(QueueMoves.Kind.Down) }
            MoveIcon("Move ${entry.title} to bottom", isLast, Icons.Filled.KeyboardDoubleArrowDown) { onMove(QueueMoves.Kind.Bottom) }
            Spacer(Modifier.weight(1f))
            if (marking) {
                CircularProgressIndicator(modifier = Modifier.size(18.dp).padding(2.dp), strokeWidth = 2.dp)
            } else {
                IconButton(onClick = onStart, modifier = Modifier.size(32.dp)) {
                    Icon(Icons.Filled.PlayArrow, contentDescription = "Mark ${entry.title} as playing", tint = Tones.Playing, modifier = Modifier.size(18.dp))
                }
                IconButton(onClick = onDone, modifier = Modifier.size(32.dp)) {
                    Icon(Icons.Filled.CheckCircle, contentDescription = "Mark ${entry.title} as played", tint = Tones.Played, modifier = Modifier.size(18.dp))
                }
            }
        }
    }
}

@Composable
private fun MoveIcon(
    label: String,
    disabled: Boolean,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    onClick: () -> Unit,
) {
    val p = Backhog.palette
    IconButton(onClick = onClick, enabled = !disabled, modifier = Modifier.size(32.dp)) {
        Icon(
            icon,
            contentDescription = label,
            tint = if (disabled) p.c800 else p.c500,
            modifier = Modifier.size(18.dp),
        )
    }
}
