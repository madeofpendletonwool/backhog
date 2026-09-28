package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.AchievementStatus
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.session.SessionManager
import com.collinpendleton.backhog.ui.components.AchievementCard
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class AchievementsUiState(
    val achievements: List<AchievementStatus> = emptyList(),
    val loading: Boolean = true,
    val error: String? = null,
    /** The domain tab: "all" | "game" | "book". */
    val tab: String = "all",
) {
    /** "any" achievements (the eggs) are about the app itself — they answer every tab. */
    val visible: List<AchievementStatus>
        get() = achievements.filter { tab == "all" || it.domain == "any" || it.domain == tab }
    val unlocked: Int get() = visible.count { it.unlockedAt != null }
}

/** The trophy wall: the catalogue with unlock state, grouped by tier. */
class AchievementsViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(AchievementsUiState())
    val state: StateFlow<AchievementsUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).achievements() }
                .onSuccess { response -> _state.update { s -> s.copy(achievements = response.achievements, loading = false) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    fun setTab(tab: String) = _state.update { it.copy(tab = tab) }
}

private val TIERS = listOf("bronze", "silver", "gold", "legendary")

private fun tierLabel(tier: String): String = when (tier) {
    "bronze" -> "Bronze"
    "silver" -> "Silver"
    "gold" -> "Gold"
    "legendary" -> "Legendary"
    else -> "Bronze"
}

/**
 * The gallery — the web's AchievementsPage: domain tabs, tier sections, the
 * masked-??? locked cards, and the dates and triggering entries on unlocked
 * ones. No Konami egg here — it was deliberately skipped for the app.
 */
@Composable
fun AchievementsScreen(
    container: AppContainer,
    baseUrl: String,
    onBack: () -> Unit,
    onOpenGame: (String) -> Unit,
) {
    val vm: AchievementsViewModel = viewModel(key = "achievements|$baseUrl") {
        AchievementsViewModel(container.session, baseUrl)
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
                Text("Achievements", style = MaterialTheme.typography.headlineSmall, color = p.c100)
                Text(
                    "Progress through the pile, not hours in it. ${ui.unlocked} of ${ui.visible.size} earned",
                    style = MaterialTheme.typography.bodySmall,
                    color = p.c400,
                )
            }
        }

        // Domain tabs.
        Row(
            Modifier
                .fillMaxWidth()
                .clip(MaterialTheme.shapes.small)
                .background(p.c900)
                .padding(4.dp),
        ) {
            listOf("all" to "All", "game" to "Games", "book" to "Books").forEach { (key, label) ->
                val active = ui.tab == key
                Text(
                    label,
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
                    color = if (active) p.c100 else p.c500,
                    textAlign = TextAlign.Center,
                    modifier = Modifier
                        .weight(1f)
                        .clip(MaterialTheme.shapes.extraSmall)
                        .background(if (active) p.fillActive else androidx.compose.ui.graphics.Color.Transparent)
                        .clickable { vm.setTab(key) }
                        .padding(vertical = 8.dp),
                )
            }
        }

        when {
            ui.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            ui.error != null && ui.achievements.isEmpty() -> Column(
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
            else -> LazyVerticalGrid(
                columns = GridCells.Adaptive(minSize = 300.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
                modifier = Modifier.fillMaxSize().padding(top = 12.dp),
            ) {
                TIERS.forEach { tier ->
                    val group = ui.visible.filter { it.tier == tier }
                    if (group.isEmpty()) return@forEach
                    val earned = group.count { it.unlockedAt != null }
                    item(span = { GridItemSpan(maxLineSpan) }) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Box(
                                Modifier.size(10.dp).clip(androidx.compose.foundation.shape.CircleShape)
                                    .background(Tones.forTier(tier)),
                            )
                            Text(
                                tierLabel(tier).uppercase(),
                                style = MaterialTheme.typography.labelMedium,
                                fontWeight = FontWeight.SemiBold,
                                color = Tones.forTier(tier),
                            )
                            Text(
                                "$earned of ${group.size}",
                                style = MaterialTheme.typography.labelSmall,
                                color = p.c500,
                            )
                        }
                    }
                    items(group, key = { "${tier}-${it.id}" }) { achievement ->
                        AchievementCard(achievement, baseUrl, onOpenEntry = onOpenGame)
                    }
                }
                item(span = { GridItemSpan(maxLineSpan) }) { Spacer(Modifier.height(20.dp)) }
            }
        }
    }
}
