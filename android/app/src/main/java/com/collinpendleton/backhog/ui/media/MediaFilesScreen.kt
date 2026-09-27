package com.collinpendleton.backhog.ui.media

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.BookSearchResult
import com.collinpendleton.backhog.api.MediaCandidate
import com.collinpendleton.backhog.api.MediaFile
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.books.byline
import com.collinpendleton.backhog.books.formatTimecode
import com.collinpendleton.backhog.books.publishYear
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.Panel
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.books.SectionLabel
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Palette
import com.collinpendleton.backhog.ui.theme.Tones
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.launch

/**
 * The file layer: the attach review queue and the raw path browser, open to
 * member and admin accounts only — the same gate the server's
 * RequireMediaManager keeps. The scanner knows what files exist, the matcher
 * proposes which book each one is, and this screen is where a human says
 * yes: confirm the suggestion, pick a different book, or ignore the files.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MediaFilesScreen(
    container: AppContainer,
    baseUrl: String,
    onBack: () -> Unit,
) {
    val vm: MediaFilesViewModel = viewModel(factory = MediaFilesViewModel.Factory(container, baseUrl))
    val state by vm.state.collectAsState()
    val p = Backhog.palette

    var kind by remember { mutableStateOf("") }
    var showSkipped by remember { mutableStateOf(false) }
    var showBrowser by remember { mutableStateOf(false) }
    var pickFor by remember { mutableStateOf<MediaCandidate?>(null) }

    LaunchedEffect(state.status) {
        if (state.status != null) {
            kotlinx.coroutines.delay(4000)
            vm.clearStatus()
        }
    }

    Scaffold(
        containerColor = p.c950,
        topBar = {
            TopAppBar(
                title = { Text("Book files", maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
                actions = {
                    if (state.scan?.running == true) {
                        CircularProgressIndicator(
                            modifier = Modifier.padding(horizontal = 12.dp).padding(end = 4.dp),
                            strokeWidth = 2.dp,
                            color = p.hlBright,
                        )
                    }
                    IconButton(
                        onClick = { vm.kickScan() },
                        enabled = state.scan?.running != true,
                    ) {
                        Icon(Icons.Filled.Refresh, contentDescription = "Scan now")
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
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(horizontal = 16.dp)
                .padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            state.scan?.let { scan ->
                ScanStrip(scan, p)
            }
            state.status?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = p.hlBright) }
            ErrorText(state.error)

            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(selected = kind == "", onClick = { kind = "" }, label = { Text("Review queue") })
                FilterChip(selected = kind == "audio", onClick = { kind = "audio" }, label = { Text("Audio") })
                FilterChip(selected = kind == "epub", onClick = { kind = "epub" }, label = { Text("Ebooks") })
                FilterChip(selected = showBrowser, onClick = { showBrowser = !showBrowser }, label = { Text("All files") })
            }

            if (showBrowser) {
                InventoryBrowser(state, p, onFilters = { k, u -> vm.setInventoryFilters(k, u) })
            } else when {
                state.loading -> Row(
                    Modifier.fillMaxWidth().padding(vertical = 40.dp),
                    horizontalArrangement = Arrangement.Center,
                ) { CircularProgressIndicator() }

                else -> {
                    val all = state.queue?.candidates ?: emptyList()
                    val candidates = if (kind.isEmpty()) all else all.filter { it.kind == kind }
                    if (candidates.isEmpty()) {
                        Text(
                            if (all.isEmpty()) {
                                if (state.queue != null) {
                                    "Every scanned file is attached or ignored. Kick a scan to pick up new files."
                                } else {
                                    "Kick a scan to inventory the NAS mount, then the matcher will propose books for what it finds."
                                }
                            } else {
                                "Every candidate of this type is attached or decided. Clear the filter to see the rest of the queue."
                            },
                            style = MaterialTheme.typography.bodySmall,
                            color = p.c500,
                            modifier = Modifier.padding(vertical = 24.dp),
                        )
                    } else {
                        LazyColumn(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                            val bulkable = candidates.filter { it.highConfidence && !(it.suggestions ?: emptyList()).isEmpty() }
                            if (bulkable.isNotEmpty()) {
                                item {
                                    BulkPanel(bulkable.size, state.bulkRunning, onConfirm = vm::bulkConfirm, p = p)
                                }
                            }
                            items(candidates, key = { it.key }) { candidate ->
                                CandidateCard(
                                    candidate = candidate,
                                    busy = candidate.key in state.busy,
                                    onConfirm = { suggestion -> vm.confirm(candidate, suggestion) },
                                    onPick = { pickFor = candidate },
                                    onIgnore = { vm.ignore(candidate) },
                                    p = p,
                                )
                            }
                            val skipped = state.queue?.skipped ?: emptyList()
                            if (skipped.isNotEmpty()) {
                                item {
                                    Text(
                                        "${skipped.size} file${if (skipped.size == 1) "" else "s"} not inventoried — formats we don't parse, DRM, and metadata sidecars",
                                        style = MaterialTheme.typography.labelMedium,
                                        color = p.c500,
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .clickable { showSkipped = true }
                                            .padding(vertical = 8.dp),
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    if (showSkipped && !(state.queue?.skipped ?: emptyList()).isEmpty()) {
        ModalBottomSheet(onDismissRequest = { showSkipped = false }) {
            SkippedList(state.queue?.skipped ?: emptyList(), p)
        }
    }
    pickFor?.let { candidate ->
        ModalBottomSheet(onDismissRequest = { pickFor = null }) {
            PickBookSheet(
                container = container,
                baseUrl = baseUrl,
                candidate = candidate,
                onPicked = { result ->
                    pickFor = null
                    vm.attachToBook(candidate, result)
                },
                p = p,
            )
        }
    }
}

/* ------------------------------------------------------------ scan strip */

@Composable
private fun ScanStrip(scan: com.collinpendleton.backhog.api.MediaScanStatus, p: Palette) {
    Panel {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (scan.running) {
                    Text("Scanning — ${scan.found} found", style = MaterialTheme.typography.labelMedium, color = p.hlBright)
                    LinearProgressIndicator(
                        modifier = Modifier.fillMaxWidth(),
                        color = p.hlBright,
                        trackColor = p.c800,
                    )
                } else {
                    val last = scan.last
                    Text(
                        if (last == null) "No scan yet" else "Last scan: ${last.newFiles} new · ${last.found} found",
                        style = MaterialTheme.typography.labelMedium,
                        color = p.c400,
                    )
                    if (last != null && last.missing > 0) {
                        Text(
                            "${last.missing} file${if (last.missing == 1) "" else "s"} missing since — the drive is probably offline; associations are kept.",
                            style = MaterialTheme.typography.labelSmall,
                            color = p.c500,
                        )
                    }
                }
            }
        }
    }
}

/* ---------------------------------------------------------- bulk confirm */

@Composable
private fun BulkPanel(count: Int, running: Boolean, onConfirm: () -> Unit, p: Palette) {
    Panel {
        Text(
            "$count high-confidence match${if (count == 1) "" else "es"}",
            style = MaterialTheme.typography.labelMedium,
            color = p.c100,
        )
        Text(
            "The safest picks in the queue — confirm them all in one go?",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )
        Button(onClick = onConfirm, enabled = !running) {
            if (running) CircularProgressIndicator(modifier = Modifier.padding(4.dp), strokeWidth = 2.dp) else Text("Confirm $count")
        }
    }
}

/* -------------------------------------------------------- candidate card */

@Composable
private fun CandidateCard(
    candidate: MediaCandidate,
    busy: Boolean,
    onConfirm: (com.collinpendleton.backhog.api.MediaSuggestion) -> Unit,
    onPick: () -> Unit,
    onIgnore: () -> Unit,
    p: Palette,
) {
    val top = candidate.suggestions?.firstOrNull()
    val isAudio = candidate.kind == "audio"
    val alternate = candidate.alternateFormat
    val alternateName = candidate.alternateOf?.split('/')?.lastOrNull() ?: ""

    Panel {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ToneChip(
                if (isAudio) "Audio" else "Ebook",
                if (isAudio) Tones.Playing else Tones.Backlog,
            )
            if (alternate) {
                Text("Alternate format", style = MaterialTheme.typography.labelSmall, color = p.c500)
            } else if (candidate.highConfidence) {
                Text("High confidence", style = MaterialTheme.typography.labelSmall, color = p.hlBright)
            }
        }
        Text(
            buildString {
                append(candidate.titleGuess.ifEmpty { candidate.files.firstOrNull()?.path ?: candidate.dirPath })
                if (candidate.authorGuess.isNotEmpty()) append(" — ${candidate.authorGuess}")
            },
            style = MaterialTheme.typography.titleSmall,
            color = p.c100,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            if (candidate.dirPath == ".") candidate.root else "${candidate.root}/${candidate.dirPath}",
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = p.c500,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            buildString {
                append("${candidate.files.size} file${if (candidate.files.size == 1) "" else "s"}")
                if (isAudio && candidate.totalDurationSeconds > 0) append(" · ${formatTimecode(candidate.totalDurationSeconds)}")
            },
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )

        top?.let { suggestion ->
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(suggestion.book.title, style = MaterialTheme.typography.bodyMedium, color = p.c200, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(byline(suggestion.book.authors), style = MaterialTheme.typography.labelSmall, color = p.c400)
                Text(
                    if (alternate) {
                        "already attached as $alternateName"
                    } else {
                        "${Math.round(suggestion.confidence * 100)}% · from ${suggestion.signal} · ${if (suggestion.inLibrary) "in your library" else "Open Library"}"
                    },
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c600,
                )
                if (alternate) {
                    Text(
                        "Adding it records the format you own. The book keeps reading from $alternateName.",
                        style = MaterialTheme.typography.labelSmall,
                        color = p.c500,
                    )
                }
            }
        } ?: Text(
            "No confident match — pick the book yourself, or ignore these files.",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (top != null) {
                Button(onClick = { onConfirm(top) }, enabled = !busy) {
                    Text(if (alternate) "Add format" else "Confirm")
                }
            }
            OutlinedButton(onClick = onPick, enabled = !busy) { Text("Pick book") }
            TextButton(onClick = onIgnore, enabled = !busy) { Text("Ignore") }
            if (busy) CircularProgressIndicator(modifier = Modifier.padding(4.dp), strokeWidth = 2.dp)
        }
    }
}

/* ------------------------------------------------------- raw file browser */

@Composable
private fun InventoryBrowser(
    state: MediaFilesState,
    p: Palette,
    onFilters: (String, Boolean) -> Unit,
) {
    val inventory = state.inventory
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionLabel("The inventory")
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(
                selected = state.inventoryUnattached,
                onClick = { onFilters(state.inventoryKind, !state.inventoryUnattached) },
                label = { Text("Unattached only") },
            )
            FilterChip(
                selected = state.inventoryKind == "epub",
                onClick = { onFilters(if (state.inventoryKind == "epub") "" else "epub", state.inventoryUnattached) },
                label = { Text("Ebooks") },
            )
            FilterChip(
                selected = state.inventoryKind == "audio",
                onClick = { onFilters(if (state.inventoryKind == "audio") "" else "audio", state.inventoryUnattached) },
                label = { Text("Audio") },
            )
        }
        if (inventory == null) {
            Row(Modifier.fillMaxWidth().padding(vertical = 24.dp), horizontalArrangement = Arrangement.Center) {
                CircularProgressIndicator()
            }
        } else {
            Text(
                "${inventory.files.size} file${if (inventory.files.size == 1) "" else "s"} on the mount",
                style = MaterialTheme.typography.labelMedium,
                color = p.c400,
            )
            LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                items(inventory.files, key = { it.id }) { file ->
                    InventoryRow(file, p)
                }
            }
        }
    }
}

@Composable
private fun InventoryRow(file: MediaFile, p: Palette) {
    Column(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Text(
            file.path,
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = p.c300,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            buildString {
                append(if (file.kind == "audio") "audio" else "ebook")
                append(" · ${file.sizeBytes / (1024 * 1024)} MB")
                file.durationSeconds?.let { append(" · ${formatTimecode(it)}") }
                if (file.missingAt != null) append(" · missing from the NAS right now")
            },
            style = MaterialTheme.typography.labelSmall,
            color = if (file.missingAt != null) p.hlBright else p.c600,
        )
    }
}

/* --------------------------------------------------------- skipped files */

private val SKIP_REASONS: Map<String, (String) -> String> = mapOf(
    "unsupported_extension" to { ext ->
        if (ext == ".aax" || ext == ".aaxc") {
            "Audible DRM format — out of scope, this tool is DRM-free by decision"
        } else {
            "Unsupported file type ($ext) — only mp3, m4a, m4b, opus, epub, mobi, azw, azw3 and pdf are inventoried"
        }
    },
    "drm_epub" to { "DRM-wrapped EPUB (encryption.xml) — out of scope, this tool is DRM-free by decision" },
    "drm_mobi" to { "DRM-protected Kindle file (PalmDOC encryption) — out of scope, this tool is DRM-free by decision" },
    "drm_pdf" to { "DRM-wrapped PDF (/Encrypt) — out of scope, this tool is DRM-free by decision" },
    "format_unhandled" to { ext ->
        "Kindle KFX ($ext) — a format with no open reader; convert it to EPUB or MOBI, or attach one of those of the same book"
    },
    "sidecar_metadata" to { "Calibre metadata sidecar — read for title, author and ISBN, and used to match the books beside it. Not a book of its own" },
)

@Composable
private fun SkippedList(skipped: List<com.collinpendleton.backhog.api.MediaSkipped>, p: Palette) {
    val grouped = skipped.groupBy { f ->
        (SKIP_REASONS[f.reason] ?: { ext -> "Skipped (${f.reason}$ext)" })(f.ext)
    }
    Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 40.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text("Not inventoried", style = MaterialTheme.typography.titleMedium, color = p.c100)
        grouped.forEach { (reason, files) ->
            Panel {
                Text("${files.size} file${if (files.size == 1) "" else "s"} — $reason", style = MaterialTheme.typography.labelMedium, color = p.c300)
                files.take(8).forEach { f ->
                    Text(f.path, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = p.c600, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                if (files.size > 8) Text("and ${files.size - 8} more", style = MaterialTheme.typography.labelSmall, color = p.c600)
            }
        }
    }
}

/* ----------------------------------------------------------- pick a book */

@OptIn(FlowPreview::class)
@Composable
private fun PickBookSheet(
    container: AppContainer,
    baseUrl: String,
    candidate: MediaCandidate,
    onPicked: (BookSearchResult) -> Unit,
    p: Palette,
) {
    var query by remember { mutableStateOf(candidate.titleGuess) }
    val results = remember { MutableStateFlow<List<BookSearchResult>>(emptyList()) }

    LaunchedEffect(query) {
        val term = query.trim()
        if (term.length < 2) {
            results.value = emptyList()
            return@LaunchedEffect
        }
        kotlinx.coroutines.delay(300)
        apiCall { container.session.api(baseUrl).searchBooks(term) }
            .onSuccess { results.value = it.results }
            .onFailure { results.value = emptyList() }
    }
    val list by results.collectAsState()

    Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 40.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("Pick the book", style = MaterialTheme.typography.titleMedium, color = p.c100)
        Text(
            "Which book is \"${candidate.titleGuess.ifEmpty { candidate.files.firstOrNull()?.path ?: "" }}\"?",
            style = MaterialTheme.typography.bodySmall,
            color = p.c400,
        )
        OutlinedTextField(
            value = query,
            onValueChange = { query = it },
            placeholder = { Text("Title or author") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(list, key = { it.book.id }) { result ->
                Column(
                    Modifier
                        .fillMaxWidth()
                        .clickable { onPicked(result) }
                        .padding(vertical = 6.dp),
                ) {
                    Text(result.book.title, style = MaterialTheme.typography.bodyMedium, color = p.c100)
                    Text(
                        listOf(byline(result.book.authors), publishYear(result.book.firstPublishYear))
                            .filter { it.isNotEmpty() }
                            .joinToString(" · ") + if (result.inLibrary) " · in your library" else "",
                        style = MaterialTheme.typography.labelSmall,
                        color = p.c400,
                    )
                }
            }
            if (query.trim().length >= 2 && list.isEmpty()) {
                item { Text("No matches yet.", style = MaterialTheme.typography.bodySmall, color = p.c500) }
            }
        }
    }
}
