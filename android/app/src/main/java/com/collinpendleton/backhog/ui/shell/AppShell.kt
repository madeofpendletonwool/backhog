package com.collinpendleton.backhog.ui.shell

import androidx.compose.animation.Crossfade
import androidx.compose.foundation.background
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoStories
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.SportsEsports
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.api.User
import com.collinpendleton.backhog.auth.SessionManager
import com.collinpendleton.backhog.data.Settings
import com.collinpendleton.backhog.data.SettingsStore
import com.collinpendleton.backhog.theme.BackhogTheme
import com.collinpendleton.backhog.theme.LocalBackhogPalette
import com.collinpendleton.backhog.theme.ThemeSlots
import com.collinpendleton.backhog.ui.settings.SettingsScreen

/**
 * The app shell: bottom navigation across the two arenas and settings,
 * matching the web's mobile layout. Arena-aware state — crossing arenas
 * re-dresses the whole room (each arena keeps its own theme slot) while each
 * destination keeps its own back stack, so Back/Up behaves per arena.
 */
@Composable
fun AppShell(session: SessionManager, settings: SettingsStore, api: ApiClient, user: User) {
    val settingsState by settings.settings.collectAsState(initial = Settings("", true, ThemeSlots()))

    // Each destination keeps its own place; the theme follows the arena the
    // reader is standing in. Settings wears the arena they arrived from.
    var current by rememberSaveable { mutableStateOf(ShellDestination.GAMES) }

    // The last arena visited — what settings renders wearing.
    var savedArena by rememberSaveable { mutableStateOf("games") }
    val lastArena = if (current != ShellDestination.SETTINGS) current.name.lowercase() else savedArena
    LaunchedEffect(lastArena) { savedArena = lastArena }

    val theme =
        when (current) {
            ShellDestination.GAMES -> settingsState.slots.games
            ShellDestination.BOOKS -> settingsState.slots.books
            ShellDestination.SETTINGS ->
                if (lastArena == "books") settingsState.slots.books else settingsState.slots.games
        }

    BackhogTheme(theme = theme) {
        val p = LocalBackhogPalette.current
        Scaffold(
            containerColor = p.c950,
            bottomBar = {
                NavigationBar(containerColor = p.c900, tonalElevation = 0.dp) {
                    ShellDestination.entries.forEach { dest ->
                        val selected = current == dest
                        NavigationBarItem(
                            selected = selected,
                            onClick = { current = dest },
                            icon = {
                                Icon(
                                    when (dest) {
                                        ShellDestination.GAMES -> Icons.Outlined.SportsEsports
                                        ShellDestination.BOOKS -> Icons.Outlined.AutoStories
                                        ShellDestination.SETTINGS -> Icons.Outlined.Settings
                                    },
                                    contentDescription = dest.label,
                                    modifier = Modifier.size(24.dp),
                                )
                            },
                            label = { Text(dest.label) },
                            colors =
                                NavigationBarItemDefaults.colors(
                                    selectedIconColor = p.hlBright,
                                    selectedTextColor = p.hlBright,
                                    unselectedIconColor = p.c500,
                                    unselectedTextColor = p.c500,
                                    indicatorColor = p.fillActive,
                                ),
                        )
                    }
                }
            },
        ) { padding ->
            Box(Modifier.fillMaxSize().padding(padding)) {
                Crossfade(targetState = current, label = "arena") { dest ->
                    when (dest) {
                        ShellDestination.GAMES -> ArenaPlaceholder("Games", "The games arena moves in at Stage 2: the library, IGDB search, the play queue, sessions — everything that hangs off them.")
                        ShellDestination.BOOKS -> ArenaPlaceholder("Books", "The books arena arrives at Stage 4: the shelf, Open Library search, the reader, and the listening room.")
                        ShellDestination.SETTINGS -> SettingsScreen(session = session, settings = settings, user = user, arena = lastArena)
                    }
                }
            }
        }
    }
}

private enum class ShellDestination(val label: String) {
    GAMES("Games"),
    BOOKS("Books"),
    SETTINGS("Settings"),
}

/** A themed, empty arena — the skeleton stage 1 ships, wearing its theme. */
@Composable
private fun ArenaPlaceholder(title: String, note: String) {
    val p = LocalBackhogPalette.current
    Column(
        Modifier
            .fillMaxSize()
            .background(p.c950)
            .padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.height(48.dp))
        Text("🐗", fontSize = 40.sp)
        Spacer(Modifier.height(10.dp))
        Text(
            title,
            color = p.c100,
            fontSize = 24.sp,
            fontWeight = FontWeight.Bold,
            fontFamily = p.displayFont,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            note,
            color = p.c400,
            fontSize = 14.sp,
            lineHeight = 20.sp,
            modifier = Modifier.fillMaxWidth(0.86f),
        )
        Spacer(Modifier.height(28.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                Modifier
                    .width(48.dp)
                    .height(2.dp)
                    .background(p.cLine),
            )
            Spacer(Modifier.width(10.dp))
            Text("Backhog for Android", color = p.c600, fontSize = 12.sp)
            Spacer(Modifier.width(10.dp))
            Box(
                Modifier
                    .width(48.dp)
                    .height(2.dp)
                    .background(p.cLine),
            )
        }
    }
}
