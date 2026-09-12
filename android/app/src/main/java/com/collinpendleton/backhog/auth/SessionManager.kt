package com.collinpendleton.backhog.auth

import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.LoginRequest
import com.collinpendleton.backhog.api.PasswordChangeRequest
import com.collinpendleton.backhog.api.RegisterRequest
import com.collinpendleton.backhog.api.SessionEvents
import com.collinpendleton.backhog.api.User
import com.collinpendleton.backhog.data.SettingsStore
import java.io.IOException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

/**
 * Where the app stands between the server and the shell. Exactly one state
 * is current at any time, and the UI is a pure function of it:
 *
 * - [AuthState.Boot] — reading local settings, deciding the rest
 * - [AuthState.NeedServer] — first launch (or switch-server): the config
 *   screen owns the window until a base URL is saved
 * - [AuthState.LoggedOut] — a server is configured; the session (if any)
 *   did not survive; login and register are the offers
 * - [AuthState.LoggedIn] — a real user, cold-started via cookie + /auth/me
 *   or freshly signed in
 */
sealed interface AuthState {
    data object Boot : AuthState
    data object NeedServer : AuthState
    data object LoggedOut : AuthState
    data class LoggedIn(val user: User) : AuthState
}

class SessionManager(
    private val settings: SettingsStore,
    val client: ApiClient,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    private val stateField = MutableStateFlow<AuthState>(AuthState.Boot)
    val state: StateFlow<AuthState> = stateField

    /** A token extracted from a pasted invite link, waiting for register. */
    val pendingInvite = MutableStateFlow<String?>(null)

    private var baseUrl: String = ""

    /** The configured origin, for display ("switch server" shows it). */
    val serverUrl: String get() = baseUrl

    /** The Retrofit surface for the configured server. */
    fun api() = client.apiFor(baseUrl)

    init {
        // Cold-start session resume: the persistent cookie jar replays the
        // session cookie, /auth/me says whose it is.
        scope.launch {
            val url = settings.settings.first().baseUrl
            if (url.isBlank()) {
                stateField.value = AuthState.NeedServer
                return@launch
            }
            baseUrl = url
            restore()
        }

        // A 401 anywhere outside /api/auth means the session died mid-flight:
        // drop to the login screen.
        scope.launch {
            SessionEvents.unauthorized.collect {
                if (stateField.value is AuthState.LoggedIn) {
                    stateField.value = AuthState.LoggedOut
                }
            }
        }
    }

    private suspend fun restore() {
        val user =
            try {
                client.call { api().me() }
            } catch (_: ApiError) {
                null
            } catch (_: IOException) {
                // Server unreachable at launch: the login screen will name
                // the problem when it is asked to do anything.
                null
            }
        stateField.value = if (user != null) AuthState.LoggedIn(user) else AuthState.LoggedOut
    }

    fun login(email: String, password: String, onResult: (Result<User>) -> Unit) {
        scope.launch {
            onResult(
                client
                    .callCatching { api().login(LoginRequest(email, password)) }
                    .onSuccess { user -> stateField.value = AuthState.LoggedIn(user) },
            )
        }
    }

    fun register(
        email: String,
        username: String,
        password: String,
        invite: String?,
        onResult: (Result<User>) -> Unit,
    ) {
        scope.launch {
            onResult(
                client
                    .callCatching {
                        api().register(
                            RegisterRequest(email, username, password, invite?.takeIf { it.isNotBlank() }),
                        )
                    }
                    .onSuccess { user -> stateField.value = AuthState.LoggedIn(user) },
            )
        }
    }

    fun logout(onDone: () -> Unit = {}) {
        scope.launch {
            runCatching { client.call { api().logout() } }
            client.cookieJar.clear()
            stateField.value = AuthState.LoggedOut
            onDone()
        }
    }

    fun changePassword(current: String, next: String, onResult: (Result<Unit>) -> Unit) {
        scope.launch {
            onResult(
                client.callCatching {
                    api().changePassword(PasswordChangeRequest(current, next))
                    Unit
                },
            )
        }
    }

    /**
     * Save a validated base URL. The cookie jar is cleared either way — a
     * cookie from one origin is worthless (and wrong) against another — so
     * this one action serves first-launch config and switch-server both.
     */
    fun adoptServer(url: String, invite: String?) {
        scope.launch {
            client.cookieJar.clear()
            settings.setBaseUrl(url)
            baseUrl = url
            pendingInvite.value = invite
            stateField.value = AuthState.LoggedOut
        }
    }

    /** Settings → switch server: start config over, session kept in place
     *  until a new URL is actually adopted (cancel restores it). */
    fun beginSwitchServer() {
        stateField.value = AuthState.NeedServer
    }

    /** Config cancelled — go back to whatever the saved URL restores to. */
    fun cancelSwitchServer() {
        scope.launch {
            if (settings.settings.first().baseUrl.isBlank()) {
                stateField.value = AuthState.NeedServer
            } else {
                restore()
            }
        }
    }
}
