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
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.SeriesSummary
import com.collinpendleton.backhog.ui.theme.Backhog

/**
 * The series index — the web's SeriesPage: journey cards with completion and
 * hours remaining, and the IGDB backfill trigger with its running banner.
 */
@Composable
fun SeriesScreen(
    container: AppContainer,
    baseUrl: String,
    onBack: () -> Unit,
    onOpenSeries: (String) -> Unit,
) {
    val vm: SeriesViewModel = viewModel(key = "series|$baseUrl") {
        SeriesViewModel(container.session, baseUrl)
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
                Text("Series", style = MaterialTheme.typography.headlineSmall, color = p.c100)
                Text("Franchises and collections, played as journeys instead of rows.", style = MaterialTheme.typography.bodySmall, color = p.c400)
            }
            if (ui.series.isNotEmpty()) {
                IconButton(onClick = vm::kickBackfill, enabled = !ui.backfillRunning && !ui.kicking) {
                    Icon(Icons.Filled.Refresh, contentDescription = "Re-link series from IGDB", tint = p.c300)
                }
            }
        }

        if (ui.backfillRunning) {
            Text(
                "Linking series from IGDB — this runs in the background and fills in as it goes.",
                style = MaterialTheme.typography.bodySmall,
                color = p.hlBright,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .background(p.hlMid.copy(alpha = 0.12f))
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }

        when {
            ui.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            ui.error != null && ui.series.isEmpty() -> Column(
                Modifier.fillMaxSize(),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterVertically),
            ) {
                Text(ui.error ?: "", color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
                Button(onClick = vm::load) {
                    Icon(Icons.Filled.Refresh, contentDescription = null, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("Try again")
                }
            }
            ui.series.isEmpty() -> Column(
                Modifier.fillMaxSize().padding(vertical = 48.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("No series yet", style = MaterialTheme.typography.titleMedium, color = p.c100)
                Text(
                    "Add a few games from the same franchise and they'll group up here automatically.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = p.c400,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
                Button(onClick = vm::kickBackfill, enabled = !ui.backfillRunning && !ui.kicking) {
                    if (ui.kicking || ui.backfillRunning) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    } else {
                        Icon(Icons.Filled.Refresh, contentDescription = null, modifier = Modifier.size(16.dp))
                        Spacer(Modifier.width(6.dp))
                        Text("Build series data")
                    }
                }
            }
            else -> LazyColumn(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                items(ui.series.size) { index ->
                    SeriesCard(ui.series[index], onOpenSeries)
                }
                item { Spacer(Modifier.height(20.dp)) }
            }
        }
    }
}

@Composable
private fun SeriesCard(series: SeriesSummary, onOpenSeries: (String) -> Unit) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    Column(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, shape)
            .clickable { onOpenSeries(series.id) }
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                series.name,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = p.c100,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                "${series.playedCount}/${series.ownedCount} played",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
        LinearProgressIndicator(
            progress = { (series.completion.toFloat() / 100f).coerceIn(0f, 1f) },
            modifier = Modifier.fillMaxWidth().height(6.dp).clip(CircleShape),
            color = p.hlMid,
            trackColor = p.c800,
        )
        Text(
            buildString {
                append("${Math.round(series.completion)}% complete")
                if (series.remainingHours > 0) append(" · ${Format.hours(series.remainingHours)} left")
            },
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )
        series.nextGame?.let { next ->
            Text(
                "Next up: ${next.name}",
                style = MaterialTheme.typography.bodyMedium,
                color = p.c300,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
