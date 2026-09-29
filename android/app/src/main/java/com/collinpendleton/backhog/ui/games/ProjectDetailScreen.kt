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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DeleteOutline
import androidx.compose.material.icons.filled.DragIndicator
import androidx.compose.material.icons.filled.RadioButtonUnchecked
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
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
import com.collinpendleton.backhog.api.ProjectItem
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/**
 * One project — the web's ProjectDetailPage: the progress panel, checklist
 * rows with their drag reorder and done overrides, or the rule goal's match
 * pool, or the count goal's explainer.
 */
@Composable
fun ProjectDetailScreen(
    container: AppContainer,
    baseUrl: String,
    projectId: String,
    onBack: () -> Unit,
    onRemoved: () -> Unit,
    onOpenGame: (String) -> Unit,
) {
    val vm: ProjectDetailViewModel = viewModel(key = "project|$baseUrl|$projectId") {
        ProjectDetailViewModel(container.session, baseUrl, projectId)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette
    var confirmDelete by remember { mutableStateOf(false) }
    var addOpen by rememberSaveable { mutableStateOf(false) }

    // Navigating is a side effect: from composition it fires on every
    // recomposition until the screen leaves, popping more than this page.
    LaunchedEffect(ui.deleted) { if (ui.deleted) onRemoved() }
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
                    ui.project?.name ?: "Project",
                    style = MaterialTheme.typography.headlineSmall,
                    color = p.c100,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val kind = ui.project?.let { kindLabelOf(it.kind) } ?: ""
                val description = ui.project?.description?.takeIf { it.isNotBlank() }
                Text(
                    listOfNotNull(kind, description).joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall,
                    color = p.c400,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            IconButton(onClick = { vm.toggleComplete() }, enabled = !ui.loading) {
                Icon(
                    Icons.Filled.CheckCircle,
                    contentDescription = if (ui.complete) "Reopen" else "Mark done",
                    tint = if (ui.complete) Tones.Played else p.c300,
                )
            }
            IconButton(onClick = { confirmDelete = true }) {
                Icon(Icons.Filled.DeleteOutline, contentDescription = "Delete project", tint = Tones.Dropped)
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
                Text("That project doesn't exist.", color = p.c300)
                TextButton(onClick = onBack) { Text("Back to projects") }
            }
            else -> {
                val project = ui.project ?: return
                ProgressPanel(project)

                when {
                    ui.isChecklist -> ChecklistSection(ui, vm, baseUrl, onOpenGame) {
                        addOpen = true
                    }
                    ui.isRuleGoal -> RuleGoalSection(ui, baseUrl, onOpenGame)
                    else -> CountGoalExplainer()
                }
            }
        }
    }

    if (addOpen) {
        AddProjectItemsSheet(vm, baseUrl, onDone = { addOpen = false })
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("Delete \"${ui.project?.name ?: ""}\"?") },
            text = { Text("The project goes away, but everything in it stays in your library.") },
            confirmButton = {
                TextButton(onClick = { confirmDelete = false; vm.delete() }) { Text("Delete project", color = Tones.Dropped) }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text("Cancel") } },
        )
    }
}

private fun kindLabelOf(kind: String): String = when (kind) {
    "checklist" -> "Checklist"
    "count_goal" -> "Count goal"
    "rule_goal" -> "Rule goal"
    else -> kind
}

@Composable
private fun ProgressPanel(project: com.collinpendleton.backhog.api.Project) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    Column(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, shape)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.Bottom) {
            Text(
                buildString {
                    append(project.progress.completedCount)
                    append(" of ")
                    append(project.progress.targetCount)
                    append(" finished")
                },
                style = MaterialTheme.typography.bodyMedium,
                color = p.c300,
                modifier = Modifier.weight(1f),
            )
            Text(
                "${Math.round(project.progress.percent)}%",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.SemiBold,
                color = p.c100,
            )
        }
        if (project.progress.estHoursTotal > 0) {
            Text(
                "${Format.hours(project.progress.estHoursDone)} of ${Format.hours(project.progress.estHoursTotal)} estimated",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
        LinearProgressIndicator(
            progress = { (project.progress.percent.toFloat() / 100f).coerceIn(0f, 1f) },
            modifier = Modifier.fillMaxWidth().height(6.dp).clip(CircleShape),
            color = if (project.completedAt != null) Tones.Played else p.hlMid,
            trackColor = p.c800,
        )
        Text(
            if (project.completedAt != null) "Target met or closed · ${Format.date(project.completedAt)}"
            else if (project.progress.estHoursRemaining > 0) "${Format.hours(project.progress.estHoursRemaining)} of estimated playtime to go"
            else "No estimated hours left in this set",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )
    }
}

@Composable
private fun ChecklistSection(
    ui: ProjectDetailUiState,
    vm: ProjectDetailViewModel,
    baseUrl: String,
    onOpenGame: (String) -> Unit,
    onAdd: () -> Unit,
) {
    if (ui.items.isEmpty()) {
        Column(
            Modifier.fillMaxWidth().padding(vertical = 32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text("Nothing in this project yet", style = MaterialTheme.typography.titleMedium, color = Backhog.palette.c100)
            Text(
                "Open a game and check it into this project to start building the list.",
                style = MaterialTheme.typography.bodyMedium,
                color = Backhog.palette.c400,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
            Button(onClick = onAdd) { Text("Add games") }
        }
        return
    }

    val listState = rememberLazyListState()
    val drag = remember {
        QueueDragState(
            listState = listState,
            onMoveLocal = { from, to -> vm.moveLocal(from, to) },
            onCommit = { id -> vm.commit(id) },
        )
    }

    Column {
        Row(
            Modifier.fillMaxWidth().padding(vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "Members",
                style = MaterialTheme.typography.titleSmall,
                color = Backhog.palette.c200,
                modifier = Modifier.weight(1f),
            )
            TextButton(onClick = onAdd) {
                Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(14.dp))
                Text("Add")
            }
        }
        LazyColumn(state = listState, verticalArrangement = Arrangement.spacedBy(6.dp)) {
            items(ui.items, key = { it.entry.id }) { item ->
                val dragging = drag.draggingKey == item.entry.id
                ChecklistRow(item, dragging, drag.dragOffset, ui.toggling, drag, vm, baseUrl, onOpenGame)
            }
            item {
                Text(
                    "Drag the handle to reorder · tap the circle to override an item's done state",
                    style = MaterialTheme.typography.labelSmall,
                    color = Backhog.palette.c600,
                    modifier = Modifier.padding(vertical = 8.dp),
                )
            }
            item { Spacer(Modifier.height(20.dp)) }
        }
    }
}

@Composable
private fun ChecklistRow(
    item: ProjectItem,
    dragging: Boolean,
    dragOffset: Float,
    toggling: String?,
    drag: QueueDragState,
    vm: ProjectDetailViewModel,
    baseUrl: String,
    onOpenGame: (String) -> Unit,
) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.medium
    val done = item.done ?: (item.entry.status == com.collinpendleton.backhog.api.EntryStatus.Played)

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
            .padding(horizontal = 4.dp, vertical = 8.dp)
            .let { m -> if (done) m.alpha(0.7f) else m },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(
            Icons.Filled.DragIndicator,
            contentDescription = "Reorder ${item.entry.title}",
            tint = p.c600,
            modifier = Modifier
                .pointerInput(Unit) {
                    detectDragGesturesAfterLongPress(
                        onDragStart = { drag.start(item.entry.id) },
                        onDrag = { change, amount ->
                            change.consume()
                            drag.drag(amount.y)
                        },
                        onDragEnd = { drag.end() },
                        onDragCancel = { drag.end() },
                    )
                }
                .size(26.dp),
        )
        if (toggling == item.entry.id) {
            CircularProgressIndicator(modifier = Modifier.size(24.dp), strokeWidth = 2.dp)
        } else {
            IconButton(onClick = { vm.setDone(item.entry.id, !done) }, modifier = Modifier.size(32.dp)) {
                Icon(
                    if (done) Icons.Filled.CheckCircle else Icons.Filled.RadioButtonUnchecked,
                    contentDescription = if (done) "Mark not done" else "Mark done",
                    tint = if (done) Tones.Played else p.c500,
                    modifier = Modifier.size(22.dp),
                )
            }
        }
        Column(
            Modifier
                .weight(1f)
                .clickable { onOpenGame(item.entry.id) },
        ) {
            Text(
                item.entry.title,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Medium,
                color = p.c100,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                if (item.done == null) "done follows status" else "done overridden",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
        IconButton(onClick = { vm.removeItem(item.entry.id) }, modifier = Modifier.size(30.dp)) {
            Icon(
                Icons.Filled.Close,
                contentDescription = "Remove ${item.entry.title}",
                tint = p.c600,
                modifier = Modifier.size(15.dp),
            )
        }
    }
}

/** The rule goal's current match pool — finishing any of these counts. */
@Composable
private fun RuleGoalSection(ui: ProjectDetailUiState, baseUrl: String, onOpenGame: (String) -> Unit) {
    val p = Backhog.palette
    Column {
        Text(
            "The current match pool — finishing any of these counts toward the goal.",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
            modifier = Modifier.padding(vertical = 6.dp),
        )
        LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            items(ui.items, key = { it.entry.id }) { item ->
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clip(MaterialTheme.shapes.medium)
                        .background(p.c900)
                        .border(1.dp, p.edgeStrong, MaterialTheme.shapes.medium)
                        .clickable { onOpenGame(item.entry.id) }
                        .padding(horizontal = 12.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    CoverImage(item.entry, baseUrl, Modifier.size(width = 36.dp, height = 48.dp).clip(MaterialTheme.shapes.small))
                    Column(Modifier.weight(1f)) {
                        Text(
                            item.entry.title,
                            style = MaterialTheme.typography.bodyMedium,
                            color = p.c100,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        item.entry.game?.timeToBeatMain?.let {
                            Text(Format.duration(it), style = MaterialTheme.typography.labelSmall, color = p.c500)
                        }
                    }
                }
            }
            item { Spacer(Modifier.height(20.dp)) }
        }
    }
}

@Composable
private fun CountGoalExplainer() {
    val p = Backhog.palette
    Column(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.large)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, MaterialTheme.shapes.large)
            .padding(16.dp),
    ) {
        Text(
            "Every game you finish counts toward this goal — no membership to manage. Progress updates the moment a game's status becomes played.",
            style = MaterialTheme.typography.bodyMedium,
            color = p.c300,
        )
    }
}

/** Pick games into the checklist — the same picker pattern the list uses. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun AddProjectItemsSheet(vm: ProjectDetailViewModel, baseUrl: String, onDone: () -> Unit) {
    var candidates by remember { mutableStateOf<List<com.collinpendleton.backhog.api.Entry>>(emptyList()) }
    var loaded by remember { mutableStateOf(false) }
    val selected = remember { mutableStateOf(setOf<String>()) }
    var query by rememberSaveable { mutableStateOf("") }
    val p = Backhog.palette

    ModalBottomSheet(onDismissRequest = onDone, containerColor = p.c950) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp)) {
            Text("Add games", style = MaterialTheme.typography.titleLarge, color = p.c100)
            if (!loaded) {
                LaunchedEffect(Unit) { vm.loadCandidates { list -> candidates = list; loaded = true } }
                Box(Modifier.fillMaxWidth().height(200.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
            } else {
                Spacer(Modifier.height(12.dp))
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it },
                    placeholder = { Text("Filter by title…") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                val visible = candidates.filter { query.isBlank() || it.title.contains(query, ignoreCase = true) }
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
                                .background(if (checked) p.fillActive else androidx.compose.ui.graphics.Color.Transparent)
                                .clickable {
                                    selected.value =
                                        if (checked) selected.value - entry.id
                                        else selected.value + entry.id
                                }
                                .padding(vertical = 2.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            androidx.compose.material3.Checkbox(checked = checked, onCheckedChange = null)
                            CoverImage(entry, baseUrl, Modifier.size(width = 32.dp, height = 42.dp).clip(MaterialTheme.shapes.extraSmall))
                            Text(entry.title, style = MaterialTheme.typography.bodyMedium, color = p.c100, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
                Row(Modifier.fillMaxWidth().padding(vertical = 16.dp), horizontalArrangement = Arrangement.End) {
                    TextButton(onClick = onDone) { Text("Cancel") }
                    Spacer(Modifier.width(8.dp))
                    OutlinedButton(
                        onClick = { vm.addItems(selected.value.toList()); onDone() },
                        enabled = selected.value.isNotEmpty(),
                    ) { Text("Add ${selected.value.size}") }
                }
            }
        }
    }
}

