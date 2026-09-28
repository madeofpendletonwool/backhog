package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.DebtReport
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class DebtUiState(
    val debt: DebtReport? = null,
    val loading: Boolean = true,
    val error: String? = null,
)

/** The backlog-debt report: what you owe yourself, and when it gets paid off. */
class DebtViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(DebtUiState())
    val state: StateFlow<DebtUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).debt() }
                .onSuccess { debt -> _state.update { it.copy(debt = debt, loading = false) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }
}
