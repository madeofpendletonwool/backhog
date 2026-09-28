package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.ClearanceScenario
import com.collinpendleton.backhog.api.DebtReport
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/**
 * The web's DebtPage, games arena: the headline, the breakdown of where the
 * hours sit, and the pace panel with its clearance scenarios.
 */
@Composable
fun DebtScreen(container: AppContainer, baseUrl: String, onBack: () -> Unit) {
    val vm: DebtViewModel = viewModel(key = "debt|$baseUrl") {
        DebtViewModel(container.session, baseUrl)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette

    Column(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
        Row(Modifier.fillMaxWidth().padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("Backlog Debt", style = MaterialTheme.typography.headlineSmall, color = p.c100)
                Text("What you owe yourself, and when it gets paid off.", style = MaterialTheme.typography.bodySmall, color = p.c400)
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
            else -> {
                val debt = ui.debt ?: return
                Column(
                    Modifier.fillMaxSize().verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    if (debt.totalHours <= 0) {
                        NothingOwed()
                    } else {
                        DebtHeadline(debt)
                        BreakdownPanel(debt)
                        PacePanel(debt)
                    }
                    Spacer(Modifier.height(20.dp))
                }
            }
        }
    }
}

@Composable
private fun NothingOwed() {
    val p = Backhog.palette
    Column(
        Modifier.fillMaxWidth().padding(vertical = 48.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text("Nothing owed", style = MaterialTheme.typography.titleMedium, color = p.c100)
        Text(
            "No unplayed hours in the backlog. Add games you mean to finish and the debt math shows up here.",
            style = MaterialTheme.typography.bodyMedium,
            color = p.c400,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
    }
}

@Composable
private fun DebtHeadline(debt: DebtReport) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    Column(
        Modifier.fillMaxWidth().clip(shape).background(p.c900).border(1.dp, p.edgeStrong, shape).padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text("You currently have", style = MaterialTheme.typography.bodyMedium, color = p.c400)
        Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(Format.hours(debt.totalHours), style = MaterialTheme.typography.displaySmall, fontWeight = FontWeight.SemiBold, color = p.c100)
            Text("of unplayed games", style = MaterialTheme.typography.titleMedium, color = p.c400, modifier = Modifier.padding(bottom = 4.dp))
        }
        debt.projection.currentPace?.let { pace ->
            Text(
                "At your current pace of ${Format.hours(pace.hoursPerWeek)} hrs/week, your backlog would take " +
                    "${Format.timespan(pace.weeks)} to clear — around ${Format.monthYear(pace.clearBy)}.",
                style = MaterialTheme.typography.bodyMedium,
                color = p.c300,
            )
        }
    }
}

@Composable
private fun BreakdownPanel(debt: DebtReport) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    val rows = listOf(
        Triple("Main backlog", "Untouched games", debt.mainBacklogHours),
        Triple("Started", "Minus what you've already logged", debt.startedHours),
        Triple("Short games", "Under 8 hours each", debt.shortGamesHours),
    )
    Column(
        Modifier.fillMaxWidth().clip(shape).background(p.c900).border(1.dp, p.edgeStrong, shape).padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Where it sits", style = MaterialTheme.typography.titleSmall, color = p.c200)
        rows.forEach { (label, hint, hours) ->
            DebtRow(label, hint, Format.hours(hours))
        }
        // Deliberately "—": shown only once they mean something, like the web.
        DebtRow("Wishlist", "A shopping list, not a debt", debt.wishlistHours?.let { Format.hours(it) } ?: "—")
        DebtRow("DLC", "Add-ons of games still owed", debt.dlcHours?.let { Format.hours(it) } ?: "—")
    }
}

@Composable
private fun DebtRow(label: String, hint: String, value: String) {
    val p = Backhog.palette
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.bodyMedium, color = p.c200)
            Text(hint, style = MaterialTheme.typography.labelSmall, color = p.c500)
        }
        Text(value, style = MaterialTheme.typography.bodyMedium, color = p.c300)
    }
}

@Composable
private fun PacePanel(debt: DebtReport) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    Column(
        Modifier.fillMaxWidth().clip(shape).background(p.c900).border(1.dp, p.edgeStrong, shape).padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text("When it clears", style = MaterialTheme.typography.titleSmall, color = p.c200)
        Text(
            debt.pace.hoursPerWeekAll?.let { "All-time pace: ${Format.hours(it)} hrs/week" }
                ?: "Pace comes from logged play sessions",
            style = MaterialTheme.typography.labelSmall,
            color = p.c500,
        )
        val current = debt.projection.currentPace
        if (current != null) {
            ScenarioRow(current, "Your pace", highlight = true)
        } else {
            Text(
                "No sessions logged in the last 90 days, so there's no current pace to project from. Log playtime on a game and this fills in.",
                style = MaterialTheme.typography.bodySmall,
                color = p.c400,
                modifier = Modifier
                    .clip(MaterialTheme.shapes.small)
                    .background(p.c850)
                    .padding(10.dp),
            )
        }
        debt.projection.scenarios.forEach { scenario ->
            ScenarioRow(scenario, "If you play", highlight = false)
        }
    }
}

@Composable
private fun ScenarioRow(scenario: ClearanceScenario, label: String, highlight: Boolean) {
    val p = Backhog.palette
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            "$label ${Format.hours(scenario.hoursPerWeek)} hrs/week",
            style = MaterialTheme.typography.bodyMedium,
            color = if (highlight) p.c100 else p.c300,
            modifier = Modifier.weight(1f),
        )
        Column(horizontalAlignment = Alignment.End) {
            Text(Format.monthYear(scenario.clearBy), style = MaterialTheme.typography.bodyMedium, color = if (highlight) p.c100 else p.c200)
            Text(
                scenario.clearBy?.let { Format.timespan(scenario.weeks) } ?: "never",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
    }
}
