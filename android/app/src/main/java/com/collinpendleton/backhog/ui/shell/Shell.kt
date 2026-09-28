package com.collinpendleton.backhog.ui.shell

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavDestination.Companion.hierarchy
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.navigation
import androidx.navigation.compose.rememberNavController
import androidx.navigation.toRoute
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.User
import com.collinpendleton.backhog.data.Arena
import com.collinpendleton.backhog.ui.components.AchievementToastsHost
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.games.AchievementsScreen
import com.collinpendleton.backhog.ui.games.DashboardScreen
import com.collinpendleton.backhog.ui.games.DebtScreen
import com.collinpendleton.backhog.ui.games.GameDetailScreen
import com.collinpendleton.backhog.ui.games.LibraryScreen
import com.collinpendleton.backhog.ui.games.ListDetailScreen
import com.collinpendleton.backhog.ui.games.ListsScreen
import com.collinpendleton.backhog.ui.games.ProjectDetailScreen
import com.collinpendleton.backhog.ui.games.ProjectsScreen
import com.collinpendleton.backhog.ui.games.QueueScreen
import com.collinpendleton.backhog.ui.games.SeriesDetailScreen
import com.collinpendleton.backhog.ui.games.SeriesScreen
import com.collinpendleton.backhog.ui.settings.SettingsScreen
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable

// Each arena is its own nested graph, so switching tabs saves and restores
// that arena's back stack — Back and Up stay inside the arena you are in.
@Serializable data object GamesGraph
@Serializable data object GamesHome
@Serializable data object GamesQueue
@Serializable data class GameDetail(val entryId: String)
@Serializable data object GamesDashboard
@Serializable data object GamesLists
@Serializable data class ListDetail(val listId: String)
@Serializable data object GamesSeries
@Serializable data class SeriesDetail(val seriesId: String)
@Serializable data object GamesProjects
@Serializable data class ProjectDetail(val projectId: String)
@Serializable data object GamesAchievements
@Serializable data object GamesDebt
@Serializable data object BooksGraph
@Serializable data object BooksHome
@Serializable data object SettingsRoute

private data class Tab(val route: Any, val label: String, val icon: ImageVector, val arena: Arena?)

private val tabs = listOf(
    Tab(GamesGraph, "Games", Icons.Filled.PlayArrow, Arena.Games),
    Tab(BooksGraph, "Books", Icons.Filled.Star, Arena.Books),
    Tab(SettingsRoute, "Settings", Icons.Filled.Settings, null),
)

@Composable
fun Shell(container: AppContainer, user: User, baseUrl: String, startArena: Arena) {
    val nav = rememberNavController()
    val scope = rememberCoroutineScope()
    val entry by nav.currentBackStackEntryAsState()
    val p = Backhog.palette
    // Read the opening arena once: a live value here would rebuild the graph
    // (and drop each arena's saved back stack) every time the tab changed.
    val initialArena = remember { startArena }
    val openGame = { id: String -> nav.navigate(GameDetail(id)) }

    Scaffold(
        containerColor = p.c950,
        bottomBar = {
            NavigationBar(containerColor = p.c900) {
                tabs.forEach { tab ->
                    val selected = entry?.destination?.hierarchy?.any { it.hasRoute(tab.route::class) } == true
                    NavigationBarItem(
                        selected = selected,
                        onClick = {
                            // Standing in an arena picks its theme; Settings keeps the last arena's.
                            tab.arena?.let { scope.launch { container.preferences.setArena(it) } }
                            nav.navigate(tab.route) {
                                popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = { Icon(tab.icon, contentDescription = null) },
                        label = { Text(tab.label) },
                        colors = NavigationBarItemDefaults.colors(
                            selectedIconColor = p.hlBright,
                            selectedTextColor = p.c100,
                            indicatorColor = p.fillActive,
                            unselectedIconColor = p.c500,
                            unselectedTextColor = p.c500,
                        ),
                    )
                }
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            NavHost(
                navController = nav,
                startDestination = if (initialArena == Arena.Books) BooksGraph else GamesGraph,
            ) {
                navigation<GamesGraph>(startDestination = GamesHome) {
                    composable<GamesHome> {
                        LibraryScreen(
                            container,
                            baseUrl,
                            onOpenGame = openGame,
                            onOpenQueue = { nav.navigate(GamesQueue) },
                            onOpenDashboard = { nav.navigate(GamesDashboard) },
                            onOpenLists = { nav.navigate(GamesLists) },
                            onOpenSeries = { nav.navigate(GamesSeries) },
                            onOpenProjects = { nav.navigate(GamesProjects) },
                            onOpenAchievements = { nav.navigate(GamesAchievements) },
                            onOpenDebt = { nav.navigate(GamesDebt) },
                        )
                    }
                    composable<GamesQueue> {
                        QueueScreen(container, baseUrl, onOpenGame = openGame, onBack = { nav.popBackStack() })
                    }
                    composable<GameDetail> { backStack ->
                        val route = backStack.toRoute<GameDetail>()
                        GameDetailScreen(
                            container,
                            baseUrl,
                            route.entryId,
                            onBack = { nav.popBackStack() },
                            onRemoved = { nav.popBackStack() },
                        )
                    }
                    composable<GamesDashboard> {
                        DashboardScreen(
                            container,
                            baseUrl,
                            onBack = { nav.popBackStack() },
                            onOpenGame = openGame,
                            onOpenDebt = { nav.navigate(GamesDebt) },
                        )
                    }
                    composable<GamesDebt> {
                        DebtScreen(container, baseUrl, onBack = { nav.popBackStack() })
                    }
                    composable<GamesLists> {
                        ListsScreen(container, baseUrl, onOpenList = { nav.navigate(ListDetail(it)) })
                    }
                    composable<ListDetail> { backStack ->
                        val route = backStack.toRoute<ListDetail>()
                        ListDetailScreen(
                            container,
                            baseUrl,
                            route.listId,
                            onBack = { nav.popBackStack() },
                            onRemoved = { nav.popBackStack() },
                            onOpenGame = openGame,
                        )
                    }
                    composable<GamesSeries> {
                        SeriesScreen(
                            container,
                            baseUrl,
                            onBack = { nav.popBackStack() },
                            onOpenSeries = { nav.navigate(SeriesDetail(it)) },
                        )
                    }
                    composable<SeriesDetail> { backStack ->
                        val route = backStack.toRoute<SeriesDetail>()
                        SeriesDetailScreen(
                            container,
                            baseUrl,
                            route.seriesId,
                            onBack = { nav.popBackStack() },
                            onOpenGame = openGame,
                        )
                    }
                    composable<GamesProjects> {
                        ProjectsScreen(container, baseUrl, onOpenProject = { nav.navigate(ProjectDetail(it)) })
                    }
                    composable<ProjectDetail> { backStack ->
                        val route = backStack.toRoute<ProjectDetail>()
                        ProjectDetailScreen(
                            container,
                            baseUrl,
                            route.projectId,
                            onBack = { nav.popBackStack() },
                            onRemoved = { nav.popBackStack() },
                            onOpenGame = openGame,
                        )
                    }
                    composable<GamesAchievements> {
                        AchievementsScreen(
                            container,
                            baseUrl,
                            onBack = { nav.popBackStack() },
                            onOpenGame = openGame,
                        )
                    }
                }
                navigation<BooksGraph>(startDestination = BooksHome) {
                    composable<BooksHome> { ArenaHome(Arena.Books, user) }
                }
                composable<SettingsRoute> { SettingsScreen(container, user, baseUrl) }
            }

            // The unlock toasts: above every screen, bottom-centre.
            AchievementToastsHost(container, baseUrl)
        }
    }
}

/** Stage 1's placeholder for each arena's home — themed, empty, and honest about it. */
@Composable
private fun ArenaHome(arena: Arena, user: User) {
    val p = Backhog.palette
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(arena.label, style = MaterialTheme.typography.headlineMedium, color = p.c100)
        Text(
            when (arena) {
                Arena.Games -> "Hi ${user.username}. Your library, queue and dashboard land here next."
                Arena.Books -> "Hi ${user.username}. Your shelf, reader and audiobooks land here next."
            },
            style = MaterialTheme.typography.bodyLarge,
            color = p.c400,
        )
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            ToneChip("Backlog", Tones.Backlog)
            ToneChip(if (arena == Arena.Games) "Playing" else "Reading", Tones.Playing)
            ToneChip(if (arena == Arena.Games) "Played" else "Read", Tones.Played)
            ToneChip("Dropped", Tones.Dropped)
        }
    }
}
