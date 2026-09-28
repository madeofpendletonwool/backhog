package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.CalendarMonth
import androidx.compose.material.icons.filled.EmojiEvents
import androidx.compose.material.icons.filled.Flag
import androidx.compose.material.icons.filled.HourglassEmpty
import androidx.compose.material.icons.filled.Layers
import androidx.compose.material.icons.filled.Monitor
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Tag
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.Insights
import com.collinpendleton.backhog.api.Superlative
import com.collinpendleton.backhog.data.ServerUrl
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/**
 * "Your Gaming Problem" — the web's DashboardPage: one diagnosis up top, the
 * year's Backlog Challenge card, then the wall of superlatives.
 */
@Composable
fun DashboardScreen(
    container: AppContainer,
    baseUrl: String,
    onBack: () -> Unit,
    onOpenGame: (String) -> Unit,
    onOpenDebt: () -> Unit,
) {
    val vm: DashboardViewModel = viewModel(key = "dashboard|$baseUrl") {
        DashboardViewModel(container.session, baseUrl)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette

    Column(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
        Row(
            Modifier.fillMaxWidth().padding(top = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text("Your Gaming Problem", style = MaterialTheme.typography.headlineSmall, color = p.c100)
                Text("A routine check-up on the pile. The prognosis is never good.", style = MaterialTheme.typography.bodySmall, color = p.c400)
            }
            TextButton(onClick = onOpenDebt) {
                Icon(Icons.Filled.HourglassEmpty, contentDescription = null, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(4.dp))
                Text("Debt")
            }
        }

        when {
            ui.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            ui.error != null -> Column(
                Modifier.fillMaxSize(),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterVertically),
            ) {
                Text(ui.error ?: "", color = Tones.Dropped, style = MaterialTheme.typography.bodyMedium)
                Button(onClick = vm::load) {
                    Icon(Icons.Filled.Refresh, contentDescription = null, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("Try again")
                }
            }
            !ui.hasLibrary -> Column(
                Modifier.fillMaxSize().padding(vertical = 48.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("Nothing to confess yet", style = MaterialTheme.typography.titleMedium, color = p.c100)
                Text(
                    "Add games to your library and the embarrassing numbers will find you on their own.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = p.c400,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
            }
            else -> LazyColumn(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                item { DiagnosisPanel(ui.insights!!) }
                ui.season?.let { season -> item { SeasonPanel(season) } }
                items(ui.insights!!.superlatives.size) { index ->
                    SuperlativeCard(ui.insights!!.superlatives[index], baseUrl, onOpenGame)
                }
                item { Spacer(Modifier.height(20.dp)) }
            }
        }
    }
}

/** The verdict ladder — the web's `verdict()`, colours and copy verbatim. */
private fun verdict(years: Double?, unplayed: Int): Pair<String, Color> = when {
    unplayed == 0 -> "Clean bill of health" to Tones.Played
    years == null -> "No pace on file" to Color(0xFF94A3B8)
    years > 5 -> "Terminal" to Tones.Dropped
    years > 2 -> "Chronic" to Tones.Wishlist
    years > 0.75 -> "Treatable" to Tones.Playing
    else -> "Manageable" to Tones.Played
}

@Composable
private fun DiagnosisPanel(insights: Insights) {
    val p = Backhog.palette
    val headline = insights.headline
    val (label, tone) = verdict(headline.yearsAtCurrentRate, headline.unplayedGames)
    val shape = MaterialTheme.shapes.large

    Column(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, shape)
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                "THE DIAGNOSIS",
                style = MaterialTheme.typography.labelSmall,
                fontWeight = FontWeight.SemiBold,
                color = p.c500,
                modifier = Modifier.weight(1f),
            )
            Text(
                label.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                fontWeight = FontWeight.SemiBold,
                color = tone,
                modifier = Modifier
                    .clip(CircleShape)
                    .background(tone.copy(alpha = 0.12f))
                    .border(1.dp, tone.copy(alpha = 0.3f), CircleShape)
                    .padding(horizontal = 10.dp, vertical = 4.dp),
            )
        }

        val owned = if (headline.gamesOwned == 1) "one game" else "${headline.gamesOwned} games"
        val body = if (headline.unplayedGames == 0) {
            "You own $owned. Nothing is waiting on you. Suspicious, but healthy."
        } else {
            buildString {
                append("You own $owned. ")
                append("${headline.unplayedGames} ${if (headline.unplayedGames == 1) "is" else "are"} still waiting — ")
                append("${Format.hours(headline.hoursRemaining)} of playing")
                append(
                    headline.yearsAtCurrentRate?.let { ", or ${Format.years(it)} at your current pace." }
                        ?: ", and no recent pace to measure it against.",
                )
            }
        }
        Text(body, style = MaterialTheme.typography.bodyLarge, color = p.c300)

        Row(horizontalArrangement = Arrangement.spacedBy(24.dp), modifier = Modifier.padding(top = 4.dp)) {
            Figure("Games owned", "${headline.gamesOwned}")
            Figure("Unplayed", "${headline.unplayedGames}")
            Figure("Hours owed", Format.hours(headline.hoursRemaining))
            Figure("Years at pace", headline.yearsAtCurrentRate?.let { Format.years(it) } ?: "—")
        }
    }
}

@Composable
private fun Figure(label: String, value: String) {
    val p = Backhog.palette
    Column {
        Text(label.uppercase(), style = MaterialTheme.typography.labelSmall, color = p.c500)
        Text(value, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold, color = p.c100)
    }
}

/** The "YYYY Backlog Challenge" card — this year's campaign. */
@Composable
private fun SeasonPanel(season: com.collinpendleton.backhog.api.Season) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    Column(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, shape)
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("THIS YEAR'S CAMPAIGN", style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.SemiBold, color = p.c500)
                Text(
                    "${season.year} Backlog Challenge",
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = p.c100,
                )
            }
            Icon(Icons.Filled.EmojiEvents, contentDescription = null, tint = p.hlBright)
        }
        Row(
            horizontalArrangement = Arrangement.spacedBy(24.dp),
            modifier = Modifier.padding(top = 10.dp),
        ) {
            Figure("Completed", "${season.gamesCompleted}")
            Figure("Hours played", Format.hours(season.hoursPlayed))
            Figure("Franchises", "${season.franchisesCleared}")
            Figure("Rescues", "${season.rescues}")
        }
    }
}

/** Eyebrow + quip per superlative kind — the web's SUPERLATIVE_META. */
private data class SuperlativeMeta(val eyebrow: String, val quip: String, val icon: ImageVector)

private fun superlativeMeta(kind: String): SuperlativeMeta = when (kind) {
    "oldest_untouched" -> SuperlativeMeta("Longest-serving resident", "Bought, shelved, never once opened.", Icons.Filled.HourglassEmpty)
    "longest_unplayed" -> SuperlativeMeta("The biggest one", "The game you keep meaning to start. Someday.", Icons.Filled.Layers)
    "neglected_genre" -> SuperlativeMeta("Genre you keep buying", "You keep buying them. They keep not getting played.", Icons.Filled.Tag)
    "worst_platform" -> SuperlativeMeta("Where the pile lives", "Every platform has a backlog. This one has the backlog.", Icons.Filled.Monitor)
    "neglected_year" -> SuperlativeMeta("Best vintage, least opened", "Collected religiously. Played rarely.", Icons.Filled.CalendarMonth)
    else -> SuperlativeMeta("Superlative", "", Icons.Filled.EmojiEvents)
}

@Composable
private fun SuperlativeCard(superlative: Superlative, baseUrl: String, onOpenGame: (String) -> Unit) {
    val p = Backhog.palette
    val meta = superlativeMeta(superlative.kind)
    val payload = superlative.payload
    val shape = MaterialTheme.shapes.large

    Column(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, shape)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                meta.eyebrow.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                fontWeight = FontWeight.SemiBold,
                color = p.c500,
                modifier = Modifier.weight(1f),
            )
            Icon(meta.icon, contentDescription = null, tint = p.c600, modifier = Modifier.size(18.dp))
        }

        val game = payload.game
        if (game != null) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                modifier = Modifier
                    .clip(MaterialTheme.shapes.medium)
                    .clickable { payload.entryId?.let(onOpenGame) }
                    .padding(vertical = 2.dp),
            ) {
                Box(
                    Modifier
                        .size(width = 56.dp, height = 74.dp)
                        .clip(MaterialTheme.shapes.small)
                        .background((game.accentHexOrNull?.let { parseHex(it) } ?: AccentFallback).copy(alpha = 0.15f)),
                ) {
                    AsyncImage(
                        model = ServerUrl.gameCoverUrl(baseUrl, game.id),
                        contentDescription = game.name,
                        contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                        modifier = Modifier.fillMaxSize(),
                    )
                }
                Column(Modifier.align(Alignment.CenterVertically)) {
                    Text(
                        game.name,
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = p.c100,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(superlative.label, style = MaterialTheme.typography.bodySmall, color = p.hlBright, maxLines = 2)
                }
            }
        } else {
            Text(
                (payload.year?.toString() ?: payload.name ?: "—"),
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold,
                color = p.c100,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(superlative.label, style = MaterialTheme.typography.bodyMedium, color = p.hlBright)
        }

        if (meta.quip.isNotEmpty()) {
            Text(meta.quip, style = MaterialTheme.typography.labelSmall, color = p.c500)
        }
    }
}
