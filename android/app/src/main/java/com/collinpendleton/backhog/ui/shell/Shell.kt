package com.collinpendleton.backhog.ui.shell

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
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
import com.collinpendleton.backhog.ui.books.reader.ReaderScreen
import com.collinpendleton.backhog.ui.games.GameDetailScreen
import com.collinpendleton.backhog.ui.games.LibraryScreen
import com.collinpendleton.backhog.ui.games.QueueScreen
import com.collinpendleton.backhog.ui.media.MediaFilesScreen
import com.collinpendleton.backhog.ui.settings.SettingsScreen
import com.collinpendleton.backhog.ui.theme.Backhog
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable

// Each arena is its own nested graph, so switching tabs saves and restores
// that arena's back stack — Back and Up stay inside the arena you are in.
@Serializable data object GamesGraph
@Serializable data object GamesHome
@Serializable data object GamesQueue
@Serializable data class GameDetail(val entryId: String)
@Serializable data object BooksGraph
@Serializable data object BooksHome
@Serializable data class BookDetail(val entryId: String)
@Serializable data object ReadingDashboard
@Serializable data object SettingsRoute

/** The reader. A jump offset (−1 when absent) lands on a paragraph; with `peek` it is a look that never writes a position. */
@Serializable data class BookReader(val entryId: String, val offset: Long = -1L, val peek: Boolean = false)

/** The file layer — member/admin only, the server's RequireMediaManager gate. */
@Serializable data object BookFilesRoute

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
        NavHost(
            navController = nav,
            startDestination = if (initialArena == Arena.Books) BooksGraph else GamesGraph,
            modifier = Modifier.padding(padding),
        ) {
            navigation<GamesGraph>(startDestination = GamesHome) {
                composable<GamesHome> {
                    LibraryScreen(container, baseUrl, onOpenGame = openGame, onOpenQueue = { nav.navigate(GamesQueue) })
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
            }
            navigation<BooksGraph>(startDestination = BooksHome) {
                composable<BooksHome> {
                    BookLibraryScreen(
                        container = container,
                        baseUrl = baseUrl,
                        onOpen = { entryId -> nav.navigate(BookDetail(entryId)) },
                        onDashboard = { nav.navigate(ReadingDashboard) },
                        onOpenFiles = if (user.canManageMedia) {
                            { nav.navigate(BookFilesRoute) }
                        } else null,
                    )
                }
                composable<BookDetail> { entry ->
                    BookDetailScreen(
                        container = container,
                        baseUrl = baseUrl,
                        entryId = entry.toRoute<BookDetail>().entryId,
                        onBack = { nav.popBackStack() },
                        onOpen = { id -> nav.navigate(BookDetail(id)) },
                        canManageMedia = user.canManageMedia,
                        onRead = { id -> nav.navigate(BookReader(id)) },
                        onReadJump = { id, offset -> nav.navigate(BookReader(id, offset, peek = true)) },
                        onOpenFiles = { nav.navigate(BookFilesRoute) },
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
                composable<BookReader> { backStack ->
                    val route = backStack.toRoute<BookReader>()
                    ReaderScreen(
                        container = container,
                        baseUrl = baseUrl,
                        entryId = route.entryId,
                        jumpOffset = route.offset.takeIf { it >= 0 },
                        jumpPeek = route.peek,
                        onBack = { nav.popBackStack() },
                    )
                }
                composable<BookFilesRoute> {
                    MediaFilesScreen(
                        container = container,
                        baseUrl = baseUrl,
                        onBack = { nav.popBackStack() },
                    )
                }
            }
            composable<SettingsRoute> { SettingsScreen(container, user, baseUrl) }
        }
    }
}
