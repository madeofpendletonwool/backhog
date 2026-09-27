package com.collinpendleton.backhog.session

import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.BackhogApi
import com.collinpendleton.backhog.api.PersistentCookieJar
import com.collinpendleton.backhog.api.User
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.data.AppPreferences
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Where the app stands with its server. The root composable renders exactly one screen per state. */
sealed interface SessionState {
    /** Cold start: reading the saved server and asking it who we are. */
    data object Starting : SessionState

    /** No server configured yet (first launch, or after "switch server"). */
    data object NeedsServer : SessionState

    /** A server, but nobody signed in. [invite] is a token carried in from a pasted invite link. */
    data class SignedOut(val baseUrl: String, val invite: String? = null, val expired: Boolean = false) : SessionState

    /** The saved session could not be checked because the server did not answer. */
    data class Unreachable(val baseUrl: String, val message: String) : SessionState

    data class SignedIn(val baseUrl: String, val user: User) : SessionState
}

/**
 * Owns the base URL, the cookie jar and the API instance built from them, and
 * the one [SessionState] the UI follows. The cookie *is* the session: cold
 * start just asks `/auth/me` whether it is still good.
 */
class SessionManager(
    private val prefs: AppPreferences,
    private val cookieJar: PersistentCookieJar,
    private val scope: CoroutineScope,
    private val apiFactory: (baseUrl: String, onUnauthorized: () -> Unit) -> BackhogApi =
        { url, onUnauthorized -> ApiClient.create(url, cookieJar, onUnauthorized) },
) {
    private val _state = MutableStateFlow<SessionState>(SessionState.Starting)
    val state: StateFlow<SessionState> = _state.asStateFlow()

    private var apiFor: Pair<String, BackhogApi>? = null

    /** The API for the configured server. Only valid once a server is set. */
    fun api(baseUrl: String): BackhogApi {
        apiFor?.let { (url, api) -> if (url == baseUrl) return api }
        return apiFactory(baseUrl, ::onUnauthorized).also { apiFor = baseUrl to it }
    }

    val currentApi: BackhogApi?
        get() = when (val s = _state.value) {
            is SessionState.SignedIn -> api(s.baseUrl)
            is SessionState.SignedOut -> api(s.baseUrl)
            is SessionState.Unreachable -> api(s.baseUrl)
            else -> null
        }

    fun start() {
        scope.launch { restore() }
    }

    /** Cold start and "retry": resume the cookie session if there is one. */
    suspend fun restore() {
        val baseUrl = prefs.currentBaseUrl()
        if (baseUrl == null) {
            _state.value = SessionState.NeedsServer
            return
        }
        if (!cookieJar.hasCookie(SESSION_COOKIE)) {
            _state.value = SessionState.SignedOut(baseUrl)
            return
        }
        _state.value = SessionState.Starting
        apiCall { api(baseUrl).me() }
            .onSuccess { _state.value = SessionState.SignedIn(baseUrl, it) }
            .onFailure { e ->
                val err = e as ApiError
                _state.value = when {
                    err.status == 0 -> SessionState.Unreachable(baseUrl, err.message.orEmpty())
                    else -> SessionState.SignedOut(baseUrl, expired = err.isUnauthorized)
                }
            }
    }

    /** Save a server the person has already health-checked. Any old session belongs to the old server. */
    suspend fun configureServer(baseUrl: String, invite: String? = null) {
        cookieJar.clear()
        prefs.setBaseUrl(baseUrl)
        _state.value = SessionState.SignedOut(baseUrl, invite = invite)
    }

    fun signedIn(user: User) {
        val baseUrl = when (val s = _state.value) {
            is SessionState.SignedOut -> s.baseUrl
            is SessionState.SignedIn -> s.baseUrl
            is SessionState.Unreachable -> s.baseUrl
            else -> return
        }
        _state.value = SessionState.SignedIn(baseUrl, user)
    }

    suspend fun logout() {
        val baseUrl = (state.value as? SessionState.SignedIn)?.baseUrl
        // Best effort: the server forgets the session; either way this device does.
        if (baseUrl != null) apiCall { api(baseUrl).logout() }
        cookieJar.clear()
        _state.value = baseUrl?.let { SessionState.SignedOut(it) } ?: SessionState.NeedsServer
    }

    suspend fun switchServer() {
        (state.value as? SessionState.SignedIn)?.let { s -> apiCall { api(s.baseUrl).logout() } }
        cookieJar.clear()
        prefs.setBaseUrl(null)
        apiFor = null
        _state.value = SessionState.NeedsServer
    }

    /** Back to the sign-in form, keeping a pasted invite. */
    fun showSignIn(invite: String? = null) {
        _state.update { s ->
            when (s) {
                is SessionState.SignedOut -> s.copy(invite = invite ?: s.invite)
                is SessionState.Unreachable -> SessionState.SignedOut(s.baseUrl, invite)
                else -> s
            }
        }
    }

    private fun onUnauthorized() {
        cookieJar.clear()
        _state.update { s ->
            if (s is SessionState.SignedIn) SessionState.SignedOut(s.baseUrl, expired = true) else s
        }
    }

    companion object {
        const val SESSION_COOKIE = "backhog_session"
    }
}
