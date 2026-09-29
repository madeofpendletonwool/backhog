package com.collinpendleton.backhog.ui.home

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.MenuBook
import androidx.compose.material.icons.filled.Casino
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Headphones
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.SportsEsports
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.ReadingNowBook
import com.collinpendleton.backhog.api.User
import com.collinpendleton.backhog.books.byline
import com.collinpendleton.backhog.ui.books.BookCover
import com.collinpendleton.backhog.ui.books.ProgressBar
import com.collinpendleton.backhog.ui.games.CoverImage
import com.collinpendleton.backhog.ui.games.TonightSheet
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import java.time.LocalTime

/**
 * The front door: what you're in the middle of, both arenas at once. Every
 * row is one tap from carrying on — a book's page, a game's page, or the
 * tape itself — and the arenas' own tabs are for everything else.
 */
@Composable
fun HomeScreen(
    container: AppContainer,
    baseUrl: String,
    user: User,
    listeningEntryId: String?,
    onOpenBook: (String) -> Unit,
    onOpenGame: (String) -> Unit,
    onListen: (String) -> Unit,
    onOpenBooks: () -> Unit,
    onOpenGames: () -> Unit,
    onOpenQueue: () -> Unit,
) {
    val vm: HomeViewModel = viewModel(key = "home|$baseUrl", factory = HomeViewModel.Factory(container, baseUrl))
    val state by vm.state.collectAsState()
    val p = Backhog.palette
    var tonightOpen by rememberSaveable { mutableStateOf(false) }

    // Anything changed in a book or a game shows the moment we come back; the
    // first resume is the initial load, already under way.
    var resumed by remember { mutableStateOf(false) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        if (resumed) vm.reload() else resumed = true
    }

    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(bottom = 24.dp),
    ) {
        item { Greeting(user) }

        if (state.loading && state.reading.isEmpty() && state.playing.isEmpty()) {
            item {
                Box(Modifier.fillMaxWidth().padding(48.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
            }
            return@LazyColumn
        }
        if (state.offline) {
            item {
                Text(
                    "Couldn't reach the server — showing nothing rather than something stale.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }
        }

        item { StatStrip(state) }

        // --- books -----------------------------------------------------------
        item { RowHeader("Continue reading", onMore = onOpenBooks) }
        item {
            if (state.reading.isEmpty()) {
                EmptyRow(
                    Icons.AutoMirrored.Filled.MenuBook,
                    "Nothing on the go. Mark a book as Reading and it lands here.",
                    onOpenBooks,
                )
            } else {
                LazyRow(
                    contentPadding = PaddingValues(horizontal = 16.dp),
                    horizontalArrangement = Arrangement.spacedBy(14.dp),
                ) {
                    items(state.reading, key = { it.entry.id }) { book ->
                        ReadingCard(
                            book,
                            baseUrl,
                            listening = book.entry.id == listeningEntryId,
                            onOpen = { onOpenBook(book.entry.id) },
                            onListen = { onListen(book.entry.id) },
                        )
                    }
                }
            }
        }

        // --- games -----------------------------------------------------------
        item { RowHeader("Playing now", onMore = onOpenGames) }
        item {
            if (state.playing.isEmpty()) {
                EmptyRow(
                    Icons.Filled.SportsEsports,
                    "Nothing in progress. Start something from the backlog.",
                    onOpenGames,
                )
            } else {
                LazyRow(
                    contentPadding = PaddingValues(horizontal = 16.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    items(state.playing, key = { it.id }) { entry ->
                        PlayingCard(entry, baseUrl) { onOpenGame(entry.id) }
                    }
                }
            }
        }

        item { TonightCard { tonightOpen = true } }

        if (state.upNext.isNotEmpty()) {
            item { RowHeader("Up next", onMore = onOpenQueue, moreLabel = "Queue") }
            items(state.upNext, key = { "next-" + it.id }) { entry ->
                UpNextRow(entry, baseUrl) { onOpenGame(entry.id) }
            }
        }
    }

    if (tonightOpen) {
        TonightSheet(container, baseUrl, onDismiss = { tonightOpen = false }, onOpenGame = onOpenGame)
    }
}

@Composable
private fun Greeting(user: User) {
    val p = Backhog.palette
    val hour = remember { LocalTime.now().hour }
    val part = when (hour) {
        in 5..11 -> "Good morning"
        in 12..16 -> "Good afternoon"
        in 17..21 -> "Good evening"
        else -> "Up late"
    }
    Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 20.dp, bottom = 16.dp)) {
        Text(
            "$part, ${user.username}",
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.SemiBold,
            color = p.c100,
        )
    }
}

/** The two numbers that matter: how much is waiting, in each arena. */
@Composable
private fun StatStrip(state: HomeState) {
    val games = state.gameStats
    val books = state.bookStats
    if (games == null && books == null) return
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .padding(bottom = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        games?.let {
            StatTile(
                "${it.backlog}",
                "games in the backlog",
                it.backlogHours.takeIf { h -> h > 0 }?.let { h -> "${Format.hours(h)} to clear" },
                Modifier.weight(1f),
            )
        }
        books?.let {
            StatTile(
                "${it.backlog}",
                "books to read",
                "${it.read} read so far",
                Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun StatTile(value: String, label: String, detail: String?, modifier: Modifier) {
    val p = Backhog.palette
    Column(
        modifier
            .clip(MaterialTheme.shapes.medium)
            .background(p.c900)
            .border(1.dp, p.edge, MaterialTheme.shapes.medium)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(value, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.SemiBold, color = p.cMax)
        Text(label, style = MaterialTheme.typography.bodySmall, color = p.c300)
        detail?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = p.c500) }
    }
}

@Composable
private fun RowHeader(title: String, onMore: () -> Unit, moreLabel: String = "See all") {
    val p = Backhog.palette
    Row(
        Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 4.dp, top = 20.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium, color = p.c100, modifier = Modifier.weight(1f))
        TextButton(onClick = onMore) {
            Text(moreLabel)
            Icon(Icons.Filled.ChevronRight, contentDescription = null, modifier = Modifier.size(18.dp))
        }
    }
}

@Composable
private fun EmptyRow(icon: androidx.compose.ui.graphics.vector.ImageVector, text: String, onClick: () -> Unit) {
    val p = Backhog.palette
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(MaterialTheme.shapes.medium)
            .background(p.c900)
            .clickable(onClick = onClick)
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(icon, contentDescription = null, tint = p.c500)
        Text(text, style = MaterialTheme.typography.bodyMedium, color = p.c400)
    }
}

/**
 * A book in progress: the cover carries the progress bar along its foot, and
 * a book with a recording gets a play button that resumes the tape directly.
 */
@Composable
private fun ReadingCard(
    book: ReadingNowBook,
    baseUrl: String,
    listening: Boolean,
    onOpen: () -> Unit,
    onListen: () -> Unit,
) {
    val p = Backhog.palette
    val brief = book.entry.book ?: return
    Column(
        Modifier
            .width(128.dp)
            .clip(MaterialTheme.shapes.small)
            .clickable(onClick = onOpen),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Box {
            BookCover(
                brief.title,
                brief.coverUrl,
                brief.accentHex,
                baseUrl,
                brief.id,
                Modifier.fillMaxWidth().aspectRatio(2f / 3f),
            )
            if (book.audio) {
                Box(
                    Modifier
                        .align(Alignment.BottomEnd)
                        .padding(6.dp)
                        .size(40.dp)
                        .clip(CircleShape)
                        .background(if (listening) p.hlBright else Color.Black.copy(alpha = 0.7f))
                        .clickable(onClick = onListen),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        if (listening) Icons.Filled.Headphones else Icons.Filled.PlayArrow,
                        contentDescription = if (listening) "Now playing" else "Resume listening",
                        tint = if (listening) p.hlInk else Color.White,
                        modifier = Modifier.size(22.dp),
                    )
                }
            }
        }
        ProgressBar((book.percent / 100.0).toFloat())
        Text(
            brief.title,
            style = MaterialTheme.typography.labelLarge,
            color = p.c100,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            buildString {
                append("${Math.round(book.percent)}%")
                book.remainingHours?.takeIf { it > 0 }?.let { append(" · ${Format.hours(it)} left") }
            },
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
            maxLines = 1,
        )
    }
}

/** A game in progress: the cover, the time sunk, and how far that is toward beating it. */
@Composable
private fun PlayingCard(entry: Entry, baseUrl: String, onOpen: () -> Unit) {
    val p = Backhog.palette
    val game = entry.game ?: return
    val estimate = game.timeToBeatMain
    Column(
        Modifier
            .width(136.dp)
            .clip(MaterialTheme.shapes.small)
            .clickable(onClick = onOpen),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        CoverImage(
            entry,
            baseUrl,
            Modifier
                .fillMaxWidth()
                .aspectRatio(3f / 4f)
                .clip(MaterialTheme.shapes.small),
        )
        if (entry.loggedMinutes > 0 && estimate != null && estimate > 0) {
            ProgressBar((entry.loggedMinutes * 60f / estimate).coerceIn(0f, 1f), fill = Tones.Playing)
        }
        Text(
            game.name,
            style = MaterialTheme.typography.labelLarge,
            color = p.c100,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            when {
                entry.loggedMinutes > 0 && estimate != null ->
                    "${Format.minutes(entry.loggedMinutes)} of ~${Format.duration(estimate)}"
                entry.loggedMinutes > 0 -> "${Format.minutes(entry.loggedMinutes)} played"
                estimate != null -> "~${Format.duration(estimate)} to beat"
                else -> "Just started"
            },
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
            maxLines = 1,
        )
    }
}

@Composable
private fun TonightCard(onClick: () -> Unit) {
    val p = Backhog.palette
    Row(
        Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 16.dp, top = 24.dp)
            .clip(MaterialTheme.shapes.medium)
            .background(p.hlMid.copy(alpha = 0.16f))
            .border(1.dp, p.hlMid.copy(alpha = 0.4f), MaterialTheme.shapes.medium)
            .clickable(onClick = onClick)
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Icon(Icons.Filled.Casino, contentDescription = null, tint = p.hlBright, modifier = Modifier.size(28.dp))
        Column(Modifier.weight(1f)) {
            Text("What should I play tonight?", style = MaterialTheme.typography.titleSmall, color = p.c100)
            Text("Roll for something that fits the evening you've got.", style = MaterialTheme.typography.bodySmall, color = p.c400)
        }
        Icon(Icons.Filled.ChevronRight, contentDescription = null, tint = p.c400)
    }
}

@Composable
private fun UpNextRow(entry: Entry, baseUrl: String, onOpen: () -> Unit) {
    val p = Backhog.palette
    val game = entry.game ?: return
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onOpen)
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        CoverImage(entry, baseUrl, Modifier.size(width = 42.dp, height = 56.dp).clip(MaterialTheme.shapes.extraSmall))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(game.name, style = MaterialTheme.typography.bodyLarge, color = p.c100, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val meta = listOfNotNull(
                Format.releaseYear(game.firstReleaseDate).takeIf { it.isNotEmpty() },
                game.timeToBeatMain?.let { "~${Format.duration(it)}" },
            ).joinToString(" · ")
            if (meta.isNotEmpty()) Text(meta, style = MaterialTheme.typography.bodySmall, color = p.c500)
        }
        Icon(Icons.Filled.ChevronRight, contentDescription = null, tint = p.c600)
    }
}
