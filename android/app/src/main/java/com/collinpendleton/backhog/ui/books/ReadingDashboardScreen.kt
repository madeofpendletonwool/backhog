package com.collinpendleton.backhog.ui.books

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.BookSuperlative
import com.collinpendleton.backhog.api.ReadingNowBook
import com.collinpendleton.backhog.books.byline
import com.collinpendleton.backhog.books.formatHours
import com.collinpendleton.backhog.ui.components.Panel
import com.collinpendleton.backhog.ui.theme.Backhog
import kotlin.math.roundToInt

/**
 * "Your Reading Problem" — the pile diagnosed, the books in progress, the
 * pace, the year's Reading Season, and the superlatives the backend has
 * already written the copy for.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ReadingDashboardScreen(
    container: AppContainer,
    baseUrl: String,
    onBack: () -> Unit,
    onOpen: (String) -> Unit,
) {
    val vm: ReadingDashboardViewModel = viewModel(factory = ReadingDashboardViewModel.Factory(container, baseUrl))
    val state by vm.state.collectAsState()
    val p = Backhog.palette

    Scaffold(
        containerColor = p.c950,
        topBar = {
            TopAppBar(
                title = { Text("Your Reading Problem") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = p.c950,
                    titleContentColor = p.c100,
                    navigationIconContentColor = p.c300,
                ),
            )
        },
    ) { padding ->
        val loadError = state.error
        when {
            state.loading -> Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                CircularProgressIndicator()
            }
            loadError != null -> Column(
                Modifier.fillMaxSize().padding(padding).padding(24.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(loadError, color = MaterialTheme.colorScheme.error)
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
                Diagnosis(state)
                ContinueRow(state.now?.books ?: emptyList(), baseUrl, onOpen)
                PaceCard(state)
                SeasonCard(state.season)
                Superlatives(state.insights?.superlatives ?: emptyList())
            }
        }
    }
}

/** The one-line verdict over the four numbers that back it. */
@Composable
private fun Diagnosis(state: ReadingDashboardState) {
    val p = Backhog.palette
    val headline = state.insights?.headline ?: return
    Panel {
        val years = headline.yearsAtCurrentRate
        Text(
            when {
                years == null -> "The pace is not measurable yet — log some reading sessions."
                years >= 10.0 -> "You read a page a year of this."
                years >= 3.0 -> "A ${years.roundToInt()}-year problem."
                else -> "Manageable — ${years.roundToInt()} year${if (years.roundToInt() == 1) "" else "s"} at your pace."
            },
            style = MaterialTheme.typography.titleMedium,
            color = p.c100,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
            Figure("Owned", headline.booksOwned.toString())
            Figure("Unread", headline.unreadBooks.toString())
            Figure("Pages owed", formatPageCount(headline.pagesOwed))
            Figure("Hours owed", formatHours(headline.hoursOwed))
        }
    }
}

private fun formatPageCount(pages: Double): String =
    if (pages >= 1000) "%,dk".format((pages / 1000).roundToInt()) else pages.roundToInt().toString()

@Composable
private fun Figure(label: String, value: String) {
    val p = Backhog.palette
    Column {
        Text(value, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, color = p.c200)
        Text(label, style = MaterialTheme.typography.labelSmall, color = p.c500)
    }
}

/** The hero row: what you are in the middle of, most recently read first. */
@Composable
private fun ContinueRow(books: List<ReadingNowBook>, baseUrl: String, onOpen: (String) -> Unit) {
    if (books.isEmpty()) return
    val p = Backhog.palette
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionLabel("In progress")
        LazyRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            items(books, key = { it.entry.id }) { item ->
                Column(
                    Modifier
                        .width(128.dp)
                        .clickable { onOpen(item.entry.id) },
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    val book = item.entry.book
                    if (book != null) {
                        BookCover(book.title, book.coverUrl, book.accentHex, baseUrl, book.id, Modifier.fillMaxWidth().aspectRatio(2f / 3f))
                    }
                    BookCaption(item.entry, compact = true)
                    ProgressBar(item.percent.toFloat() / 100f)
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text("${item.percent.roundToInt()}%", style = MaterialTheme.typography.labelSmall, color = p.c400)
                        item.remainingHours?.let { hours ->
                            Text(
                                "${formatHours(hours)} left${if (item.audio) " on tape" else ""}",
                                style = MaterialTheme.typography.labelSmall,
                                color = p.c600,
                            )
                        }
                    }
                }
            }
        }
    }
}

/** How fast you actually read, and whether that number is measured or default. */
@Composable
private fun PaceCard(state: ReadingDashboardState) {
    val p = Backhog.palette
    val pace = state.insights?.pace ?: return
    Panel {
        SectionLabel("Your pace")
        Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
            Figure("Pages/hour", if (pace.measured) String.format(java.util.Locale.US, "%.0f", pace.pagesPerHour) else "—")
            Figure("Measured from", "${formatHours(pace.sessionHours)} of reading")
            pace.hoursPerWeek90d?.let { Figure("Hours/week (90d)", String.format(java.util.Locale.US, "%.1f", it)) }
        }
        if (!pace.measured) {
            Text(
                "A default until more reading is logged — the projection says so too.",
                style = MaterialTheme.typography.labelSmall,
                color = p.c600,
            )
        }
        state.debt?.projection?.currentPace?.let { scenario ->
            Text(
                if (scenario.clearBy != null) {
                    "At your pace the pile clears ${scenario.clearBy.take(4)}."
                } else {
                    "At your pace the pile never clears — read faster or buy slower."
                },
                style = MaterialTheme.typography.bodySmall,
                color = p.c400,
            )
        }
    }
}

/** The "YYYY Reading Challenge" card. */
@Composable
private fun SeasonCard(season: com.collinpendleton.backhog.api.ReadingSeason?) {
    if (season == null || season.year == 0) return
    val p = Backhog.palette
    Panel {
        SectionLabel("${season.year} Reading Challenge")
        Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
            Figure("Finished", season.booksFinished.toString())
            Figure("Pages", formatPageCount(season.pagesRead))
            Figure("Listened", formatHours(season.hoursListened))
            Figure("Authors cleared", season.authorsCleared.toString())
            Figure("Rescues", season.rescues.toString())
        }
        Text(
            "Rescues are books finished after a year or more on the shelf.",
            style = MaterialTheme.typography.labelSmall,
            color = p.c600,
        )
    }
}

/** The superlatives — the backend wrote the copy, this just sets it. */
@Composable
private fun Superlatives(superlatives: List<BookSuperlative>) {
    if (superlatives.isEmpty()) return
    val p = Backhog.palette
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        SectionLabel("Superlatives")
        superlatives.forEach { superlative ->
            Panel {
                Text(superlative.label, style = MaterialTheme.typography.bodyMedium, color = p.c200)
            }
        }
    }
}
