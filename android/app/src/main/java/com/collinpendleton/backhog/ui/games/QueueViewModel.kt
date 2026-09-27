package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.api.entryPatch
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class QueueUiState(
    val entries: List<Entry> = emptyList(),
    val loading: Boolean = true,
    val error: String? = null,
    /** A rejected move: local order was rolled back and the banner explains. */
    val moveError: Boolean = false,
    /** The entry id whose status flip is in flight (Start / Done from a row). */
    val marking: String? = null,
) {
    /** "How deep am I": Σ time-to-beat of the games; books contribute nothing. */
    val totalHours: Double get() = entries.sumOf { it.hours }

    /** Running total through `index` — "if I play everything down to here". */
    fun cumulativeHours(index: Int): Double = entries.subList(0, index + 1).sumOf { it.hours }
}

/**
 * The play queue: every backlog entry across both arenas, ordered. Moves are
 * optimistic — reorder locally, then tell the server the new neighbours — and
 * roll back with a banner if the server refuses, exactly the web's behaviour.
 */
class QueueViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(QueueUiState())
    val state: StateFlow<QueueUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).queue() }
                .onSuccess { response -> _state.update { it.copy(entries = response.entries, loading = false) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    fun dismissMoveError() = _state.update { it.copy(moveError = false) }

    /** Local-only reorder — the drag gesture calls this continuously as the row moves. */
    fun moveLocal(from: Int, to: Int) {
        _state.update { it.copy(entries = QueueMoves.moved(it.entries, from, to)) }
    }

    /** A quick-move button: one local move plus the server call for it. */
    fun move(from: Int, to: Int) {
        if (from == to) return
        val entryId = _state.value.entries.getOrNull(from)?.id ?: return
        moveLocal(from, to)
        commit(entryId)
    }

    /** Persist an entry's position against its current neighbours (drag end). */
    fun commit(entryId: String) {
        val snapshot = _state.value.entries
        val request = QueueMoves.requestFor(snapshot, entryId) ?: return
        viewModelScope.launch {
            apiCall { session.api(baseUrl).reorder(request) }
                .onFailure {
                    _state.update { current ->
                        if (current.entries.map { e -> e.id } == snapshot.map { e -> e.id }) {
                            current.copy(entries = snapshot, moveError = true)
                        } else {
                            current.copy(moveError = true)
                        }
                    }
                }
        }
    }

    /** Start playing: leaves the queue (the server nulls the position) and auto-stamps started_at. */
    fun markPlaying(entryId: String) = flip(entryId, EntryStatus.Playing)

    /**
     * Mark finished from the row — the web's next_up achievement path. The
     * server stamps finished_at and fires the unlock; toasts arrive in Stage 3.
     */
    fun markFinished(entryId: String) = flip(entryId, EntryStatus.Played)

    private fun flip(entryId: String, status: EntryStatus) {
        if (_state.value.marking != null) return
        _state.update { it.copy(marking = entryId) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).patchEntry(entryId, entryPatch { status(status) }) }
                .onSuccess {
                    _state.update { current ->
                        current.copy(entries = current.entries.filterNot { e -> e.id == entryId }, marking = null)
                    }
                }
                .onFailure { e ->
                    _state.update { it.copy(marking = null, moveError = false, error = (e as ApiError).message) }
                }
        }
    }
}
