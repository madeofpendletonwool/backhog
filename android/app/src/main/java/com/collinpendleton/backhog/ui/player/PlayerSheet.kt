package com.collinpendleton.backhog.ui.player

import androidx.activity.compose.BackHandler
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
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.FastForward
import androidx.compose.material.icons.filled.FastRewind
import androidx.compose.material.icons.filled.Headphones
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.MenuBook
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.books.explainPage
import com.collinpendleton.backhog.books.formatPage
import com.collinpendleton.backhog.books.formatTimecode
import com.collinpendleton.backhog.player.PlayerContract
import com.collinpendleton.backhog.player.PlayerUiState
import com.collinpendleton.backhog.player.RATES
import com.collinpendleton.backhog.player.SLEEP_MINUTES
import com.collinpendleton.backhog.ui.books.ProgressBar
import com.collinpendleton.backhog.ui.books.SectionLabel
import com.collinpendleton.backhog.ui.books.accentColor
import com.collinpendleton.backhog.ui.theme.Backhog
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.math.abs
import kotlin.math.floor

/**
 * The persistent player: a thin bar above the navigation, and the full-screen
 * "now playing" it raises. The port of the web's `AudioPlayer` + `FullPlayer`
 * — a transport, an identity, and the whole book as one seek bar; everything
 * else (skips, speed, sleep, chapters, the way back to the text) lives in
 * the full view, which renders over everything and drops away with Back.
 */
@Composable
fun MiniPlayer(
    state: PlayerUiState,
    onToggle: () -> Unit,
    onSkipBack: () -> Unit,
    onSkipForward: () -> Unit,
    onExpand: () -> Unit,
    onClose: () -> Unit,
) {
    val p = Backhog.palette
    if (!state.hasBook) return
    val accent = accentColor(state.accentHex)
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 10.dp, vertical = 4.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(p.c900)
            .border(1.dp, p.edgeStrong, RoundedCornerShape(12.dp))
            .padding(horizontal = 10.dp, vertical = 8.dp),
    ) {
        state.error?.let { message ->
            Text(
                message,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.labelSmall,
                modifier = Modifier.padding(bottom = 4.dp),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            // The identity block is the expand gesture, and it owns the dead
            // space between the title and the transport.
            Row(
                Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(8.dp))
                    .clickable(onClick = onExpand)
                    .padding(vertical = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                MiniCover(state, accent)
                Column(Modifier.weight(1f)) {
                    Text(
                        state.title,
                        style = MaterialTheme.typography.bodyMedium,
                        color = p.cMax,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        state.authors.ifEmpty { "Audiobook" },
                        style = MaterialTheme.typography.labelSmall,
                        color = p.c400,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            IconButton(onClick = onSkipBack, modifier = Modifier.size(38.dp)) {
                Icon(Icons.Filled.FastRewind, "Back 30 seconds", tint = p.c300)
            }
            IconButton(onClick = onToggle, modifier = Modifier.size(42.dp)) {
                if (state.buffering && !state.isPlaying) {
                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp, color = p.hlBright)
                } else {
                    Icon(
                        if (state.isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                        if (state.isPlaying) "Pause" else "Play",
                        tint = p.cMax,
                        modifier = Modifier.size(28.dp),
                    )
                }
            }
            IconButton(onClick = onSkipForward, modifier = Modifier.size(38.dp)) {
                Icon(Icons.Filled.FastForward, "Forward 30 seconds", tint = p.c300)
            }
            IconButton(onClick = onExpand, modifier = Modifier.size(34.dp)) {
                Icon(Icons.Filled.ExpandLess, "Open full player", tint = p.c300)
            }
            IconButton(onClick = onClose, modifier = Modifier.size(34.dp)) {
                Icon(Icons.Filled.Close, "Close player", tint = p.c300)
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(formatTimecode(state.globalSeconds), style = MaterialTheme.typography.labelSmall, color = p.c400)
            ProgressBar(
                percent = if (state.durationMs > 0) state.positionMs.toFloat() / state.durationMs else 0f,
                modifier = Modifier.weight(1f),
                fill = accent,
            )
            Text(remaining(state), style = MaterialTheme.typography.labelSmall, color = p.c400)
        }
    }
}

@Composable
private fun MiniCover(state: PlayerUiState, accent: Color) {
    val p = Backhog.palette
    Box(
        Modifier
            .size(40.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(accent.copy(alpha = 0.18f))
            .border(1.dp, p.edgeStrong, RoundedCornerShape(8.dp)),
        contentAlignment = Alignment.Center,
    ) {
        if (state.coverUrl.isNotEmpty()) {
            AsyncImage(
                model = state.coverUrl,
                contentDescription = state.title,
                contentScale = ContentScale.Crop,
                modifier = Modifier.size(40.dp),
            )
        } else {
            Icon(Icons.Filled.Headphones, contentDescription = null, tint = accent)
        }
    }
}

private fun remaining(state: PlayerUiState): String =
    if (state.durationMs > 0) "−${formatTimecode((state.durationMs - state.positionMs) / 1000.0)}" else "—"

/* --------------------------------------------------------- full player */

/**
 * "Now playing": the bar opened out over the whole app — the jacket the size
 * it deserves, the skips and speed and sleep side by side, and the handoff
 * strip that says where the tape is on paper and offers the way back to the
 * text.
 */
@Composable
fun FullPlayer(
    container: AppContainer,
    baseUrl: String,
    state: PlayerUiState,
    onToggle: () -> Unit,
    onSeek: (Long) -> Unit,
    onSkip: (Int) -> Unit,
    onNextTrack: () -> Unit,
    onPreviousTrack: () -> Unit,
    onSetRate: (Float) -> Unit,
    onSetSleep: (String, Int) -> Unit,
    onReload: () -> Unit,
    onClose: () -> Unit,
    onOpenBook: (String) -> Unit,
    onContinueReading: (entryId: String, charOffset: Long) -> Unit,
) {
    val p = Backhog.palette
    val accent = accentColor(state.accentHex)
    BackHandler(onBack = onClose)

    Column(
        Modifier
            .fillMaxSize()
            .background(p.c950)
            .statusBarsPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp)
            .padding(bottom = 32.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onClose) {
                Icon(Icons.Filled.KeyboardArrowDown, "Close full player", tint = p.c300)
            }
            Text(
                "Now playing",
                style = MaterialTheme.typography.labelMedium,
                color = p.c500,
                textAlign = TextAlign.Center,
                modifier = Modifier.weight(1f),
            )
            TextButtonLike("Book") { state.entryId?.let(onOpenBook) }
        }

        state.error?.let { message ->
            Row(
                Modifier
                    .fillMaxWidth()
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    message,
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.weight(1f),
                )
                TextButtonLike("Try again", accent = accent, onClick = onReload)
            }
        }

        // Full width, or "centred" only centres within its own content and the
        // whole block hugs the left edge.
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 16.dp, bottom = 20.dp),
        ) {
            Box(
                Modifier
                    .fillMaxWidth(0.72f)
                    .widthIn(max = 320.dp)
                    .aspectRatio(1f)
                    .clip(RoundedCornerShape(16.dp))
                    .background(accent.copy(alpha = 0.18f))
                    .border(1.dp, p.edgeStrong, RoundedCornerShape(16.dp)),
                contentAlignment = Alignment.Center,
            ) {
                if (state.coverUrl.isNotEmpty()) {
                    AsyncImage(
                        model = state.coverUrl,
                        contentDescription = state.title,
                        contentScale = ContentScale.Crop,
                        modifier = Modifier.fillMaxSize(),
                    )
                } else {
                    Icon(Icons.Filled.Headphones, contentDescription = null, tint = accent, modifier = Modifier.size(56.dp))
                }
            }
            Text(
                state.title,
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold,
                color = p.cMax,
                textAlign = TextAlign.Center,
            )
            Text(
                state.authors.ifEmpty { "Unknown author" },
                style = MaterialTheme.typography.bodyMedium,
                color = p.c300,
                textAlign = TextAlign.Center,
            )
            state.currentTrack?.let { track ->
                Text(
                    "${track.trackNumber}. ${track.title}",
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c500,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        SeekRow(state, accent, onSeek)
        TransportRow(state, onToggle, onSkip, onNextTrack, onPreviousTrack)
        Spacer(Modifier.height(24.dp))
        SectionLabel("Speed")
        Row(
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState())
                .padding(top = 6.dp),
        ) {
            RATES.forEach { rate ->
                Chip(
                    label = rateLabel(rate),
                    active = abs(state.rate - rate) < 0.01f,
                    accent = accent,
                    onClick = { onSetRate(rate) },
                )
            }
        }

        SleepSection(state, onSetSleep)
        HandoffPanel(container, baseUrl, state, accent, onContinueReading)
        EditionLine(container, baseUrl, state)
        ChaptersSection(state, accent, onSeek)
    }
}

private fun rateLabel(rate: Float): String {
    val whole = rate.toInt()
    return if (rate == whole.toFloat()) "$whole×" else "${rate}×"
}

@Composable
private fun TextButtonLike(label: String, accent: Color? = null, onClick: () -> Unit) {
    val p = Backhog.palette
    Text(
        label,
        style = MaterialTheme.typography.labelMedium,
        color = accent ?: p.c300,
        modifier = Modifier
            .clip(RoundedCornerShape(6.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 8.dp, vertical = 6.dp),
    )
}

/** Elapsed, the bar, and how much book is left — the whole book, not one file. */
@Composable
private fun SeekRow(state: PlayerUiState, accent: Color, onSeek: (Long) -> Unit) {
    val p = Backhog.palette
    var scrub by remember { mutableStateOf<Float?>(null) }
    val seconds = (state.durationMs / 1000f).coerceAtLeast(1f)
    val value = (scrub ?: state.positionMs / 1000f).coerceIn(0f, seconds)
    Column(Modifier.padding(vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(formatTimecode(value.toDouble()), style = MaterialTheme.typography.labelMedium, color = p.c400)
            Slider(
                value = value,
                onValueChange = { scrub = it },
                onValueChangeFinished = {
                    scrub?.let { onSeek((it * 1000).toLong()) }
                    scrub = null
                },
                valueRange = 0f..seconds,
                enabled = state.durationMs > 0,
                colors = SliderDefaults.colors(
                    thumbColor = accent,
                    activeTrackColor = accent,
                    inactiveTrackColor = p.c700,
                ),
                modifier = Modifier.weight(1f),
            )
            Text(remaining(state), style = MaterialTheme.typography.labelMedium, color = p.c400)
        }
        if (state.durationMs > 0) {
            Text(
                "${(state.positionMs * 100 / state.durationMs.coerceAtLeast(1))}%",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
                textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

@Composable
private fun TransportRow(
    state: PlayerUiState,
    onToggle: () -> Unit,
    onSkip: (Int) -> Unit,
    onNextTrack: () -> Unit,
    onPreviousTrack: () -> Unit,
) {
    val p = Backhog.palette
    // Spread across the width: five controls bunched mid-screen read as one
    // blob, and the edges are where a thumb actually rests.
    Row(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp),
        horizontalArrangement = Arrangement.SpaceEvenly,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onPreviousTrack, modifier = Modifier.size(56.dp)) {
            Icon(Icons.Filled.SkipPrevious, "Previous track", tint = p.c200, modifier = Modifier.size(30.dp))
        }
        IconButton(onClick = { onSkip(-30) }, modifier = Modifier.size(56.dp)) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Icon(Icons.Filled.FastRewind, "Back 30 seconds", tint = p.c200, modifier = Modifier.size(28.dp))
                Text("30", style = MaterialTheme.typography.labelSmall, color = p.c500)
            }
        }
        Box(
            Modifier
                .size(76.dp)
                .clip(RoundedCornerShape(26.dp))
                .background(p.hlBright)
                .clickable(onClick = onToggle),
            contentAlignment = Alignment.Center,
        ) {
            if (state.buffering && !state.isPlaying) {
                CircularProgressIndicator(Modifier.size(26.dp), strokeWidth = 2.dp, color = p.hlInk)
            } else {
                Icon(
                    if (state.isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                    if (state.isPlaying) "Pause" else "Play",
                    tint = p.hlInk,
                    modifier = Modifier.size(36.dp),
                )
            }
        }
        IconButton(onClick = { onSkip(30) }, modifier = Modifier.size(56.dp)) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Icon(Icons.Filled.FastForward, "Forward 30 seconds", tint = p.c200, modifier = Modifier.size(28.dp))
                Text("30", style = MaterialTheme.typography.labelSmall, color = p.c500)
            }
        }
        IconButton(onClick = onNextTrack, modifier = Modifier.size(56.dp)) {
            Icon(Icons.Filled.SkipNext, "Next track", tint = p.c200, modifier = Modifier.size(30.dp))
        }
    }
}

@Composable
private fun Chip(label: String, active: Boolean, accent: Color, onClick: () -> Unit) {
    val p = Backhog.palette
    Text(
        label,
        style = MaterialTheme.typography.labelMedium,
        color = if (active) p.c100 else p.c400,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (active) accent.copy(alpha = 0.25f) else p.c850)
            .border(1.dp, if (active) accent else p.edge, RoundedCornerShape(999.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
    )
}

/** The sleep timer. "End of chapter" is the one people actually want. */
@Composable
private fun SleepSection(state: PlayerUiState, onSetSleep: (String, Int) -> Unit) {
    Spacer(Modifier.height(24.dp))
    SectionLabel(
        when (state.sleepKind) {
            PlayerContract.SLEEP_MINUTES -> "Sleep · ${sleepCountdown(state)}"
            PlayerContract.SLEEP_CHAPTER -> "Sleep · at the end of this chapter"
            else -> "Sleep timer"
        },
    )
    Row(
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(top = 6.dp),
    ) {
        Chip(
            label = "Off",
            active = state.sleepKind == PlayerContract.SLEEP_OFF,
            accent = Backhog.palette.hlBright,
            onClick = { onSetSleep(PlayerContract.SLEEP_OFF, 0) },
        )
        SLEEP_MINUTES.forEach { minutes ->
            Chip(
                label = "${minutes}m",
                active = state.sleepKind == PlayerContract.SLEEP_MINUTES && state.sleepMinutes == minutes,
                accent = Backhog.palette.hlBright,
                onClick = { onSetSleep(PlayerContract.SLEEP_MINUTES, minutes) },
            )
        }
        Chip(
            label = "End of chapter",
            active = state.sleepKind == PlayerContract.SLEEP_CHAPTER,
            accent = Backhog.palette.hlBright,
            onClick = { onSetSleep(PlayerContract.SLEEP_CHAPTER, 0) },
        )
    }
}

/** "4:12" and falling — re-derived every second while a duration sleep runs. */
@Composable
private fun sleepCountdown(state: PlayerUiState): String {
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(state.sleepEndsAt) {
        while (now < state.sleepEndsAt) {
            delay(1000)
            now = System.currentTimeMillis()
        }
    }
    return formatTimecode(((state.sleepEndsAt - now) / 1000.0).coerceAtLeast(0.0))
}

/* ------------------------------------------------------- the handoff */

/**
 * Where the tape is in the paper copy, and the way back to the text — the
 * player's half of the read/listen handoff. The paper number is the server's
 * translation, bucketed to the half minute like the web's `PaperPage`: a
 * page that moves once a minute is as useful as one that moves sixty times.
 */
@Composable
private fun HandoffPanel(
    container: AppContainer,
    baseUrl: String,
    state: PlayerUiState,
    accent: Color,
    onContinueReading: (entryId: String, charOffset: Long) -> Unit,
) {
    val entryId = state.entryId ?: return
    val p = Backhog.palette
    val bucket = floor(state.globalSeconds / 30.0) * 30.0
    var paper by remember { mutableStateOf<String?>(null) }
    var paperHint by remember { mutableStateOf<String?>(null) }
    var aligned by remember { mutableStateOf<Boolean?>(null) }

    LaunchedEffect(entryId) {
        aligned = apiCall { container.session.api(baseUrl).bookPosition(entryId) }
            .getOrNull()
            ?.audio
            ?.derived
    }
    LaunchedEffect(entryId, bucket) {
        apiCall { container.session.api(baseUrl).translatePosition(entryId, audio = bucket) }
            .onSuccess { translation ->
                paper = formatPage(translation.page)?.let { "$it in your paper copy" }
                paperHint = explainPage(translation.page)
            }
    }

    Column(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 14.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(p.c900)
            .border(1.dp, p.edgeStrong, RoundedCornerShape(12.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        paper?.let {
            Text(
                it,
                style = MaterialTheme.typography.labelMedium,
                color = p.c300,
            )
            paperHint?.let { hint ->
                Text(hint, style = MaterialTheme.typography.labelSmall, color = p.c500)
            }
        }
        if (aligned == true) {
            ContinueReadingButton(accent) {
                // The reverse handoff: open the reader on the sentence being
                // narrated. The translation is the server's — speculative,
                // nothing stored — and the tape keeps playing through it.
                apiCallTranslate(container, baseUrl, entryId, state.globalSeconds) { charOffset ->
                    if (charOffset != null) onContinueReading(entryId, charOffset)
                }
            }
        } else if (aligned == false) {
            Text(
                "This audiobook isn't aligned to the text yet — run Align on the book's page, and \"continue reading\" opens the reader in the right place.",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
    }
}

@Composable
private fun ContinueReadingButton(accent: Color, onClick: () -> Unit) {
    val p = Backhog.palette
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(accent.copy(alpha = 0.18f))
            .border(1.dp, accent, RoundedCornerShape(10.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(Icons.Filled.MenuBook, contentDescription = null, tint = accent)
        Text(
            "Continue reading",
            style = MaterialTheme.typography.labelLarge,
            color = p.c100,
        )
    }
}

/** Runs the speculative translation off-composition, then hands back the offset. */
private fun apiCallTranslate(
    container: AppContainer,
    baseUrl: String,
    entryId: String,
    globalSeconds: Double,
    then: (Long?) -> Unit,
) {
    container.appScope.launch {
        val offset = apiCall {
            container.session.api(baseUrl).translatePosition(entryId, audio = globalSeconds)
        }.getOrNull()?.charOffset
        then(offset)
    }
}

/** Which recording is designated: "Listening to {edition}". */
@Composable
private fun EditionLine(container: AppContainer, baseUrl: String, state: PlayerUiState) {
    val entryId = state.entryId ?: return
    val p = Backhog.palette
    var edition by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(entryId) {
        edition = apiCall { container.session.api(baseUrl).bookFiles(entryId) }
            .getOrNull()
            ?.audioEditions
            ?.firstOrNull { it.primary }
            ?.label
            ?.takeIf { it.isNotBlank() }
    }
    edition?.let {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Icon(Icons.Filled.Headphones, contentDescription = null, tint = p.c500, modifier = Modifier.size(14.dp))
            Text(
                "Listening to $it",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
    }
}

/* ------------------------------------------------------------ chapters */

/** The running order — the tracks, which are the chapters the tape owns. */
@Composable
private fun ChaptersSection(state: PlayerUiState, accent: Color, onSeek: (Long) -> Unit) {
    val tape = state.tape ?: return
    if (tape.present.isEmpty()) return
    val p = Backhog.palette
    SectionLabel("Chapters · ${tape.present.size}")

    if (state.degraded) {
        Text(
            "At least one file's length could not be read, so every time after it is short by however long that file really runs.",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
            modifier = Modifier.padding(vertical = 4.dp),
        )
    }

    Column(Modifier.padding(top = 6.dp)) {
        tape.present.forEachIndexed { index, track ->
            val active = index == state.trackIndex
            Row(
                Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(8.dp))
                    .background(if (active) p.fillActive else Color.Transparent)
                    .clickable { onSeek(tape.windowPositionMs(track.globalStart)) }
                    .padding(horizontal = 10.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Text(
                    track.trackNumber.toString(),
                    style = MaterialTheme.typography.labelSmall,
                    color = if (active) accent else p.c500,
                    modifier = Modifier.size(width = 24.dp, height = 16.dp),
                    textAlign = TextAlign.End,
                )
                Text(
                    track.title,
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (active) p.c100 else p.c400,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (track.missing) {
                    Text(
                        "OFFLINE",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
                Text(
                    if (track.measured) formatTimecode(track.globalStart) else "—",
                    style = MaterialTheme.typography.labelSmall,
                    color = p.c500,
                )
            }
        }
    }
}
