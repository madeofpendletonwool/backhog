package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.SeriesDetail
import com.collinpendleton.backhog.api.SeriesSummary
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

data class SeriesUiState(
    val series: List<SeriesSummary> = emptyList(),
    val loading: Boolean = true,
    val error: String? = null,
    /** An IGDB enrichment walk is running — poll while it fills in. */
    val backfillRunning: Boolean = false,
    val kicking: Boolean = false,
)

/** The series index: every franchise with two or more owned games, rolled up. */
class SeriesViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(SeriesUiState())
    val state: StateFlow<SeriesUiState> = _state.asStateFlow()

    init {
        load()
        checkBackfill()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).series() }
                .onSuccess { response -> _state.update { s -> s.copy(series = response.series, loading = false) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    private fun checkBackfill() {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).seriesBackfillStatus() }
                .onSuccess { response ->
                    _state.update { it.copy(backfillRunning = response.running) }
                    if (response.running) pollWhileRunning()
                }
        }
    }

    /** The enrichment walk's trigger — its per-run cap is the server's 500 games. */
    fun kickBackfill() {
        if (_state.value.kicking) return
        _state.update { it.copy(kicking = true) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).kickSeriesBackfill() }
                .onSuccess { response ->
                    _state.update { it.copy(kicking = false, backfillRunning = true) }
                    if (_state.value.backfillRunning) pollWhileRunning()
                }
                .onFailure { e -> _state.update { it.copy(kicking = false, error = (e as ApiError).message) } }
        }
    }

    private fun pollWhileRunning() {
        viewModelScope.launch {
            while (isActive) {
                delay(5000)
                val status = apiCall { session.api(baseUrl).seriesBackfillStatus() }.getOrNull() ?: continue
                if (!status.running) {
                    _state.update { it.copy(backfillRunning = false) }
                    load()
                    break
                }
                // Cards appear as IGDB data lands.
                load()
            }
        }
    }
}

data class SeriesDetailUiState(
    val detail: SeriesDetail? = null,
    val loading: Boolean = true,
    val error: String? = null,
    val actionError: String? = null,
    /** The good-ones floor reveal, local like the web's showAll. */
    val showAll: Boolean = false,
)

/** One series as a journey, in the user's chosen play order. */
class SeriesDetailViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
    private val seriesId: String,
) : ViewModel() {
    private val _state = MutableStateFlow(SeriesDetailUiState())
    val state: StateFlow<SeriesDetailUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).seriesDetail(seriesId) }
                .onSuccess { detail -> _state.update { it.copy(detail = detail, loading = false) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    fun setShowAll(showAll: Boolean) = _state.update { it.copy(showAll = showAll) }

    /** Switch play order — release / chronological / recommended / custom / good_ones. */
    fun setPlayOrder(order: String) {
        val detail = _state.value.detail ?: return
        if (detail.playOrder == order) return
        _state.update { s -> s.copy(detail = s.detail?.copy(playOrder = order)) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).setSeriesPlayOrder(seriesId, com.collinpendleton.backhog.api.PlayOrderRequest(order)) }
                .onFailure { e ->
                    _state.update { it.copy(actionError = (e as ApiError).message) }
                    load()
                }
                .onSuccess { load() }
        }
    }

    // --- custom order: optimistic move by game id, then neighbours ------------

    fun moveLocal(from: Int, to: Int) {
        val detail = _state.value.detail ?: return
        val members = detail.members.toMutableList().apply { add(to, removeAt(from)) }
        _state.update { s -> s.copy(detail = s.detail?.copy(members = members)) }
    }

    fun commit(gameId: Long) {
        val members = _state.value.detail?.members ?: return
        val index = members.indexOfFirst { it.game.id == gameId }
        if (index < 0) return
        val request = com.collinpendleton.backhog.api.SeriesReorderRequest(
            gameId = gameId,
            beforeId = members.getOrNull(index - 1)?.game?.id ?: 0,
            afterId = members.getOrNull(index + 1)?.game?.id ?: 0,
        )
        viewModelScope.launch {
            apiCall { session.api(baseUrl).reorderSeries(seriesId, request) }
                .onFailure { e ->
                    _state.update { it.copy(actionError = (e as ApiError).message) }
                    load()
                }
        }
    }
}
