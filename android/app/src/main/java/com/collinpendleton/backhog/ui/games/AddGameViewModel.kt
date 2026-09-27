package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.AddEntryRequest
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.SearchResult
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class AddGameUiState(
    val term: String = "",
    val results: List<SearchResult> = emptyList(),
    val searching: Boolean = false,
    /** Distinguishes "no matches for a real search" from the idle hint. */
    val searched: Boolean = false,
    /** 503: no IGDB creds on the server — the web's degraded message. */
    val degraded: Boolean = false,
    val error: String? = null,
    /** The game id whose add is in flight. */
    val adding: Long? = null,
    /** Games added this sitting — shown as "In library" so the sheet can stay open. */
    val added: Set<Long> = emptySet(),
) {
    fun owned(result: SearchResult): Boolean = result.inLibrary || result.game.id in added
}

/**
 * IGDB search → `POST /api/library`. The web's dialog adds straight to the
 * backlog; the app adds the one choice the web pushes to the detail page —
 * wishlist — as a second button, so the shopping list is reachable at the
 * moment of discovery.
 */
@OptIn(FlowPreview::class)
class AddGameViewModel(private val session: SessionManager, private val baseUrl: String) : ViewModel() {
    private val _state = MutableStateFlow(AddGameUiState())
    val state: StateFlow<AddGameUiState> = _state.asStateFlow()

    private val term = MutableStateFlow("")

    init {
        viewModelScope.launch {
            term.debounce(300).collect { runSearch() }
        }
    }

    fun setTerm(text: String) {
        _state.update { it.copy(term = text) }
        term.value = text
    }

    /** Retry after a transient failure (not the degraded mode — that needs server config). */
    fun retry() = runSearch()

    fun add(gameId: Long, status: EntryStatus) {
        if (_state.value.adding != null || _state.value.added.contains(gameId)) return
        _state.update { it.copy(adding = gameId, error = null) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).addEntry(AddEntryRequest(gameId = gameId, status = status)) }
                .onSuccess {
                    _state.update { it.copy(adding = null, added = it.added + gameId) }
                }
                .onFailure { e ->
                    val error = e as ApiError
                    // A race or a re-add: the outcome the user wanted is already true.
                    if (error.status == 409) {
                        _state.update { it.copy(adding = null, added = it.added + gameId) }
                    } else {
                        _state.update { it.copy(adding = null, error = error.message) }
                    }
                }
        }
    }

    private fun runSearch() {
        val query = term.value.trim()
        if (query.length < 2) {
            _state.update { it.copy(results = emptyList(), searched = false, searching = false, degraded = false, error = null) }
            return
        }
        _state.update { it.copy(searching = true, degraded = false, error = null) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).searchGames(query) }
                .onSuccess { response ->
                    _state.update {
                        it.copy(results = response.results, searched = true, searching = false)
                    }
                }
                .onFailure { e ->
                    val error = e as ApiError
                    _state.update {
                        it.copy(
                            searching = false,
                            searched = true,
                            degraded = error.status == 503,
                            error = if (error.status == 503) null else error.message,
                        )
                    }
                }
        }
    }
}
