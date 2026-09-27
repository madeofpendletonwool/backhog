package com.collinpendleton.backhog.ui.shell

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
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
import androidx.compose.runtime.rememberCoroutineScope
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
import com.collinpendleton.backhog.ui.books.BookDetailScreen
import com.collinpendleton.backhog.ui.books.BookLibraryScreen
import com.collinpendleton.backhog.ui.books.ReadingDashboardScreen
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.settings.SettingsScreen
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable

// Each arena is its own nested graph, so switching tabs saves and restores
// that arena's back stack — Back and Up stay inside the arena you are in.
@Serializable data object GamesGraph
@Serializable data object GamesHome
@Serializable data object BooksGraph
@Serializable data object BooksHome
@Serializable data class BookDetail(val entryId: String)
@Serializable data object ReadingDashboard
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
        NavHost(
            navController = nav,
            startDestination = if (startArena == Arena.Books) BooksGraph else GamesGraph,
            modifier = Modifier.padding(padding),
        ) {
            navigation<GamesGraph>(startDestination = GamesHome) {
                composable<GamesHome> { ArenaHome(Arena.Games, user) }
            }
            navigation<BooksGraph>(startDestination = BooksHome) {
                composable<BooksHome> {
                    BookLibraryScreen(
                        container = container,
                        baseUrl = baseUrl,
                        onOpen = { entryId -> nav.navigate(BookDetail(entryId)) },
                        onDashboard = { nav.navigate(ReadingDashboard) },
                    )
                }
                composable<BookDetail> { entry ->
                    BookDetailScreen(
                        container = container,
                        baseUrl = baseUrl,
                        entryId = entry.toRoute<BookDetail>().entryId,
                        onBack = { nav.popBackStack() },
                        onOpen = { id -> nav.navigate(BookDetail(id)) },
                    )
                }
                composable<ReadingDashboard> {
                    ReadingDashboardScreen(
                        container = container,
                        baseUrl = baseUrl,
                        onBack = { nav.popBackStack() },
                        onOpen = { entryId -> nav.navigate(BookDetail(entryId)) },
                    )
                }
            }
            composable<SettingsRoute> { SettingsScreen(container, user, baseUrl) }
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
