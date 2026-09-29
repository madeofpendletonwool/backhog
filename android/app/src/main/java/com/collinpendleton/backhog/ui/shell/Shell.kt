package com.collinpendleton.backhog.ui.shell

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.windowInsetsBottomHeight
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.MenuBook
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.SportsEsports
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
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
import com.collinpendleton.backhog.ui.components.AchievementToastsHost
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
import com.collinpendleton.backhog.ui.media.MediaFilesScreen
import com.collinpendleton.backhog.ui.player.FullPlayer
import com.collinpendleton.backhog.ui.player.MiniPlayer
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
@Serializable data class BookDetail(val entryId: String)
@Serializable data object ReadingDashboard
@Serializable data object SettingsRoute

/** The reader. A jump offset (−1 when absent) lands on a paragraph; with `peek` it is a look that never writes a position. */
@Serializable data class BookReader(val entryId: String, val offset: Long = -1L, val peek: Boolean = false)

/** The file layer — member/admin only, the server's RequireMediaManager gate. */
@Serializable data object BookFilesRoute

private data class Tab(val route: Any, val label: String, val icon: ImageVector, val arena: Arena?)

private val tabs = listOf(
    Tab(GamesGraph, "Games", Icons.Filled.SportsEsports, Arena.Games),
    Tab(BooksGraph, "Books", Icons.AutoMirrored.Filled.MenuBook, Arena.Books),
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

    // The tape outlives every screen: connect to the playback service once,
    // and the bar below the content renders whatever it is holding.
    LaunchedEffect(Unit) { container.player.ensure() }
    val player by container.player.state.collectAsStateWithLifecycle()
    var showPlayer by rememberSaveable { mutableStateOf(false) }
    val openFullPlayer = {
        showPlayer = true
        container.player.pollPositionForUi()
    }

    Box(Modifier.fillMaxSize()) {
        Scaffold(
            containerColor = p.c950,
            bottomBar = {
                Column {
                    MiniPlayer(
                        state = player,
                        onToggle = container.player::toggle,
                        onSkipBack = container.player::skipBack,
                        onSkipForward = container.player::skipForward,
                        onExpand = { openFullPlayer() },
                        onClose = container.player::close,
                    )
                    // The reader is full-bleed: the tabs would sit under every page
                    // turn. The mini player stays — the reader hands off to it.
                    val reading = entry?.destination?.hasRoute(BookReader::class) == true
                    if (!reading) NavigationBar(containerColor = p.c900) {
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
                    } else {
                        // What the bar would have kept clear: the system's gesture strip.
                        androidx.compose.foundation.layout.Spacer(
                            Modifier.windowInsetsBottomHeight(androidx.compose.foundation.layout.WindowInsets.navigationBars),
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

            // The unlock toasts: above every screen, bottom-centre.
            AchievementToastsHost(container, baseUrl)
        }
    }

    // "Now playing" over the whole app: raised by the bar's identity block,
    // dropped by Back or its own chevron. The tape keeps playing under it.
    AnimatedVisibility(
        visible = showPlayer && player.hasBook,
        enter = slideInVertically(initialOffsetY = { it }) + fadeIn(),
        exit = slideOutVertically(targetOffsetY = { it }) + fadeOut(),
    ) {
        FullPlayer(
            container = container,
            baseUrl = baseUrl,
            state = player,
            onToggle = container.player::toggle,
            onSeek = container.player::seekTo,
            onSkip = { delta -> if (delta > 0) container.player.skipForward() else container.player.skipBack() },
            onNextTrack = container.player::nextTrack,
            onPreviousTrack = container.player::previousTrack,
            onSetRate = container.player::setRate,
            onSetSleep = container.player::setSleep,
            onReload = container.player::reload,
            onClose = { showPlayer = false },
            onOpenBook = { entryId ->
                showPlayer = false
                nav.navigate(BookDetail(entryId))
            },
            onContinueReading = { entryId, offset ->
                showPlayer = false
                nav.navigate(BookReader(entryId, offset))
            },
        )
    }
    }
}
