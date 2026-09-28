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
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.DragIndicator
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.VideogameAsset
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
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
import com.collinpendleton.backhog.api.SeriesMember
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/** The rating floor for the "just the good ones" view. */
private const val GOOD_ONES_FLOOR = 75

/** The web's PLAY_ORDERS — value/label pairs in display order. */
private val PLAY_ORDERS = listOf(
    "release" to "Release order",
    "chronological" to "Chronological (DLC with its game)",
    "recommended" to "Recommended (best first)",
    "custom" to "Custom (drag)",
    "good_ones" to "Just the good ones (IGDB ≥ 75)",
)

/**
 * One series as a journey — the web's SeriesDetailPage: the ordered member
 * list in the user's chosen play order, with DLC nested by their parent game.
 */
@Composable
fun SeriesDetailScreen(
    container: AppContainer,
    baseUrl: String,
    seriesId: String,
    onBack: () -> Unit,
    onOpenGame: (String) -> Unit,
) {
    val vm: SeriesDetailViewModel = viewModel(key = "series|$seriesId|$baseUrl") {
        SeriesDetailViewModel(container.session, baseUrl, seriesId)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette

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
                    ui.detail?.name ?: "Series",
                    style = MaterialTheme.typography.headlineSmall,
                    color = p.c100,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val d = ui.detail
                Text(
                    if (d == null) "" else buildString {
                        append("${d.playedCount}/${d.ownedCount} played · ${Math.round(d.completion)}% complete")
                        if (d.remainingHours > 0) append(" · ${Format.hours(d.remainingHours)} left")
                        if (d.dlcHours > 0) append(" · ${Format.hours(d.dlcHours)} of it DLC")
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = p.c400,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            PlayOrderMenu(ui.detail?.playOrder ?: "release", vm::setPlayOrder)
        }

        ErrorText(ui.actionError)

        when {
            ui.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            ui.error != null -> Column(
                Modifier.fillMaxSize(),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterVertically),
            ) {
                Text("That series doesn't exist.", color = p.c300)
                TextButton(onClick = onBack) { Text("Back to series") }
            }
            else -> {
                val detail = ui.detail ?: return
                val isCustom = detail.playOrder == "custom"
                val isGoodOnes = detail.playOrder == "good_ones"
                val members = detail.members
                val hidden = if (isGoodOnes && !ui.showAll) {
                    members.filter { (it.game.igdbRating ?: 0.0) < GOOD_ONES_FLOOR }
                } else emptyList()
                val visible = if (isGoodOnes && !ui.showAll) {
                    members.filter { (it.game.igdbRating ?: 0.0) >= GOOD_ONES_FLOOR }
                } else members

                if (isGoodOnes) {
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clip(MaterialTheme.shapes.small)
                            .background(p.c850)
                            .padding(horizontal = 12.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            "Showing the good ones — members rated $GOOD_ONES_FLOOR+ on IGDB.",
                            style = MaterialTheme.typography.labelMedium,
                            color = p.c400,
                            modifier = Modifier.weight(1f),
                        )
                        if (hidden.isNotEmpty()) {
                            Text(
                                "Show all ${members.size}",
                                style = MaterialTheme.typography.labelMedium,
                                color = p.hlBright,
                                modifier = Modifier.clickable { vm.setShowAll(true) }.padding(4.dp),
                            )
                        }
                    }
                }

                if (isCustom) {
                    CustomJourney(ui, vm, visible, baseUrl, onOpenGame)
                } else {
                    LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        items(visible, key = { it.game.id }) { member ->
                            val index = visible.indexOfFirst { it.game.id == member.game.id }
                            MemberRow(member, index, baseUrl, onOpenGame)
                        }
                        if (ui.showAll && hidden.isNotEmpty()) {
                            item {
                                Text(
                                    "Below the $GOOD_ONES_FLOOR floor:",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = p.c500,
                                    modifier = Modifier.padding(top = 8.dp),
                                )
                            }
                            items(hidden, key = { it.game.id }) { member ->
                                val index = hidden.indexOfFirst { it.game.id == member.game.id }
                                MemberRow(member, index, baseUrl, onOpenGame, dimmed = true)
                            }
                        }
                        item { Spacer(Modifier.height(20.dp)) }
                    }
                }
            }
        }
    }
}

@Composable
private fun PlayOrderMenu(selected: String, onPick: (String) -> Unit) {
    val p = Backhog.palette
    var open by remember { mutableStateOf(false) }
    Box {
        Row(
            Modifier
                .clip(MaterialTheme.shapes.small)
                .border(1.dp, p.edgeStrong, MaterialTheme.shapes.small)
                .clickable { open = true }
                .padding(horizontal = 10.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                PLAY_ORDERS.firstOrNull { it.first == selected }?.second ?: selected,
                style = MaterialTheme.typography.labelMedium,
                color = p.c200,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Icon(Icons.Filled.KeyboardArrowDown, contentDescription = "Play order", tint = p.c500)
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            PLAY_ORDERS.forEach { (value, label) ->
                DropdownMenuItem(
                    text = { Text(label, style = MaterialTheme.typography.bodySmall) },
                    onClick = {
                        open = false
                        onPick(value)
                    },
                )
            }
        }
    }
}

/** The custom journey: drag to reorder, persisted with fractional positions. */
@Composable
private fun CustomJourney(
    ui: SeriesDetailUiState,
    vm: SeriesDetailViewModel,
    members: List<SeriesMember>,
    baseUrl: String,
    onOpenGame: (String) -> Unit,
) {
    val listState = rememberLazyListState()
    val dragState = remember {
        DragAdapter(
            listState = listState,
            onMoveLocal = { from, to -> vm.moveLocal(from, to) },
            onCommit = { id -> vm.commit(id) },
        )
    }

    LazyColumn(state = listState, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        items(members, key = { it.game.id }) { member ->
            val index = members.indexOfFirst { it.game.id == member.game.id }
            val dragging = dragState.draggingKey == member.game.id
            SortableMemberRow(member, index, dragging, dragState.dragOffset, dragState, baseUrl, onOpenGame)
        }
        item { Spacer(Modifier.height(20.dp)) }
    }
}

/** Members keyed by game id — the queue's drag math with a different key. */
internal class DragAdapter(
    private val listState: LazyListState,
    private val onMoveLocal: (Int, Int) -> Unit,
    private val onCommit: (Long) -> Unit,
) {
    var draggingKey: Long? by mutableStateOf(null)
    var dragOffset: Float by mutableFloatStateOf(0f)

    fun start(key: Long) {
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

@Composable
private fun SortableMemberRow(
    member: SeriesMember,
    index: Int,
    dragging: Boolean,
    dragOffset: Float,
    dragState: DragAdapter,
    baseUrl: String,
    onOpenGame: (String) -> Unit,
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
        Icon(
            Icons.Filled.DragIndicator,
            contentDescription = "Reorder ${member.game.name}",
            tint = p.c600,
            modifier = Modifier
                .pointerInput(Unit) {
                    detectDragGesturesAfterLongPress(
                        onDragStart = { dragState.start(member.game.id) },
                        onDrag = { change, amount ->
                            change.consume()
                            dragState.drag(amount.y)
                        },
                        onDragEnd = { dragState.end() },
                        onDragCancel = { dragState.end() },
                    )
                }
                .size(26.dp),
        )
        MemberRowContent(member, index, baseUrl, onOpenGame)
    }
}

/** One member of the journey: cover, name, kind, hours, and library status. */
@Composable
private fun MemberRow(
    member: SeriesMember,
    index: Int,
    baseUrl: String,
    onOpenGame: (String) -> Unit,
    dimmed: Boolean = false,
) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.medium
    Row(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, shape)
            .let { m -> if (dimmed || !member.owned) m.alpha(0.65f) else m }
            .let { m ->
                if (member.owned && member.entryId != null) {
                    m.clickable { onOpenGame(member.entryId!!) }
                } else m
            }
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        MemberRowContent(member, index, baseUrl, onOpenGame, showNumber = false)
    }
}

/** One member's row content; a RowScope so the inner column can take weight. */
@Composable
private fun androidx.compose.foundation.layout.RowScope.MemberRowContent(
    member: SeriesMember,
    index: Int,
    baseUrl: String,
    onOpenGame: (String) -> Unit,
    showNumber: Boolean = true,
) {
    val p = Backhog.palette
    val game = member.game
    if (showNumber) {
        Text(
            "${index + 1}",
            style = MaterialTheme.typography.labelMedium,
            color = p.c500,
            modifier = Modifier.width(18.dp),
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
    }
    val entry = member.entryId?.let { id ->
        com.collinpendleton.backhog.api.Entry(
            id = id,
            mediaType = "game",
            game = game,
            createdAt = "",
            updatedAt = "",
        )
    }
    if (entry != null) {
        CoverImage(entry, baseUrl, Modifier.size(width = 40.dp, height = 52.dp).clip(MaterialTheme.shapes.small))
    } else {
        Box(
            Modifier
                .size(width = 40.dp, height = 52.dp)
                .clip(MaterialTheme.shapes.small)
                .background(p.c850),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                Icons.Filled.VideogameAsset,
                contentDescription = null,
                tint = p.c600,
                modifier = Modifier.size(18.dp),
            )
        }
    }
    Column(Modifier.weight(1f)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(
                game.name,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Medium,
                color = p.c100,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false),
            )
            if (member.kind != "game") {
                Text(
                    member.kind,
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c400,
                    modifier = Modifier
                        .clip(MaterialTheme.shapes.extraSmall)
                        .background(p.fillActive)
                        .padding(horizontal = 6.dp, vertical = 1.dp),
                )
            }
        }
        val meta = buildList {
            Format.releaseYear(game.firstReleaseDate).takeIf { it.isNotEmpty() }?.let { add(it) }
            game.timeToBeatMain?.let { add(Format.duration(it)) }
            game.igdbRating?.let { add("★ ${Math.round(it)}") }
        }.joinToString(" · ")
        if (meta.isNotEmpty()) {
            Text(meta, style = MaterialTheme.typography.labelSmall, color = p.c500, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
    if (member.owned) {
        ToneChip(statusLabelOf(member.status), toneForSeriesStatus(member.status))
    } else {
        Text("Not owned", style = MaterialTheme.typography.labelSmall, color = p.c600)
    }
}

private fun statusLabelOf(status: String): String =
    com.collinpendleton.backhog.api.EntryStatus.fromKey(status)?.gameLabel ?: status

private fun toneForSeriesStatus(status: String): androidx.compose.ui.graphics.Color =
    com.collinpendleton.backhog.api.EntryStatus.fromKey(status)?.let { Tones.forStatus(it) } ?: Tones.Backlog
