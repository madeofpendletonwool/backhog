package com.collinpendleton.backhog.ui.auth

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.AuthConfig
import com.collinpendleton.backhog.api.LoginRequest
import com.collinpendleton.backhog.api.RegisterRequest
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.data.ServerUrl
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class SignInUiState(
    val registering: Boolean = false,
    val config: AuthConfig? = null,
    val configError: String? = null,
    val invite: String? = null,
    val busy: Boolean = false,
    val error: String? = null,
) {
    val invited: Boolean get() = config?.invite != null

    /** The web's rule: sign-up is offered with an invite, open registration, or on a fresh server. */
    val canRegister: Boolean
        get() = invited || config?.registrationEnabled == true || config?.setup == true

    /** A token that the server did not recognise — unknown, spent, revoked or expired. */
    val staleInvite: Boolean get() = !invite.isNullOrBlank() && config != null && !invited
}

class SignInViewModel(private val session: SessionManager, private val baseUrl: String, invite: String?) : ViewModel() {
    private val _state = MutableStateFlow(SignInUiState(invite = invite, registering = invite != null))
    val state: StateFlow<SignInUiState> = _state.asStateFlow()

    fun loadConfig() {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).authConfig(_state.value.invite) }
                .onSuccess { config ->
                    _state.update {
                        it.copy(
                            config = config,
                            configError = null,
                            // A fresh server has no one to sign in as.
                            registering = it.registering || config.setup,
                        )
                    }
                }
                .onFailure { e -> _state.update { it.copy(configError = e.message) } }
        }
    }

    fun setRegistering(registering: Boolean) = _state.update { it.copy(registering = registering, error = null) }

    /** Accepts an invite link or a bare token, and re-resolves what it grants. */
    fun useInvite(raw: String) {
        val trimmed = raw.trim()
        val token = if ('/' in trimmed || '?' in trimmed) ServerUrl.parse(trimmed)?.invite else trimmed
        _state.update { it.copy(invite = token?.takeIf { t -> t.isNotBlank() }, error = null) }
        loadConfig()
    }

    fun login(email: String, password: String) = submit {
        session.api(baseUrl).login(LoginRequest(email.trim(), password))
    }

    fun register(email: String, username: String, password: String) {
        val problem = when {
            username.trim().length < 2 -> "Pick a username of at least 2 characters."
            password.length < 8 -> "Passwords need at least 8 characters."
            else -> null
        }
        if (problem != null) {
            _state.update { it.copy(error = problem) }
            return
        }
        val invite = _state.value.invite?.takeIf { _state.value.invited }
        submit { session.api(baseUrl).register(RegisterRequest(email.trim(), username.trim(), password, invite)) }
    }

    private fun submit(call: suspend () -> com.collinpendleton.backhog.api.User) {
        if (_state.value.busy) return
        _state.update { it.copy(busy = true, error = null) }
        viewModelScope.launch {
            apiCall(call)
                .onSuccess { user -> session.signedIn(user) }
                .onFailure { e -> _state.update { it.copy(error = (e as ApiError).message) } }
            _state.update { it.copy(busy = false) }
        }
    }
}
