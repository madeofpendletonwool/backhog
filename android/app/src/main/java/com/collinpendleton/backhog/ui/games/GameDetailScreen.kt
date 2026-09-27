package com.collinpendleton.backhog.ui.games

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Block
import androidx.compose.material.icons.filled.CalendarMonth
import androidx.compose.material.icons.filled.Cancel
import androidx.compose.material.icons.filled.CardGiftcard
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.DeleteOutline
import androidx.compose.material.icons.filled.OpenInNew
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.RemoveCircleOutline
import androidx.compose.material.icons.filled.VideogameAsset
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.Game
import com.collinpendleton.backhog.api.GameExtras
import com.collinpendleton.backhog.api.RelatedGame
import com.collinpendleton.backhog.data.ServerUrl
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.Panel
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Palette
import com.collinpendleton.backhog.ui.theme.Tones
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

/**
 * The game dossier + user column, section-for-section like the web's
 * GameDetailPage: hero, about, facts, screenshots, videos, related games,
 * notes; then playtime, rating, timeline, lists, platform, links, remove.
 */
@Composable
fun GameDetailScreen(
    container: AppContainer,
    baseUrl: String,
    entryId: String,
    onBack: () -> Unit,
    onRemoved: () -> Unit,
) {
    val vm: GameDetailViewModel = viewModel(key = "entry|$baseUrl|$entryId") {
        GameDetailViewModel(container.session, baseUrl, entryId)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette

    if (ui.deleted) {
        onRemoved()
        return
    }

    Box(Modifier.fillMaxSize().background(p.c950)) {
        when {
            ui.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            ui.error != null -> Column(
                Modifier.fillMaxSize().padding(24.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text("That game isn't in your library.", color = p.c300, style = MaterialTheme.typography.bodyLarge)
                Text(ui.error ?: "", color = p.c500, style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = onBack) { Text("Back to library") }
            }
            else -> {
                val entry = ui.entry ?: return
                DetailBody(entry, ui, vm, baseUrl, onBack)
            }
        }
    }
}

@Composable
private fun DetailBody(
    entry: Entry,
    ui: GameDetailUiState,
    vm: GameDetailViewModel,
    baseUrl: String,
    onBack: () -> Unit,
) {
    val p = Backhog.palette
    val game = entry.game ?: return
    val extras = game.extras
    var confirmDelete by remember { mutableStateOf(false) }

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Hero(entry, game, extras, baseUrl, onBack)

        Column(
            Modifier.padding(horizontal = 16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            ErrorText(ui.actionError)

            StatusSection(entry, ui.busy, vm)
            SessionPanel(ui, vm)
            RatingPanel(entry, vm)
            NotesPanel(ui, vm)

            AboutPanel(game)
            FactsPanel(game, extras)
            if (extras != null && extras.screenshotImageIds.isNotEmpty()) ScreenshotsPanel(extras)
            if (extras != null && extras.videos.isNotEmpty()) VideosPanel(extras)
            if (extras != null && extras.similarGames.isNotEmpty()) RelatedPanel("Similar games", extras.similarGames)
            if (extras != null && extras.expansions.isNotEmpty()) RelatedPanel("Expansions", extras.expansions)
            if (extras != null && extras.dlcs.isNotEmpty()) RelatedPanel("DLC & add-ons", extras.dlcs)

            TimelinePanel(entry)
            if (ui.listNames.isNotEmpty()) ListsPanel(ui.listNames)
            if (game.platforms.isNotEmpty()) PlatformPanel(entry, game, vm)
            if (extras != null && extras.websites.isNotEmpty()) LinksPanel(extras)

            OutlinedButton(
                onClick = { confirmDelete = true },
                enabled = !ui.busy,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Icon(Icons.Filled.DeleteOutline, contentDescription = null, tint = Tones.Dropped, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(6.dp))
                Text("Remove from library", color = Tones.Dropped)
            }
            Spacer(Modifier.height(28.dp))
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("Remove ${game.name}?") },
            text = {
                Text("This removes it from your library, along with your rating and notes. The game itself stays searchable, so you can add it again later.")
            },
            confirmButton = {
                TextButton(onClick = { confirmDelete = false; vm.delete() }) { Text("Remove", color = Tones.Dropped) }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text("Cancel") } },
        )
    }
}

// --- hero ---------------------------------------------------------------------

@Composable
private fun Hero(entry: Entry, game: Game, extras: GameExtras?, baseUrl: String, onBack: () -> Unit) {
    val p = Backhog.palette
    val context = LocalContext.current
    fun open(url: String) {
        runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))) }
    }

    Box(Modifier.fillMaxWidth()) {
        // The cover, blown up and blurred behind its own artwork.
        Box(Modifier.matchParentSize().alpha(0.25f).blur(28.dp)) {
            AsyncImage(
                model = ServerUrl.gameCoverUrl(baseUrl, game.id),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.matchParentSize(),
            )
        }
        Box(
            Modifier.matchParentSize().background(
                Brush.verticalGradient(0f to p.c950.copy(alpha = 0.45f), 1f to p.c950),
            ),
        )

        Column(Modifier.padding(horizontal = 16.dp)) {
            TextButton(onClick = onBack, contentPadding = androidx.compose.foundation.layout.PaddingValues(start = 0.dp)) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = null, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(4.dp))
                Text("Library")
            }

            Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                Box(
                    Modifier
                        .width(108.dp)
                        .aspectRatio(3f / 4f)
                        .clip(MaterialTheme.shapes.medium)
                        .border(1.dp, p.edgeStrong, MaterialTheme.shapes.medium),
                ) {
                    AsyncImage(
                        model = ServerUrl.gameCoverUrl(baseUrl, game.id),
                        contentDescription = null,
                        contentScale = ContentScale.Crop,
                        modifier = Modifier.matchParentSize(),
                    )
                }

                Column(Modifier.padding(top = 4.dp)) {
                    Text(
                        game.name,
                        style = MaterialTheme.typography.headlineSmall,
                        fontWeight = FontWeight.SemiBold,
                        color = p.cMax,
                    )
                    val metaRows = buildList {
                        Format.releaseDate(game.firstReleaseDate).takeIf { it.isNotEmpty() }?.let { add(it) }
                        extras?.developer?.takeIf { it.isNotBlank() }?.let { add(it) }
                        game.timeToBeatMain?.let { add("${Format.duration(it)} to beat") }
                        game.timeToBeatComplete?.let { add("${Format.duration(it)} to 100%") }
                        game.igdbRating?.let { add("${Math.round(it)} on IGDB") }
                        extras?.aggregatedRating?.let { add("${Math.round(it)} critics") }
                    }
                    if (metaRows.isNotEmpty()) {
                        Text(
                            metaRows.joinToString(" · "),
                            style = MaterialTheme.typography.bodySmall,
                            color = p.c300,
                        )
                    }
                    if (game.genres.isNotEmpty()) {
                        Row(
                            Modifier.padding(top = 8.dp).horizontalScroll(rememberScrollState()),
                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            game.genres.forEach { genre ->
                                ToneChip(genre.name, p.hlMid)
                            }
                        }
                    }
                }
            }
            Spacer(Modifier.height(16.dp))
        }
    }
}

// --- the user column -------------------------------------------------------------

/** The full six-state menu; "played" on a platformless game asks which one, like the web. */
@Composable
private fun StatusSection(entry: Entry, busy: Boolean, vm: GameDetailViewModel) {
    val game = entry.game ?: return
    var askPlatform by remember { mutableStateOf(false) }
    val p = Backhog.palette

    fun choose(status: EntryStatus) {
        if (entry.status == status) return
        if (status == EntryStatus.Played && entry.platformId == null && game.platforms.isNotEmpty()) {
            askPlatform = true
            return
        }
        vm.patch { status(status) }
    }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(
            Modifier
                .fillMaxWidth()
                .clip(MaterialTheme.shapes.small)
                .background(p.c850)
                .border(1.dp, p.edgeStrong, MaterialTheme.shapes.small)
                .alpha(if (busy) 0.6f else 1f),
        ) {
            EntryStatus.all.forEach { status ->
                val active = entry.status == status
                Box(
                    Modifier
                        .weight(1f)
                        .clickable(enabled = !busy) { choose(status) }
                        .background(if (active) Tones.forStatus(status) else Color.Transparent)
                        .padding(vertical = 10.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        statusIcon(status),
                        contentDescription = status.gameLabel,
                        tint = if (active) Color(0xFF0B0E14) else p.c400,
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
        }
        Text(
            "${entry.status.gameLabel} — tap to change. Wishlist moves it to your shopping list.",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )
    }

    if (askPlatform) {
        AlertDialog(
            onDismissRequest = { askPlatform = false; vm.patch { status(EntryStatus.Played) } },
            title = { Text("Which platform did you play ${game.name} on?") },
            text = {
                Column {
                    Text(
                        "Platform trophies count the system you actually finished on, not every system the game released for.",
                        style = MaterialTheme.typography.bodySmall,
                    )
                    Spacer(Modifier.height(12.dp))
                    game.platforms.forEach { platform ->
                        Text(
                            platform.name,
                            color = p.c200,
                            style = MaterialTheme.typography.bodyMedium,
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(MaterialTheme.shapes.small)
                                .clickable {
                                    askPlatform = false
                                    vm.patch {
                                        status(EntryStatus.Played)
                                        platform(platform.id)
                                    }
                                }
                                .padding(vertical = 10.dp, horizontal = 8.dp),
                        )
                    }
                }
            },
            confirmButton = {
                TextButton(onClick = { askPlatform = false; vm.patch { status(EntryStatus.Played) } }) {
                    Text("Skip for now")
                }
            },
        )
    }
}

internal fun statusIcon(status: EntryStatus): androidx.compose.ui.graphics.vector.ImageVector = when (status) {
    EntryStatus.Backlog -> Icons.Filled.RemoveCircleOutline
    EntryStatus.Playing -> Icons.Filled.PlayArrow
    EntryStatus.Played -> Icons.Filled.CheckCircle
    EntryStatus.Dropped -> Icons.Filled.Cancel
    EntryStatus.Ignored -> Icons.Filled.Block
    EntryStatus.Wishlist -> Icons.Filled.CardGiftcard
}

/** "Playtime": the running total vs the estimate, the log form, the history. */
@Composable
private fun SessionPanel(ui: GameDetailUiState, vm: GameDetailViewModel) {
    val entry = ui.entry ?: return
    val game = entry.game ?: return
    val estimate = game.timeToBeatMain
    var open by remember { mutableStateOf(false) }
    val p = Backhog.palette

    Panel {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("Playtime", style = MaterialTheme.typography.titleSmall, color = p.c200)
                Text(
                    if (ui.totalMinutes > 0) buildString {
                        append(Format.minutes(ui.totalMinutes))
                        append(" logged")
                        estimate?.let { append(" · ~${Format.hours(it / 3600.0)} to beat") }
                    } else "Nothing logged yet",
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c500,
                )
            }
            if (!open) {
                OutlinedButton(onClick = { open = true }, enabled = !ui.busy) { Text("Log") }
            }
        }

        if (ui.totalMinutes > 0 && estimate != null && estimate > 0) {
            LinearProgressIndicator(
                progress = { (ui.totalMinutes * 60f / estimate).coerceIn(0f, 1f) },
                modifier = Modifier.fillMaxWidth().height(6.dp).clip(CircleShape),
                color = Tones.Playing,
                trackColor = p.c800,
            )
        }

        if (open) {
            SessionForm(ui, vm, onDone = { open = false })
        }

        if (ui.sessions.isNotEmpty()) {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                ui.sessions.forEach { session ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Text(Format.minutes(session.minutes), style = MaterialTheme.typography.labelMedium, color = p.c200)
                        Text(Format.playedOn(session.playedOn), style = MaterialTheme.typography.labelSmall, color = p.c500)
                        Text(
                            session.note,
                            style = MaterialTheme.typography.labelSmall,
                            color = p.c500,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        IconButton(onClick = { vm.deleteSession(session.id) }, modifier = Modifier.size(28.dp)) {
                            Icon(Icons.Filled.Delete, contentDescription = "Delete session", tint = p.c600, modifier = Modifier.size(14.dp))
                        }
                    }
                }
            }
        }
    }
}

private val SESSION_PRESETS = listOf(15, 30, 45, 60, 90, 120, 180)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SessionForm(ui: GameDetailUiState, vm: GameDetailViewModel, onDone: () -> Unit) {
    val p = Backhog.palette
    var minutes by rememberSaveable { mutableStateOf(60) }
    var playedOn by rememberSaveable { mutableStateOf(Format.today()) }
    var note by rememberSaveable { mutableStateOf("") }
    var pickDate by remember { mutableStateOf(false) }
    val today = Format.today()

    Column(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.small)
            .background(p.c850)
            .padding(10.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text("How long?", style = MaterialTheme.typography.labelMedium, color = p.c400)
        Row(
            Modifier.horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            SESSION_PRESETS.forEach { preset ->
                val selected = minutes == preset
                Text(
                    Format.minutes(preset),
                    style = MaterialTheme.typography.labelMedium,
                    color = if (selected) p.hlInk else p.c300,
                    modifier = Modifier
                        .clip(MaterialTheme.shapes.extraSmall)
                        .background(if (selected) p.hlMid else p.c800)
                        .clickable { minutes = preset }
                        .padding(horizontal = 10.dp, vertical = 6.dp),
                )
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                value = if (minutes == 0) "" else minutes.toString(),
                onValueChange = { text -> minutes = text.toIntOrNull()?.coerceIn(0, 1440) ?: 0 },
                label = { Text("Minutes") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number, imeAction = ImeAction.Next),
                modifier = Modifier.width(120.dp),
            )
        }

        OutlinedTextField(
            value = playedOn,
            onValueChange = { playedOn = it },
            readOnly = true,
            label = { Text("When?") },
            trailingIcon = {
                IconButton(onClick = { pickDate = true }) {
                    Icon(Icons.Filled.CalendarMonth, contentDescription = "Pick date", tint = p.c400)
                }
            },
            singleLine = true,
            modifier = Modifier.fillMaxWidth().clickable { pickDate = true },
        )

        OutlinedTextField(
            value = note,
            onValueChange = { note = it },
            label = { Text("Where did you get to? (optional)") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.align(Alignment.End)) {
            TextButton(onClick = onDone) { Text("Cancel") }
            Button(
                onClick = {
                    vm.addSession(minutes, playedOn.takeIf { it.isNotBlank() }, note)
                    onDone()
                },
                enabled = minutes in 1..1440 && !ui.busy,
            ) { Text("Log session") }
        }
    }

    if (pickDate) {
        val state = rememberStateFor(playedOn)
        DatePickerDialog(
            onDismissRequest = { pickDate = false },
            confirmButton = {
                TextButton(onClick = {
                    pickDate = false
                    state.selectedDateMillis?.let { millis ->
                        // The web's date input caps at today; same rule here.
                        val capped = minOf(millis, System.currentTimeMillis())
                        playedOn = Instant.ofEpochMilli(capped).atZone(ZoneId.of("UTC")).toLocalDate().toString()
                    }
                }) { Text("OK") }
            },
            dismissButton = { TextButton(onClick = { pickDate = false }) { Text("Cancel") } },
        ) {
            DatePicker(state = state)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun rememberStateFor(isoDate: String): androidx.compose.material3.DatePickerState {
    val initial = remember(isoDate) {
        runCatching { LocalDate.parse(isoDate).atStartOfDay(ZoneId.of("UTC")).toInstant().toEpochMilli() }
            .getOrDefault(System.currentTimeMillis())
    }
    return androidx.compose.material3.rememberDatePickerState(initialSelectedDateMillis = initial)
}

/** 1–10, numbered buttons like the web's RatingPicker; tapping the active score clears it. */
@Composable
private fun RatingPanel(entry: Entry, vm: GameDetailViewModel) {
    val p = Backhog.palette
    Panel {
        Text("Your rating", style = MaterialTheme.typography.titleSmall, color = p.c200)
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            (1..10).forEach { score ->
                val active = score <= (entry.userRating ?: 0)
                Box(
                    Modifier
                        .weight(1f)
                        .aspectRatio(1f)
                        .clip(MaterialTheme.shapes.extraSmall)
                        .background(if (active) Tones.Wishlist.copy(alpha = 0.9f) else p.c800)
                        .clickable {
                            vm.patch {
                                rating(if (entry.userRating == score) null else score)
                            }
                        },
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "$score",
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = if (active) Color(0xFF141019) else p.c600,
                    )
                }
            }
        }
        Text(
            if (entry.userRating != null) "You rated this ${entry.userRating}/10 — tap again to clear" else "Not rated yet",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )
    }
}

@Composable
private fun NotesPanel(ui: GameDetailUiState, vm: GameDetailViewModel) {
    val p = Backhog.palette
    Panel {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Notes", style = MaterialTheme.typography.titleSmall, color = p.c200, modifier = Modifier.weight(1f))
            if (ui.notesDirty) {
                Button(onClick = vm::saveNotes, enabled = !ui.busy) { Text("Save") }
            }
        }
        OutlinedTextField(
            value = ui.notesDraft ?: "",
            onValueChange = vm::setNotesDraft,
            placeholder = { Text("Where you left off, why you bounced off it, what to do next…") },
            modifier = Modifier.fillMaxWidth(),
            minLines = 3,
        )
    }
}

// --- the dossier ------------------------------------------------------------------

@Composable
private fun AboutPanel(game: Game) {
    val extras = game.extras
    if (game.summary.isBlank() && extras?.storyline.isNullOrBlank()) return
    val p = Backhog.palette
    Panel {
        Text("About", style = MaterialTheme.typography.titleSmall, color = p.c200)
        if (game.summary.isNotBlank()) {
            Text(game.summary, style = MaterialTheme.typography.bodySmall, color = p.c400)
        }
        val storyline = extras?.storyline
        if (!storyline.isNullOrBlank() && storyline != game.summary) {
            Text(storyline, style = MaterialTheme.typography.bodySmall, color = p.c400)
        }
    }
}

/** The at-a-glance facts table; renders nothing when there's nothing to show. */
@Composable
private fun FactsPanel(game: Game, extras: GameExtras?) {
    val p: Palette = Backhog.palette
    val rows = buildList {
        add("Platforms" to game.platforms.map { it.name })
        add("Publisher" to listOfNotNull(extras?.publisher?.takeIf { it.isNotBlank() }))
        add("Modes" to (extras?.gameModes ?: emptyList()))
        add("Perspective" to (extras?.playerPerspectives ?: emptyList()))
        add("Themes" to (extras?.themes ?: emptyList()))
        add("Franchise" to listOfNotNull(extras?.franchise?.takeIf { it.isNotBlank() }))
        add("Collection" to listOfNotNull(extras?.collection?.takeIf { it.isNotBlank() }))
        add("Age rating" to (extras?.ageRatings ?: emptyList()))
        add("Also known as" to (extras?.alternativeNames ?: emptyList()))
        add("Type" to listOfNotNull(extras?.category?.takeIf { it.isNotBlank() }))
    }.filter { it.second.isNotEmpty() }
    if (rows.isEmpty()) return

    Panel {
        Text("Details", style = MaterialTheme.typography.titleSmall, color = p.c200)
        rows.forEach { (label, items) ->
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(
                    label,
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c500,
                    modifier = Modifier.width(96.dp),
                )
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp), modifier = Modifier.weight(1f)) {
                    // A wrapping chip run per fact row.
                    items.forEach { item ->
                        Text(
                            item,
                            style = MaterialTheme.typography.labelSmall,
                            color = p.c300,
                            modifier = Modifier
                                .clip(MaterialTheme.shapes.extraSmall)
                                .background(p.fillActive)
                                .padding(horizontal = 8.dp, vertical = 3.dp),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ScreenshotsPanel(extras: GameExtras) {
    val p = Backhog.palette
    val context = LocalContext.current
    Panel {
        Text("Screenshots", style = MaterialTheme.typography.titleSmall, color = p.c200)
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            extras.screenshotImageIds.chunked(2).forEach { rowIds ->
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    rowIds.forEach { id ->
                        AsyncImage(
                            model = Format.screenshotThumb(id),
                            contentDescription = "Screenshot",
                            contentScale = ContentScale.Crop,
                            modifier = Modifier
                                .weight(1f)
                                .aspectRatio(16f / 9f)
                                .clip(MaterialTheme.shapes.small)
                                .border(1.dp, p.edge, MaterialTheme.shapes.small)
                                .clickable {
                                    runCatching {
                                        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(Format.screenshotFull(id))))
                                    }
                                },
                        )
                    }
                    if (rowIds.size == 1) Spacer(Modifier.weight(1f))
                }
            }
        }
    }
}

@Composable
private fun VideosPanel(extras: GameExtras) {
    val p = Backhog.palette
    val context = LocalContext.current
    Panel {
        Text("Videos", style = MaterialTheme.typography.titleSmall, color = p.c200)
        extras.videos.forEach { video ->
            Row(
                Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .clickable {
                        runCatching {
                            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("https://www.youtube.com/watch?v=${video.videoId}")))
                        }
                    }
                    .padding(vertical = 8.dp, horizontal = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = p.c500, modifier = Modifier.size(16.dp))
                Text(video.name, style = MaterialTheme.typography.bodySmall, color = p.c300, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                Icon(Icons.Filled.OpenInNew, contentDescription = null, tint = p.c600, modifier = Modifier.size(13.dp))
            }
        }
    }
}

/** A horizontally scrolling row of related-game covers; display-only, like the web. */
@Composable
private fun RelatedPanel(title: String, games: List<RelatedGame>) {
    val p = Backhog.palette
    Panel {
        Text(title, style = MaterialTheme.typography.titleSmall, color = p.c200)
        Row(
            Modifier.horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            games.forEach { related ->
                Column(Modifier.width(76.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Box(
                        Modifier
                            .fillMaxWidth()
                            .aspectRatio(3f / 4f)
                            .clip(MaterialTheme.shapes.small)
                            .background(p.c850)
                            .border(1.dp, p.edge, MaterialTheme.shapes.small),
                        contentAlignment = Alignment.Center,
                    ) {
                        if (related.coverImageId != null) {
                            AsyncImage(
                                model = Format.relatedCover(related.coverImageId),
                                contentDescription = related.name,
                                contentScale = ContentScale.Crop,
                                modifier = Modifier.matchParentSize(),
                            )
                        } else {
                            Icon(Icons.Filled.VideogameAsset, contentDescription = null, tint = p.c600, modifier = Modifier.size(20.dp))
                        }
                    }
                    Text(
                        related.name,
                        style = MaterialTheme.typography.labelSmall,
                        color = p.c400,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}

@Composable
private fun TimelinePanel(entry: Entry) {
    val p = Backhog.palette
    Panel {
        Text("Timeline", style = MaterialTheme.typography.titleSmall, color = p.c200)
        listOf(
            "Added" to entry.createdAt,
            "Started" to entry.startedAt,
            "Finished" to entry.finishedAt,
        ).forEach { (label, iso) ->
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(label, style = MaterialTheme.typography.bodySmall, color = p.c500)
                val rel = Format.relativeTime(iso)
                Text(
                    Format.date(iso) + (if (rel.isNotEmpty() && iso != null) "  ·  $rel" else ""),
                    style = MaterialTheme.typography.bodySmall,
                    color = p.c300,
                )
            }
        }
    }
}

@Composable
private fun ListsPanel(names: List<String>) {
    val p = Backhog.palette
    Panel {
        Text("Lists", style = MaterialTheme.typography.titleSmall, color = p.c200)
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            names.forEach { name ->
                ToneChip(name, p.hlMid)
            }
        }
    }
}

@Composable
private fun PlatformPanel(entry: Entry, game: Game, vm: GameDetailViewModel) {
    val p = Backhog.palette
    var open by remember { mutableStateOf(false) }
    val selected = game.platforms.firstOrNull { it.id == entry.platformId }
    Panel {
        Text("Platform", style = MaterialTheme.typography.titleSmall, color = p.c200)
        Text("Which one are you playing it on?", style = MaterialTheme.typography.labelSmall, color = p.c500)
        Box {
            Row(
                Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .border(1.dp, p.edgeStrong, MaterialTheme.shapes.small)
                    .clickable { open = true }
                    .padding(horizontal = 12.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(selected?.name ?: "Not set", style = MaterialTheme.typography.bodyMedium, color = p.c200, modifier = Modifier.weight(1f))
            }
            androidx.compose.material3.DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                androidx.compose.material3.DropdownMenuItem(
                    text = { Text("Not set") },
                    onClick = {
                        open = false
                        vm.patch { platform(null) }
                    },
                )
                game.platforms.forEach { platform ->
                    androidx.compose.material3.DropdownMenuItem(
                        text = { Text(platform.name) },
                        onClick = {
                            open = false
                            vm.patch { platform(platform.id) }
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun LinksPanel(extras: GameExtras) {
    val p = Backhog.palette
    val context = LocalContext.current
    Panel {
        Text("Links", style = MaterialTheme.typography.titleSmall, color = p.c200)
        extras.websites.forEach { site ->
            Row(
                Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .clickable {
                        runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(site.url))) }
                    }
                    .padding(vertical = 8.dp, horizontal = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(Format.websiteLabel(site.url), style = MaterialTheme.typography.bodySmall, color = p.c300, modifier = Modifier.weight(1f))
                Icon(Icons.Filled.OpenInNew, contentDescription = null, tint = p.c600, modifier = Modifier.size(13.dp))
            }
        }
    }
}
