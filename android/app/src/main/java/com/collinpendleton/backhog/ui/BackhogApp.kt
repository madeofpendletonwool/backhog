package com.collinpendleton.backhog.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.auth.AuthState
import com.collinpendleton.backhog.auth.SessionManager
import com.collinpendleton.backhog.data.Settings
import com.collinpendleton.backhog.data.SettingsStore
import com.collinpendleton.backhog.theme.BackhogTheme
import com.collinpendleton.backhog.theme.ThemeSlots
import com.collinpendleton.backhog.ui.components.Spinner
import com.collinpendleton.backhog.ui.auth.AuthFlow
import com.collinpendleton.backhog.ui.serverconfig.ServerConfigScreen
import com.collinpendleton.backhog.ui.shell.AppShell

/**
 * The whole app as a function of its session state: config → sign in →
 * shell. The theme worn before the shell exists is the games slot — the same
 * default the web's pre-paint script lands on.
 */
@Composable
fun BackhogApp(session: SessionManager, settings: SettingsStore, api: ApiClient) {
    val state by session.state.collectAsState()
    val settingsState by settings.settings.collectAsState(initial = Settings("", true, ThemeSlots()))

    BackhogTheme(theme = settingsState.slots.games) {
        when (val s = state) {
            AuthState.Boot -> Splash()
            AuthState.NeedServer -> ServerConfigScreen(session = session, api = api, settings = settings)
            AuthState.LoggedOut -> AuthFlow(session = session, api = api)
            is AuthState.LoggedIn ->
                AppShell(session = session, settings = settings, api = api, user = s.user)
        }
    }
}

@Composable
private fun Splash() {
    val p = com.collinpendleton.backhog.theme.LocalBackhogPalette.current
    Box(Modifier.fillMaxSize().background(p.c950), contentAlignment = Alignment.Center) {
        Spinner()
    }
}
