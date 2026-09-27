package com.collinpendleton.backhog

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.toArgb
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.collinpendleton.backhog.data.Arena
import com.collinpendleton.backhog.data.ThemeSettings
import com.collinpendleton.backhog.session.SessionState
import com.collinpendleton.backhog.ui.auth.ServerScreen
import com.collinpendleton.backhog.ui.auth.SignInScreen
import com.collinpendleton.backhog.ui.auth.UnreachableScreen
import com.collinpendleton.backhog.ui.shell.Shell
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.BackhogTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val container = (application as BackhogApp).container
        setContent { BackhogRoot(container) }
    }
}

@Composable
private fun BackhogRoot(container: AppContainer) {
    val state by container.session.state.collectAsStateWithLifecycle()
    val themes by container.preferences.themes.collectAsStateWithLifecycle(ThemeSettings())
    val arena by container.preferences.arena.collectAsStateWithLifecycle(null)
    val theme = themes.forArena(arena ?: Arena.Games)

    BackhogTheme(theme) {
        SystemBars(theme.palette.isLight)
        // A Surface, not a Box: it also sets the content colour every bare Text inherits.
        Surface(Modifier.fillMaxSize(), color = Backhog.palette.c950, contentColor = Backhog.palette.c100) {
            when (val s = state) {
                SessionState.Starting -> Loading()
                SessionState.NeedsServer -> ServerScreen(container.session)
                is SessionState.SignedOut -> SignInScreen(container.session, s)
                is SessionState.Unreachable -> UnreachableScreen(container.session, s)
                // Wait for the saved arena so the shell opens where you left it.
                is SessionState.SignedIn -> arena?.let { Shell(container, s.user, s.baseUrl, it) } ?: Loading()
            }
        }
    }
}

@Composable
private fun Loading() {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
}

/** Status/nav bar icons follow the theme's ground: dark icons on Paper, light elsewhere. */
@Composable
private fun SystemBars(light: Boolean) {
    val activity = androidx.activity.compose.LocalActivity.current as? ComponentActivity ?: return
    val transparent = android.graphics.Color.TRANSPARENT
    LaunchedEffect(light) {
        val style = if (light) {
            SystemBarStyle.light(transparent, androidx.compose.ui.graphics.Color.Black.toArgb())
        } else {
            SystemBarStyle.dark(transparent)
        }
        activity.enableEdgeToEdge(statusBarStyle = style, navigationBarStyle = style)
    }
}
