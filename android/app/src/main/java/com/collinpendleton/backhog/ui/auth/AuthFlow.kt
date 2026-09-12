package com.collinpendleton.backhog.ui.auth

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.api.AuthConfig
import com.collinpendleton.backhog.auth.SessionManager
import java.io.IOException

/**
 * Sign-in and sign-up, mirroring the web's Login and Register pages: the
 * unauthenticated /auth/config payload decides what may be offered, and an
 * invite token resolves into the role it grants.
 */
@Composable
fun AuthFlow(session: SessionManager, api: ApiClient) {
    var mode by remember { mutableStateOf<AuthMode>(AuthMode.LOGIN) }
    when (mode) {
        AuthMode.LOGIN -> LoginScreen(session = session, api = api, onRegister = { mode = AuthMode.REGISTER })
        AuthMode.REGISTER -> RegisterScreen(session = session, api = api, onLogin = { mode = AuthMode.LOGIN })
    }
}

private enum class AuthMode { LOGIN, REGISTER }

internal sealed interface AuthConfigState {
    data object Loading : AuthConfigState
    data class Ready(val config: AuthConfig) : AuthConfigState
    data class Failed(val message: String) : AuthConfigState
}

/** The unauthenticated payload the sign-in pages read — fetched per screen
 *  (the invite token is part of the question being asked). */
@Composable
internal fun rememberAuthConfig(api: ApiClient, session: SessionManager, invite: String?): AuthConfigState {
    var state by remember { mutableStateOf<AuthConfigState>(AuthConfigState.Loading) }
    LaunchedEffect(invite) {
        state = AuthConfigState.Loading
        state =
            try {
                AuthConfigState.Ready(
                    api.call { session.api().authConfig(invite?.takeIf { it.isNotBlank() }) },
                )
            } catch (e: IOException) {
                AuthConfigState.Failed("Could not reach the server — check your connection.")
            } catch (e: Exception) {
                AuthConfigState.Failed(e.message ?: "Could not read the sign-in configuration.")
            }
    }
    return state
}
