package com.collinpendleton.backhog.ui.books

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.books.byline
import com.collinpendleton.backhog.books.chapterTitle
import com.collinpendleton.backhog.books.describeSource
import com.collinpendleton.backhog.books.editionLabel
import com.collinpendleton.backhog.books.explainPage
import com.collinpendleton.backhog.books.formatPage
import com.collinpendleton.backhog.books.formatTimecode
import com.collinpendleton.backhog.books.pageCountFor
import com.collinpendleton.backhog.books.positionHeadline
import com.collinpendleton.backhog.books.publishYear
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.Field
import com.collinpendleton.backhog.ui.components.Panel
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import java.time.LocalDate
import java.time.format.DateTimeFormatter

/**
 * One book, whole: the dossier, where you are in it, the printings in hand,
 * what is attached, and who it is shared with. The reader and the player
 * arrive from here in Stages 5–6; until then the position panel and
 * search-in-book are the whole of the book's inner life.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BookDetailScreen(
    container: AppContainer,
    baseUrl: String,
    entryId: String,
    onBack: () -> Unit,
    onOpen: (String) -> Unit,
    canManageMedia: Boolean = false,
    onRead: (String) -> Unit = {},
    onReadJump: (entryId: String, offset: Long) -> Unit = { _, _ -> },
    onOpenFiles: () -> Unit = {},
) {
    val vm: BookDetailViewModel = viewModel(factory = BookDetailViewModel.Factory(container, baseUrl, entryId))
    val state by vm.state.collectAsState()
    val p = Backhog.palette

    var showSessions by remember { mutableStateOf(false) }
    var showRegister by remember { mutableStateOf(false) }
    var showScan by remember { mutableStateOf(false) }
    var showSearch by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    var toast by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(state.deleted) { if (state.deleted) onBack() }
    LaunchedEffect(state.unlocks) {
        state.unlocks.firstOrNull()?.let {
            toast = "Achievement unlocked: ${it.title}"
            vm.consumeUnlocks()
        }
    }
    LaunchedEffect(state.actionError) {
        state.actionError?.let {
            toast = it
            vm.clearActionError()
        }
    }
    LaunchedEffect(toast) {
        if (toast != null) {
            kotlinx.coroutines.delay(3500)
            toast = null
        }
    }

    Box(Modifier.fillMaxSize()) {
        Scaffold(
        containerColor = p.c950,
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        state.entry?.book?.title ?: "Book",
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
                actions = {
                    IconButton(onClick = { showSearch = true }) {
                        Icon(Icons.Filled.Search, contentDescription = "Search in this book")
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = p.c950,
                    titleContentColor = p.c100,
                    navigationIconContentColor = p.c300,
                    actionIconContentColor = p.c300,
                ),
            )
        },
    ) { padding ->
        when {
            state.loading -> Column(
                Modifier.fillMaxSize().padding(padding),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) { CircularProgressIndicator() }
            state.error != null -> Column(
                Modifier.fillMaxSize().padding(padding).padding(24.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(state.error!!, color = MaterialTheme.colorScheme.error)
                OutlinedButton(onClick = vm::reload) { Text("Try again") }
            }
            else -> Column(
                Modifier
                    .fillMaxSize()
                    .padding(padding)
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp)
                    .padding(bottom = 48.dp),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                Header(state, baseUrl)
                FactsPanel(state)
                ShelfPanel(state, vm)
                PositionPanel(
                    state,
                    onScan = { showScan = true },
                    onRead = { onRead(entryId) },
                    onListen = { container.player.open(entryId) },
                )
                SessionsPanel(state, onAdd = { showSessions = true })
                CopiesPanel(state, vm, onRegister = { showRegister = true })
                FilesPanel(state, vm, canManageMedia, onOpenFiles)
                SharePanel(state, vm)
                DangerPanel(vm, onConfirm = { confirmDelete = true })
            }
        }
    }

    toast?.let { message ->
        Panel(
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .padding(16.dp),
        ) { Text(message, color = p.c100) }
    }

    if (showSessions) AddSessionDialog(vm, onDismiss = { showSessions = false })
    if (showRegister) RegisterCopyDialog(state, vm, onDismiss = { showRegister = false })
    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("Remove from shelf?") },
            text = { Text("Your status, rating, notes, sessions and position go with it.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    vm.deleteEntry()
                }) { Text("Remove") }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text("Keep") } },
        )
    }
    if (showScan && state.drivingCopy != null) {
        ModalBottomSheet(onDismissRequest = { showScan = false }) {
            ScanPageSheet(
                vm = vm,
                copyId = state.drivingCopy!!.id,
                anchorCount = state.drivingCopy!!.anchorCount,
                onDone = { showScan = false },
            )
        }
    }
    if (showSearch) {
        ModalBottomSheet(onDismissRequest = { showSearch = false }) {
            SearchInBookSheet(
                container = container,
                baseUrl = baseUrl,
                entryId = entryId,
                entry = state.entry,
                onOpen = onOpen,
                onJump = { offset ->
                    showSearch = false
                    onReadJump(entryId, offset)
                },
            )
        }
    }
    }
}
/* ------------------------------------------------------------------ header */

@Composable
private fun Header(state: BookDetailState, baseUrl: String) {
    val p = Backhog.palette
    val book = state.book
    val brief = state.entry?.book
    if (book == null && brief == null) return
    val title = book?.title ?: brief!!.title
    val line = listOf(byline(book?.authors), publishYear(book?.firstPublishYear))
        .filter { it.isNotEmpty() }
        .joinToString(" · ")
    Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        BookCover(
            title,
            book?.coverUrl ?: brief!!.coverUrl,
            book?.accentHex ?: brief!!.accentHex,
            baseUrl,
            book?.id ?: brief!!.id,
            Modifier.width(96.dp).aspectRatio(2f / 3f),
        )
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(title, style = MaterialTheme.typography.titleLarge, color = p.cMax)
            if (line.isNotEmpty()) Text(line, style = MaterialTheme.typography.bodyMedium, color = p.c400)
            LenderBadge(state.entry?.sharedBy)
            state.entry?.let { entry ->
                ToneChip(entry.status.bookLabel, statusTone(entry.status))
                entry.loggedMinutes.takeIf { it > 0 }?.let { minutes ->
                    Text(
                        "Read for ${minutes / 60}h ${minutes % 60}m",
                        style = MaterialTheme.typography.labelSmall,
                        color = p.c500,
                    )
                }
            }
        }
    }
}

internal fun statusTone(status: EntryStatus) = when (status) {
    EntryStatus.Backlog -> Tones.Backlog
    EntryStatus.Playing -> Tones.Playing
    EntryStatus.Played -> Tones.Played
    EntryStatus.Dropped, EntryStatus.Ignored -> Tones.Dropped
    EntryStatus.Wishlist -> Tones.Silver
}

/* ------------------------------------------------------------------- facts */

@Composable
private fun FactsPanel(state: BookDetailState) {
    val p = Backhog.palette
    val book = state.book ?: return
    val subjects = book.subjects.orEmpty()
    val editions = book.editions.orEmpty()
    val pageCount = pageCountFor(editions, state.entry?.editionId)
    if (book.description.isBlank() && subjects.isEmpty() && pageCount == null) return

    Panel {
        SectionLabel("The work")
        if (book.description.isNotBlank()) {
            var expanded by remember { mutableStateOf(false) }
            Text(
                book.description,
                style = MaterialTheme.typography.bodyMedium,
                color = p.c300,
                maxLines = if (expanded) Int.MAX_VALUE else 6,
                overflow = TextOverflow.Ellipsis,
            )
            if (book.description.length > 400) {
                TextButton(onClick = { expanded = !expanded }) {
                    Text(if (expanded) "Show less" else "Read more")
                }
            }
        }
        if (pageCount != null) {
            Text(
                com.collinpendleton.backhog.books.formatPages(pageCount),
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
            )
        }
        if (subjects.isNotEmpty()) {
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                subjects.take(3).forEach { subject ->
                    ToneChip(subject.take(24), Tones.Silver)
                }
            }
        }
    }
}

/* -------------------------------------------------------------- your shelf */

@Composable
private fun ShelfPanel(state: BookDetailState, vm: BookDetailViewModel) {
    val entry = state.entry ?: return
    val p = Backhog.palette

    Panel {
        SectionLabel("On your shelf")
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            EntryStatus.entries.chunked(3).forEach { row ->
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        row.forEach { status ->
                            FilterChip(
                                selected = entry.status == status,
                                onClick = { if (entry.status != status) vm.setStatus(status) },
                                label = { Text(status.bookLabel, style = MaterialTheme.typography.labelSmall) },
                            )
                        }
                    }
                }
            }
        }

        RatingPicker(entry.userRating, vm::setRating)

        NotesEditor(entry.notes, vm::setNotes)

        entry.createdAt.take(10)?.let { FactRow("Added", it) }
        entry.startedAt?.take(10)?.let { FactRow("Started", it) }
        entry.finishedAt?.take(10)?.let { FactRow("Finished", it) }
    }
}

@Composable
private fun RatingPicker(current: Int?, set: (Int?) -> Unit) {
    val p = Backhog.palette
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(
            if (current != null) "Your rating: $current / 10" else "Not rated yet",
            style = MaterialTheme.typography.labelMedium,
            color = p.c400,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            (1..10).forEach { score ->
                val picked = current == score
                Text(
                    score.toString(),
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = if (picked) FontWeight.Bold else FontWeight.Normal,
                    color = if (picked) p.hlInk else p.c400,
                    modifier = Modifier
                        .size(30.dp)
                        .clip(androidx.compose.foundation.shape.CircleShape)
                        .background(if (picked) p.hlMid else androidx.compose.ui.graphics.Color.Transparent)
                        .clickable { set(if (picked) null else score) }
                        .padding(6.dp),
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
            }
        }
    }
}

@Composable
private fun NotesEditor(notes: String, save: (String) -> Unit) {
    val p = Backhog.palette
    var draft by remember(notes) { mutableStateOf(notes) }
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedTextField(
            value = draft,
            onValueChange = { draft = it },
            label = { Text("Notes") },
            modifier = Modifier.fillMaxWidth(),
            minLines = 2,
        )
        if (draft != notes) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = { save(draft) }) { Text("Save notes") }
                TextButton(onClick = { draft = notes }) { Text("Revert") }
            }
        } else if (notes.isNotEmpty()) {
            Text("Saved", style = MaterialTheme.typography.labelSmall, color = p.c600)
        }
    }
}

@Composable
private fun FactRow(label: String, value: String) {
    val p = Backhog.palette
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = p.c500, modifier = Modifier.weight(1f))
        Text(value, style = MaterialTheme.typography.labelMedium, color = p.c300)
    }
}

/* --------------------------------------------------------------- position */

@Composable
private fun PositionPanel(state: BookDetailState, onScan: () -> Unit, onRead: () -> Unit, onListen: () -> Unit = {}) {
    val p = Backhog.palette
    val entry = state.entry ?: return
    val position = state.position ?: return
    val editions = state.book?.editions.orEmpty()
    val copy = state.drivingCopy
    val pageCount = pageCountFor(editions, copy?.editionId, entry.editionId)
    val paged = position.positionMode == "page"
    val hasText = state.files?.files?.any { it.kind == "epub" } == true

    Panel {
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionLabel("Where you are")
            position.updatedAt?.let {
                Text(
                    "${describeSource(position.source)} · ${it.take(10)}",
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c500,
                )
            }
        }

        Text(
            positionHeadline(position, pageCount),
            style = MaterialTheme.typography.titleLarge,
            color = p.cMax,
        )
        val complete = position.percent >= 100.0
        Text(
            if (complete) "Finished" else "${Math.round(position.percent)}%",
            style = MaterialTheme.typography.labelMedium,
            color = p.c300,
        )
        ProgressBar(position.percent.toFloat() / 100f)

        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (!paged) position.chapter?.let { chapter ->
                CoordinateRow("Chapter", chapterTitle(chapter))
            }
            if (!paged) position.page?.let { page ->
                CoordinateRow("On paper", formatPage(page) + (pageCount?.let { " of $it" } ?: ""), explainPage(page))
            }
            position.audio?.let { audio ->
                CoordinateRow(
                    "On tape",
                    "${formatTimecode(audio.seconds)} of ${formatTimecode(audio.totalDuration)}",
                )
            }
        }

        // The reader opens whenever there is something to read: an attached
        // text, or a paged primary. The button says what it does.
        if (hasText || paged) {
            Button(onClick = onRead, modifier = Modifier.fillMaxWidth()) {
                Text(if (position.updatedAt == null) "Start reading" else "Continue reading")
            }
        }

        // The player opens whenever a designated recording exists; the audio
        // view of the stored position says whether the tape has been started.
        if (state.files?.audioEditions?.isNotEmpty() == true) {
            OutlinedButton(onClick = onListen, modifier = Modifier.fillMaxWidth()) {
                Text(if (position.audio != null && position.updatedAt != null) "Continue listening" else "Listen")
            }
        }

        if (!paged) {
            val scannable = copy != null && position.charCount > 0
            if (scannable) {
                OutlinedButton(onClick = onScan, modifier = Modifier.fillMaxWidth()) { Text("Scan a page") }
                if (entry.startedAt == null) {
                    Text(
                        "Not started — a scan sets your place.",
                        style = MaterialTheme.typography.labelSmall,
                        color = p.c500,
                    )
                }
            } else {
                Text(
                    if (position.charCount == 0L) {
                        "Attach the ebook and a photographed page can be matched into the text."
                    } else {
                        "Register your paper copy under On paper below, and a photo of the page you're on becomes your position."
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = p.c500,
                )
            }
        }
    }
}

@Composable
private fun CoordinateRow(label: String, value: String, hint: String? = null) {
    val p = Backhog.palette
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = p.c500, modifier = Modifier.weight(1f))
        Text(
            value,
            style = MaterialTheme.typography.labelMedium,
            color = p.c300,
        )
    }
    hint?.let {
        Text(it, style = MaterialTheme.typography.labelSmall, color = p.c600)
    }
}

/* --------------------------------------------------------------- sessions */

@Composable
private fun SessionsPanel(state: BookDetailState, onAdd: () -> Unit) {
    val p = Backhog.palette
    Panel {
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionLabel("Reading log")
            TextButton(onClick = onAdd) { Text("Log a session") }
        }
        if (state.sessions.isEmpty()) {
            Text("Nothing logged yet.", style = MaterialTheme.typography.bodySmall, color = p.c500)
        } else {
            state.sessions.take(8).forEach { session ->
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(
                        session.playedOn.take(10),
                        style = MaterialTheme.typography.labelMedium,
                        color = p.c500,
                        modifier = Modifier.width(84.dp),
                    )
                    Text(
                        "${session.minutes / 60}h ${session.minutes % 60}m",
                        style = MaterialTheme.typography.labelMedium,
                        color = p.c300,
                        modifier = Modifier.width(64.dp),
                    )
                    Text(
                        session.note,
                        style = MaterialTheme.typography.labelMedium,
                        color = p.c400,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
    }
}

@Composable
private fun AddSessionDialog(vm: BookDetailViewModel, onDismiss: () -> Unit) {
    var minutes by remember { mutableStateOf("") }
    var note by remember { mutableStateOf("") }
    val parsed = minutes.toIntOrNull()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Log a reading session") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Field(
                    value = minutes,
                    onValueChange = { minutes = it.filter { c -> c.isDigit() } },
                    label = "Minutes",
                    keyboardType = KeyboardType.Number,
                )
                Field(value = note, onValueChange = { note = it }, label = "Note (optional)")
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    parsed?.let { vm.addSession(it, note) }
                    onDismiss()
                },
                enabled = parsed != null && parsed > 0,
            ) { Text("Log") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/* ------------------------------------------------------------------ copies */

@Composable
private fun CopiesPanel(state: BookDetailState, vm: BookDetailViewModel, onRegister: () -> Unit) {
    val p = Backhog.palette
    val copies = state.copies?.copies ?: return

    Panel {
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionLabel("On paper")
            TextButton(onClick = onRegister) { Text("Register a copy") }
        }
        if (copies.isEmpty()) {
            Text(
                "No printings registered. Register the copy you hold and its page numbers turn on.",
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
            )
        } else {
            copies.forEach { copy -> CopyRow(copy, state, vm) }
        }
    }
}

@Composable
private fun CopyRow(copy: com.collinpendleton.backhog.api.PhysicalCopy, state: BookDetailState, vm: BookDetailViewModel) {
    val p = Backhog.palette
    var expanded by remember { mutableStateOf(false) }
    val editions = state.book?.editions.orEmpty()
    val edition = editions.firstOrNull { it.id == copy.editionId }

    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    edition?.let { editionLabel(it).ifEmpty { it.id } } ?: copy.editionId,
                    style = MaterialTheme.typography.labelMedium,
                    color = p.c200,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val bits = buildList {
                    add(if (copy.acquisition == "borrowed") "Borrowed" else "Owned")
                    if (copy.returnedAt != null) add("returned")
                    add(
                        "${copy.anchorCount} page${if (copy.anchorCount == 1) "" else "s"} mapped" +
                            if (copy.seededCount > 0) " (${copy.seededCount} seeded)" else "",
                    )
                    if (copy.drivesPages) add("drives page numbers")
                }
                Text(bits.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = p.c500)
            }
            TextButton(onClick = { expanded = !expanded }) { Text(if (expanded) "Hide" else "Manage") }
        }

        if (expanded) {
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                OutlinedButton(onClick = { vm.deleteCopy(copy.id) }, modifier = Modifier.weight(1f)) {
                    Icon(Icons.Filled.Delete, contentDescription = null, modifier = Modifier.size(14.dp))
                    Spacer(Modifier.width(4.dp))
                    Text("Drop")
                }
            }
            if (copy.acquisition == "borrowed" && copy.returnedAt == null) {
                OutlinedButton(onClick = { vm.returnCopy(copy.id) }, modifier = Modifier.fillMaxWidth()) {
                    Text("Mark returned")
                }
            }
            if (copy.returnedAt != null) {
                OutlinedButton(onClick = { vm.reopenCopy(copy.id) }, modifier = Modifier.fillMaxWidth()) {
                    Text("Check out again")
                }
            }
            if (copy.acquisition == "borrowed" && copy.returnedAt == null) {
                OutlinedButton(onClick = { vm.ownCopy(copy.id) }, modifier = Modifier.fillMaxWidth()) {
                    Text("Bought it — mark owned")
                }
            }
            AnchorList(vm, copy.id)
        }
    }
}

/** The page map itself: what is pinned, and how sure each pin is. */
@Composable
private fun AnchorList(vm: BookDetailViewModel, copyId: String) {
    val p = Backhog.palette
    var anchors by remember { mutableStateOf<List<com.collinpendleton.backhog.api.PageAnchor>?>(null) }
    LaunchedEffect(copyId) {
        anchors = vm.anchorsFor(copyId).getOrNull()
    }
    val list = anchors ?: return
    if (list.isEmpty()) {
        Text("No pages pinned yet.", style = MaterialTheme.typography.labelSmall, color = p.c600)
        return
    }
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        list.sortedByDescending { it.printedPage }.forEach { anchor ->
            Text(
                "page ${anchor.printedPage} · ${anchor.source} · ${(anchor.confidence * 100).toInt()}%",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
    }
}

@Composable
private fun RegisterCopyDialog(state: BookDetailState, vm: BookDetailViewModel, onDismiss: () -> Unit) {
    val editions = state.book?.editions.orEmpty().let { com.collinpendleton.backhog.books.sortEditions(it) }
    var editionId by remember { mutableStateOf(state.entry?.editionId ?: "") }
    var borrowed by remember { mutableStateOf(false) }
    var dueAt by remember { mutableStateOf("") }
    var notes by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Register a printing") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                if (editions.isEmpty()) {
                    Text("No printings on file for this work.")
                } else {
                    Text("Which printing is it?", style = MaterialTheme.typography.labelMedium)
                    editions.take(8).forEach { edition ->
                        FilterChip(
                            selected = editionId == edition.id,
                            onClick = { editionId = edition.id },
                            label = {
                                Text(
                                    editionLabel(edition).ifEmpty { edition.id },
                                    style = MaterialTheme.typography.labelSmall,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            },
                        )
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(selected = !borrowed, onClick = { borrowed = false }, label = { Text("Owned") })
                    FilterChip(selected = borrowed, onClick = { borrowed = true }, label = { Text("Borrowed") })
                }
                if (borrowed) {
                    Field(
                        value = dueAt,
                        onValueChange = { dueAt = it },
                        label = "Due back (YYYY-MM-DD, optional)",
                        keyboardType = KeyboardType.Number,
                    )
                }
                Field(value = notes, onValueChange = { notes = it }, label = "Notes (optional)")
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    vm.registerCopy(editionId.ifEmpty { editions.firstOrNull()?.id ?: "" }, borrowed, dueAt, notes)
                    onDismiss()
                },
                enabled = editions.isNotEmpty(),
            ) { Text("Register") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/* ------------------------------------------------------------------- files */

/**
 * The attached-files surface. A manager sees the whole file layer — formats,
 * recordings, promotion with its stated consequences, detachment, and the
 * alignment queue. A reader sees the reader-safe summary: the server blanks
 * NAS paths for them, and audio presence is the useful fact.
 */
@Composable
private fun FilesPanel(
    state: BookDetailState,
    vm: BookDetailViewModel,
    canManageMedia: Boolean,
    onOpenFiles: () -> Unit,
) {
    val p = Backhog.palette
    val files = state.files ?: return
    if (files.files.isEmpty() && files.audioEditions.isEmpty()) return

    if (!canManageMedia) {
        Panel {
            SectionLabel("Attached")
            val text = files.files.filter { it.kind == "epub" }
            if (text.isNotEmpty()) {
                Text("Ebook attached — read it from Where you are.", style = MaterialTheme.typography.bodySmall, color = p.c400)
            }
            files.audioEditions.forEach { edition ->
                val badge = buildList {
                    add("${edition.trackCount} tracks")
                    add(formatTimecode(edition.totalDuration))
                    if (edition.primary) add("the one that plays")
                    if (edition.degraded) add("durations incomplete")
                    if (edition.missingCount > 0) add("${edition.missingCount} missing")
                }.joinToString(" · ")
                Text(edition.label.ifEmpty { "A recording" }, style = MaterialTheme.typography.labelMedium, color = p.c300)
                Text(badge, style = MaterialTheme.typography.labelSmall, color = p.c500)
            }
        }
        return
    }

    val texts = files.files.filter { it.kind == "epub" }
    val editions = files.audioEditions
    val current = texts.firstOrNull { it.primaryText }
    val currentEdition = editions.firstOrNull { it.primary }

    Panel {
        SectionLabel("Files")
        Text(
            "What this book is read and listened from. Nothing under the NAS mounts is ever written — only these associations.",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )

        if (files.files.any { it.missingAt != null } || editions.any { it.missingCount > 0 }) {
            Text(
                "Some files are missing from the NAS right now — the drive is probably offline. The associations are kept; nothing is deleted, and a scan marks them restored when the drive returns.",
                style = MaterialTheme.typography.labelSmall,
                color = p.hlBright,
            )
        }

        // --- ebook formats: which text everything is measured against ---
        if (texts.size >= 2) {
            Text("Ebook formats", style = MaterialTheme.typography.titleSmall, color = p.c200)
            Text(
                "One of these is the text your place, percentage and audio alignment are measured against.",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
            var confirmPromote by remember { mutableStateOf<Long?>(null) }
            texts.forEach { file ->
                val name = file.path.split('/').lastOrNull() ?: file.path
                Row(
                    Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(name, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace, color = p.c300, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (file.missingAt != null) {
                            Text("missing from the NAS right now", style = MaterialTheme.typography.labelSmall, color = p.hlBright)
                        }
                    }
                    if (file.primaryText) {
                        ToneChip("Reading this", Tones.Played)
                    } else {
                        TextButton(
                            onClick = { confirmPromote = file.id },
                            enabled = file.missingAt == null,
                        ) { Text("Read this instead") }
                    }
                }
            }
            Text(
                "Switching keeps your percentage through the book and recomputes the exact spot in the new text. Any audio alignment is dropped, because it was built against ${current?.path?.split('/')?.lastOrNull() ?: "the other file"} and would point at the wrong words.",
                style = MaterialTheme.typography.labelSmall,
                color = p.c600,
            )
            confirmPromote?.let { fileId ->
                AlertDialog(
                    onDismissRequest = { confirmPromote = null },
                    title = { Text("Change the reading text?") },
                    text = {
                        Text("Your position is carried over by percentage and recomputed on the new text — the exact paragraph may move. Any audio alignment is deleted.")
                    },
                    confirmButton = {
                        TextButton(onClick = {
                            vm.promoteTextFile(fileId)
                            confirmPromote = null
                        }) { Text("Switch") }
                    },
                    dismissButton = { TextButton(onClick = { confirmPromote = null }) { Text("Cancel") } },
                )
            }
        }

        // --- audio editions: which recording plays ---
        if (editions.size >= 2) {
            Text("Audiobook versions", style = MaterialTheme.typography.titleSmall, color = p.c200)
            Text(
                "One of these is the audiobook — the one the player plays, and the one your place in the audio is measured against.",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
            editions.forEach { edition ->
                val unplayable = edition.missingCount >= edition.trackCount
                Row(
                    Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(edition.label, style = MaterialTheme.typography.labelMedium, color = p.c300, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            buildString {
                                edition.narrator?.let { append("read by $it · ") }
                                append(if (edition.degraded) "length unknown" else formatTimecode(edition.totalDuration))
                                append(" · ${edition.trackCount} file${if (edition.trackCount == 1) "" else "s"}")
                            },
                            style = MaterialTheme.typography.labelSmall,
                            color = p.c600,
                        )
                        if (edition.missingCount > 0) {
                            Text(
                                if (unplayable) "missing from the NAS right now" else "${edition.missingCount} of its files are missing from the NAS right now",
                                style = MaterialTheme.typography.labelSmall,
                                color = p.hlBright,
                            )
                        }
                    }
                    if (edition.primary) {
                        ToneChip("Listening to this", Tones.Played)
                    } else {
                        TextButton(
                            onClick = { vm.promoteAudioEdition(edition.id) },
                            enabled = !unplayable,
                        ) { Text("Listen to this instead") }
                    }
                }
            }
            Text(
                "Switching keeps how far through the book you are and resumes at the same fraction of the new recording. Any audio alignment is dropped, because it was built against ${currentEdition?.label ?: "the other recording"}.",
                style = MaterialTheme.typography.labelSmall,
                color = p.c600,
            )
        } else {
            editions.forEach { edition ->
                Text(
                    buildString {
                        append(edition.label.ifEmpty { "A recording" })
                        append(" · ${edition.trackCount} tracks · ")
                        append(if (edition.degraded) "durations incomplete" else formatTimecode(edition.totalDuration))
                    },
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c500,
                )
            }
        }

        // --- every file: detach ---
        Text("Attached files", style = MaterialTheme.typography.titleSmall, color = p.c200)
        var confirmDetach by remember { mutableStateOf<Long?>(null) }
        files.files.forEach { file ->
            val name = file.path.split('/').lastOrNull() ?: file.path
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(name, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = p.c400, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    if (file.primaryText) Text("the reading text", style = MaterialTheme.typography.labelSmall, color = p.c600)
                }
                TextButton(onClick = { confirmDetach = file.id }) { Text("Detach") }
            }
        }
        confirmDetach?.let { fileId ->
            AlertDialog(
                onDismissRequest = { confirmDetach = null },
                title = { Text("Detach this file?") },
                text = { Text("The book stops being read or listened from it. The file itself stays on the NAS.") },
                confirmButton = {
                    TextButton(onClick = {
                        vm.detachFile(fileId)
                        confirmDetach = null
                    }) { Text("Detach") }
                },
                dismissButton = { TextButton(onClick = { confirmDetach = null }) { Text("Cancel") } },
            )
        }

        OutlinedButton(onClick = onOpenFiles, modifier = Modifier.fillMaxWidth()) {
            Text("Scan and attach in Book files")
        }
    }

    // --- alignment: the text↔audio map --------------------------------
    val hasText = texts.isNotEmpty()
    val hasAudio = editions.isNotEmpty()
    LaunchedEffect(hasText, hasAudio) {
        if (hasText && hasAudio) vm.loadAlign()
    }
    if (hasText || hasAudio) {
        AlignmentPanel(state, vm)
    }
}

/** The states an alignment can be in, each with its honest words. */
@Composable
private fun AlignmentPanel(state: BookDetailState, vm: BookDetailViewModel) {
    val p = Backhog.palette
    val align = state.align

    Panel {
        SectionLabel("Audio alignment")
        Text("The map that lets reading and listening hand off to each other.", style = MaterialTheme.typography.labelSmall, color = p.c500)

        when {
            align == null -> {}
            align.job != null && align.job!!.state in setOf("queued", "claimed", "transcribing", "aligning") -> {
                val job = align.job!!
                val label = when (job.state) {
                    "queued" -> if (!align.workerEnabled) "Queued — no worker running" else "Queued"
                    "claimed" -> "Starting"
                    "transcribing" -> "Transcribing ${Math.round(job.progress * 100)}%"
                    else -> "Aligning ${Math.round(job.progress * 100)}%"
                }
                Text(label, style = MaterialTheme.typography.titleSmall, color = p.hlBright)
                if (job.state == "transcribing" || job.state == "aligning") {
                    ProgressBar((job.progress * 100).toFloat().coerceIn(2f, 100f))
                }
                Text(
                    when {
                        job.state == "queued" && !align.workerEnabled ->
                            "No alignment worker is running, so this will wait until one is. The rest of the book keeps working meanwhile."
                        job.state == "queued" -> "Waiting for the alignment worker to pick it up."
                        job.stageDetail.isNotEmpty() -> job.stageDetail
                        else -> "Turning the audiobook into text and matching it to the pages. This runs at a few times listening speed, so a long book takes a while."
                    },
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c400,
                )
                if (job.state == "queued") {
                    TextButton(onClick = vm::clearAlignment) { Text("Cancel") }
                }
            }
            align.job?.state == "failed" -> {
                Text("Alignment failed", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.error)
                Text(align.job!!.error ?: "The worker could not finish this alignment.", style = MaterialTheme.typography.labelSmall, color = p.c400)
                OutlinedButton(onClick = vm::enqueueAlignment) { Text("Try again") }
            }
            align.alignment != null && align.alignment!!.state in setOf("ready", "low_confidence") -> {
                val record = align.alignment!!
                Text(
                    if (record.state == "ready") "Ready" else "Low confidence",
                    style = MaterialTheme.typography.titleSmall,
                    color = if (record.state == "ready") Tones.Played else Tones.Wishlist,
                )
                Text(
                    "${Math.round(record.coverage * 100)}% covered · ${Math.round(record.meanConfidence * 100)}% confident",
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c300,
                )
                if (record.state == "low_confidence") {
                    Text(
                        "This audiobook doesn't match this ebook closely enough — it may be abridged or a different translation. Handoff between reading and listening still works, but expect it to land near, not exactly on, your page.",
                        style = MaterialTheme.typography.labelSmall,
                        color = Tones.Wishlist,
                    )
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = vm::enqueueAlignment) { Text("Re-run alignment") }
                    TextButton(onClick = vm::clearAlignment) { Text("Clear") }
                }
            }
            else -> {
                Text("Not aligned", style = MaterialTheme.typography.titleSmall, color = p.c300)
                Text(
                    "Aligning transcribes the audiobook and matches it against the text, so positions translate both ways.",
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c400,
                )
                OutlinedButton(onClick = vm::enqueueAlignment, enabled = !state.busy) { Text("Align this book") }
            }
        }
    }
}

/* ------------------------------------------------------------------ shares */

@Composable
private fun SharePanel(state: BookDetailState, vm: BookDetailViewModel) {
    val p = Backhog.palette
    val candidates = state.shareCandidates
    val sharedCount = candidates.count { it.shared }
    Panel {
        SectionLabel("Sharing")
        if (candidates.isEmpty()) {
            Text(
                "No one else to share with — invites come from the web's admin panel.",
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
            )
        } else {
            Text(
                if (sharedCount == 0) "Not shared" else "Shared with $sharedCount",
                style = MaterialTheme.typography.bodySmall,
                color = p.c400,
            )
            candidates.forEach { candidate ->
                Row(
                    Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(candidate.username, style = MaterialTheme.typography.labelMedium, color = p.c200)
                        if (candidate.inLibrary) {
                            Text("on their shelf", style = MaterialTheme.typography.labelSmall, color = p.c600)
                        }
                    }
                    if (candidate.shared) {
                        TextButton(onClick = { vm.unshare(candidate.userId) }) { Text("Unshare") }
                    } else {
                        TextButton(onClick = { vm.share(candidate.userId) }) { Text("Share") }
                    }
                }
            }
        }
    }
}

@Composable
private fun DangerPanel(vm: BookDetailViewModel, onConfirm: () -> Unit) {
    OutlinedButton(onClick = onConfirm, modifier = Modifier.fillMaxWidth()) {
        Text("Remove from shelf", color = MaterialTheme.colorScheme.error)
    }
}
