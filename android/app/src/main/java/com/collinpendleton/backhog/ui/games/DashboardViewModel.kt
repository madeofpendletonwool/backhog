package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.Insights
import com.collinpendleton.backhog.api.Season
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class DashboardUiState(
    val insights: Insights? = null,
    val season: Season? = null,
    val loading: Boolean = true,
    val error: String? = null,
) {
    val hasLibrary: Boolean get() = (insights?.headline?.gamesOwned ?: 0) > 0
}

/** "Your Gaming Problem": the insights rollup plus the year's Backlog Challenge card. */
class DashboardViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(DashboardUiState())
    val state: StateFlow<DashboardUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).insights() }
                .onSuccess { insights ->
                    _state.update { it.copy(insights = insights, loading = false) }
                }
                .onFailure { e ->
                    _state.update { it.copy(loading = false, error = (e as ApiError).message) }
                }
            apiCall { session.api(baseUrl).season() }
                .onSuccess { season -> _state.update { it.copy(season = season) } }
        }
    }
}
