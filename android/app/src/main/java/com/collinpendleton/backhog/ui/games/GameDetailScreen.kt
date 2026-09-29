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
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.filled.Block
import androidx.compose.material.icons.filled.CalendarMonth
import androidx.compose.material.icons.filled.Cancel
import androidx.compose.material.icons.filled.CardGiftcard
import androidx.compose.material.icons.filled.CheckCircle
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
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
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
import androidx.compose.runtime.LaunchedEffect
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
import com.collinpendleton.backhog.ui.components.DetailSection
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.ExpandableText
import com.collinpendleton.backhog.ui.components.FactLine
import com.collinpendleton.backhog.ui.components.RatingBar
import com.collinpendleton.backhog.ui.components.StatusPicker
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

/**
 * One game: the hero, then what you change most — status and platform —
 * then your playtime, rating and notes, then the dossier (about, details,
 * media, related), then the bookkeeping (lists, projects, timeline, links).
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
        GameDetailViewModel(container.session, baseUrl, entryId, container.unlocks)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette

    // Navigating is a side effect: from composition it would fire on every
    // recomposition until the screen left, popping more than this page.
    LaunchedEffect(ui.deleted) { if (ui.deleted) onRemoved() }
    if (ui.deleted) return

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
    val game = entry.game ?: return
    val extras = game.extras
    var confirmDelete by remember { mutableStateOf(false) }

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Hero(game, extras, baseUrl, onBack)

        Column(Modifier.padding(horizontal = 16.dp)) {
            ErrorText(ui.actionError)

            QuickControls(entry, ui.busy, vm)

            SessionPanel(ui, vm)
            RatingPanel(entry, ui, vm)

            AboutPanel(game)
            FactsPanel(game, extras)
            if (extras != null && extras.screenshotImageIds.isNotEmpty()) ScreenshotsPanel(extras)
            if (extras != null && extras.videos.isNotEmpty()) VideosPanel(extras)
            if (extras != null && extras.similarGames.isNotEmpty()) RelatedPanel("Similar games", extras.similarGames)
            if (extras != null && extras.expansions.isNotEmpty()) RelatedPanel("Expansions", extras.expansions)
            if (extras != null && extras.dlcs.isNotEmpty()) RelatedPanel("DLC & add-ons", extras.dlcs)

            ListMembershipPanel(ui, vm)
            if (ui.checklists.isNotEmpty()) ProjectMembershipPanel(ui, vm)
            TimelinePanel(entry)
            if (extras != null && extras.websites.isNotEmpty()) LinksPanel(extras)

            OutlinedButton(
                onClick = { confirmDelete = true },
                enabled = !ui.busy,
                modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
            ) {
                Icon(Icons.Filled.DeleteOutline, contentDescription = null, tint = Tones.Dropped, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(6.dp))
                Text("Remove from library", color = Tones.Dropped)
            }
            Spacer(Modifier.height(32.dp))
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

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Hero(game: Game, extras: GameExtras?, baseUrl: String, onBack: () -> Unit) {
    val p = Backhog.palette

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

        Column {
            IconButton(onClick = onBack, modifier = Modifier.padding(start = 4.dp)) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back", tint = p.c300)
            }

            Row(
                Modifier.padding(horizontal = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                Box(
                    Modifier
                        .width(112.dp)
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

                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(
                        game.name,
                        style = MaterialTheme.typography.headlineSmall,
                        fontWeight = FontWeight.SemiBold,
                        color = p.cMax,
                    )
                    val byline = listOfNotNull(
                        Format.releaseYear(game.firstReleaseDate).takeIf { it.isNotEmpty() },
                        extras?.developer?.takeIf { it.isNotBlank() },
                    ).joinToString(" · ")
                    if (byline.isNotEmpty()) {
                        Text(byline, style = MaterialTheme.typography.bodyMedium, color = p.c300)
                    }
                    // One fact per line: a single " · " run wraps mid-fact on a phone.
                    game.timeToBeatMain?.let { HeroFact("${Format.duration(it)} to beat") }
                    game.timeToBeatComplete?.let { HeroFact("${Format.duration(it)} to 100%") }
                    val scores = listOfNotNull(
                        game.igdbRating?.let { "${Math.round(it)} IGDB" },
                        extras?.aggregatedRating?.let { "${Math.round(it)} critics" },
                    ).joinToString(" · ")
                    if (scores.isNotEmpty()) HeroFact(scores)
                }
            }
            if (game.genres.isNotEmpty()) {
                FlowRow(
                    Modifier.padding(start = 16.dp, end = 16.dp, top = 14.dp),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    game.genres.forEach { genre -> ToneChip(genre.name, p.hlMid) }
                }
            }
            Spacer(Modifier.height(20.dp))
        }
    }
}

@Composable
private fun HeroFact(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = Backhog.palette.c400)
}

// --- the controls you touch most ------------------------------------------------

/**
 * Status and platform, side by side under the hero. "Played" on a
 * platformless game asks which one, like the web.
 */
@Composable
private fun QuickControls(entry: Entry, busy: Boolean, vm: GameDetailViewModel) {
    val game = entry.game ?: return
    var askPlatform by remember { mutableStateOf(false) }
    val p = Backhog.palette

    fun choose(status: EntryStatus) {
        if (status == EntryStatus.Played && entry.platformId == null && game.platforms.isNotEmpty()) {
            askPlatform = true
            return
        }
        vm.patch { status(status) }
    }

    Row(
        Modifier
            .fillMaxWidth()
            .padding(bottom = 20.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        StatusPicker(
            current = entry.status,
            label = { it.gameLabel },
            tone = Tones::forStatus,
            onPick = ::choose,
            enabled = !busy,
            modifier = Modifier.weight(1f),
        )
        if (game.platforms.isNotEmpty()) {
            PlatformPicker(entry, game, busy, vm, Modifier.weight(1f))
        }
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
                                .padding(vertical = 12.dp, horizontal = 8.dp),
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

/** Which system you're playing it on — the same shape as the status button beside it. */
@Composable
private fun PlatformPicker(entry: Entry, game: Game, busy: Boolean, vm: GameDetailViewModel, modifier: Modifier) {
    val p = Backhog.palette
    var open by remember { mutableStateOf(false) }
    val selected = game.platforms.firstOrNull { it.id == entry.platformId }
    val shape = MaterialTheme.shapes.small
    Box(modifier) {
        Row(
            Modifier
                .fillMaxWidth()
                .height(48.dp)
                .clip(shape)
                .background(p.c900)
                .border(1.dp, p.edgeStrong, shape)
                .clickable(enabled = !busy) { open = true }
                .padding(start = 14.dp, end = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                selected?.name ?: "Platform",
                style = MaterialTheme.typography.labelLarge,
                color = if (selected != null) p.c200 else p.c500,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Icon(Icons.Filled.ArrowDropDown, contentDescription = "Change platform", tint = p.c300)
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            DropdownMenuItem(
                text = { Text("Not set") },
                onClick = {
                    open = false
                    vm.patch { platform(null) }
                },
            )
            game.platforms.forEach { platform ->
                DropdownMenuItem(
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

/** "Playtime": the running total vs the estimate, the log form, the history. */
@Composable
private fun SessionPanel(ui: GameDetailUiState, vm: GameDetailViewModel) {
    val entry = ui.entry ?: return
    val game = entry.game ?: return
    val estimate = game.timeToBeatMain
    var open by remember { mutableStateOf(false) }
    val p = Backhog.palette

    DetailSection(
        "Playtime",
        action = if (!open) {
            { TextButton(onClick = { open = true }, enabled = !ui.busy) { Text("Log a session") } }
        } else null,
    ) {
        Text(
            if (ui.totalMinutes > 0) buildString {
                append(Format.minutes(ui.totalMinutes))
                append(" logged")
                estimate?.let { append(" · ~${Format.hours(it / 3600.0)} to beat") }
            } else "Nothing logged yet",
            style = MaterialTheme.typography.bodyMedium,
            color = p.c300,
        )

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
            Column {
                ui.sessions.forEach { session ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Text(
                            Format.minutes(session.minutes),
                            style = MaterialTheme.typography.bodyMedium,
                            color = p.c200,
                            modifier = Modifier.width(64.dp),
                        )
                        Text(Format.playedOn(session.playedOn), style = MaterialTheme.typography.bodySmall, color = p.c500)
                        Text(
                            session.note,
                            style = MaterialTheme.typography.bodySmall,
                            color = p.c500,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        IconButton(onClick = { vm.deleteSession(session.id) }) {
                            Icon(Icons.Filled.DeleteOutline, contentDescription = "Delete session", tint = p.c600, modifier = Modifier.size(18.dp))
                        }
                    }
                }
            }
        }
    }
}

private val SESSION_PRESETS = listOf(15, 30, 45, 60, 90, 120, 180)

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
private fun SessionForm(ui: GameDetailUiState, vm: GameDetailViewModel, onDone: () -> Unit) {
    val p = Backhog.palette
    var minutes by rememberSaveable { mutableStateOf(60) }
    var playedOn by rememberSaveable { mutableStateOf(Format.today()) }
    var note by rememberSaveable { mutableStateOf("") }
    var pickDate by remember { mutableStateOf(false) }

    Column(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .background(p.c900)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("How long?", style = MaterialTheme.typography.labelMedium, color = p.c400)
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            SESSION_PRESETS.forEach { preset ->
                val selected = minutes == preset
                Text(
                    Format.minutes(preset),
                    style = MaterialTheme.typography.labelLarge,
                    color = if (selected) p.hlInk else p.c300,
                    modifier = Modifier
                        .clip(CircleShape)
                        .background(if (selected) p.hlMid else p.c800)
                        .clickable { minutes = preset }
                        .padding(horizontal = 14.dp, vertical = 8.dp),
                )
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                value = if (minutes == 0) "" else minutes.toString(),
                onValueChange = { text -> minutes = text.toIntOrNull()?.coerceIn(0, 1440) ?: 0 },
                label = { Text("Minutes") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number, imeAction = ImeAction.Next),
                modifier = Modifier.width(112.dp),
            )
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
                modifier = Modifier.weight(1f),
            )
        }

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

/** Rating and notes together: your take on the game, in one place. */
@Composable
private fun RatingPanel(entry: Entry, ui: GameDetailUiState, vm: GameDetailViewModel) {
    DetailSection("Your rating") {
        RatingBar(entry.userRating, onPick = { score -> vm.patch { rating(score) } }, enabled = !ui.busy)
        OutlinedTextField(
            value = ui.notesDraft ?: "",
            onValueChange = vm::setNotesDraft,
            label = { Text("Notes") },
            placeholder = { Text("Where you left off, why you bounced off it, what to do next…") },
            modifier = Modifier.fillMaxWidth(),
            minLines = 3,
        )
        if (ui.notesDirty) {
            Button(onClick = vm::saveNotes, enabled = !ui.busy) { Text("Save notes") }
        }
    }
}

// --- the dossier ------------------------------------------------------------------

@Composable
private fun AboutPanel(game: Game) {
    val extras = game.extras
    val storyline = extras?.storyline?.takeIf { it.isNotBlank() && it != game.summary }
    val text = listOfNotNull(game.summary.takeIf { it.isNotBlank() }, storyline).joinToString("\n\n")
    if (text.isEmpty()) return
    DetailSection("About") {
        ExpandableText(text)
    }
}

/** The at-a-glance facts; renders nothing when there's nothing to show. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FactsPanel(game: Game, extras: GameExtras?) {
    val p = Backhog.palette
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

    DetailSection("Details") {
        rows.forEach { (label, items) ->
            // Label above its values: a side column eats a third of a phone's width.
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(label, style = MaterialTheme.typography.labelMedium, color = p.c500)
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    items.forEach { item ->
                        Text(
                            item,
                            style = MaterialTheme.typography.labelMedium,
                            color = p.c300,
                            modifier = Modifier
                                .clip(MaterialTheme.shapes.extraSmall)
                                .background(p.fillActive)
                                .padding(horizontal = 8.dp, vertical = 4.dp),
                        )
                    }
                }
            }
        }
    }
}

/** A swipeable strip, the phone's gallery idiom — a grid of thumbnails is a web page. */
@Composable
private fun ScreenshotsPanel(extras: GameExtras) {
    val p = Backhog.palette
    val context = LocalContext.current
    DetailSection("Screenshots") {
        Row(
            Modifier.horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            extras.screenshotImageIds.forEach { id ->
                AsyncImage(
                    model = Format.screenshotThumb(id),
                    contentDescription = "Screenshot",
                    contentScale = ContentScale.Crop,
                    modifier = Modifier
                        .width(260.dp)
                        .aspectRatio(16f / 9f)
                        .clip(MaterialTheme.shapes.medium)
                        .border(1.dp, p.edge, MaterialTheme.shapes.medium)
                        .clickable {
                            runCatching {
                                context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(Format.screenshotFull(id))))
                            }
                        },
                )
            }
        }
    }
}

@Composable
private fun VideosPanel(extras: GameExtras) {
    val p = Backhog.palette
    val context = LocalContext.current
    DetailSection("Videos") {
        Column {
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
                        .padding(vertical = 12.dp, horizontal = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = p.hlBright, modifier = Modifier.size(20.dp))
                    Text(video.name, style = MaterialTheme.typography.bodyMedium, color = p.c200, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Icon(Icons.Filled.OpenInNew, contentDescription = null, tint = p.c600, modifier = Modifier.size(16.dp))
                }
            }
        }
    }
}

/** A horizontally scrolling row of related-game covers; display-only, like the web. */
@Composable
private fun RelatedPanel(title: String, games: List<RelatedGame>) {
    val p = Backhog.palette
    DetailSection(title) {
        Row(
            Modifier.horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            games.forEach { related ->
                Column(Modifier.width(88.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
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
    DetailSection("Timeline") {
        listOf(
            "Added" to entry.createdAt,
            "Started" to entry.startedAt,
            "Finished" to entry.finishedAt,
        ).forEach { (label, iso) ->
            val rel = Format.relativeTime(iso)
            FactLine(label, Format.date(iso) + (if (rel.isNotEmpty() && iso != null) "  ·  $rel" else ""))
        }
    }
}

/**
 * Manual-list membership, the web's EntryMembership. Smart lists are excluded:
 * their contents are decided by rules, so a checkbox here would be a lie —
 * they render read-only in their own chips below.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ListMembershipPanel(ui: GameDetailUiState, vm: GameDetailViewModel) {
    val p = Backhog.palette
    DetailSection("Lists") {
        val smartNames = ui.listNames.filter { name -> ui.manualLists.none { it.name == name } }
        if (smartNames.isNotEmpty()) {
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                smartNames.forEach { name -> ToneChip(name, p.hlMid) }
            }
        }
        if (ui.manualLists.isEmpty()) {
            Text(
                "No manual lists yet. Create one from the Lists page to group games however you like.",
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
            )
        } else {
            Column {
                ui.manualLists.forEach { list ->
                    val member = list.id in ui.listMembership
                    CheckRow(list.name, "${list.count}", member) { vm.toggleList(list.id, member) }
                }
            }
        }
    }
}

/** Checklist-project membership: goal projects are excluded — their target is not a curated list. */
@Composable
private fun ProjectMembershipPanel(ui: GameDetailUiState, vm: GameDetailViewModel) {
    val p = Backhog.palette
    DetailSection("Projects") {
        Text("Working on something? Check it in.", style = MaterialTheme.typography.bodySmall, color = p.c500)
        Column {
            ui.checklists.forEach { project ->
                val member = project.id in ui.projectMembership
                CheckRow(
                    project.name,
                    "${project.progress.completedCount}/${project.progress.targetCount}",
                    member,
                ) { vm.toggleProject(project.id, member) }
            }
        }
    }
}

@Composable
private fun CheckRow(title: String, trailing: String, checked: Boolean, onToggle: () -> Unit) {
    val p = Backhog.palette
    Row(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.small)
            .clickable(onClick = onToggle),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        androidx.compose.material3.Checkbox(checked = checked, onCheckedChange = { onToggle() })
        Text(
            title,
            style = MaterialTheme.typography.bodyMedium,
            color = p.c200,
            modifier = Modifier.weight(1f),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(trailing, style = MaterialTheme.typography.labelSmall, color = p.c600, modifier = Modifier.padding(end = 4.dp))
    }
}

@Composable
private fun LinksPanel(extras: GameExtras) {
    val p = Backhog.palette
    val context = LocalContext.current
    DetailSection("Links") {
        Column {
            extras.websites.forEach { site ->
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clip(MaterialTheme.shapes.small)
                        .clickable {
                            runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(site.url))) }
                        }
                        .padding(vertical = 12.dp, horizontal = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    // The server's own label ("Steam", "Wiki", …) when it sent one.
                    Text(
                        site.category?.takeIf { it.isNotBlank() } ?: Format.websiteLabel(site.url),
                        style = MaterialTheme.typography.bodyMedium,
                        color = p.c200,
                        modifier = Modifier.weight(1f),
                    )
                    Icon(Icons.Filled.OpenInNew, contentDescription = null, tint = p.c600, modifier = Modifier.size(16.dp))
                }
            }
        }
    }
}
