package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.TonightPicksResult
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.api.entryPatch
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class TonightUiState(
    val minutes: Int = 90,
    val picks: TonightPicksResult? = null,
    val loading: Boolean = false,
    val error: String? = null,
    /** The random roll's answer, if any — "Just roll one". */
    val rolled: Entry? = null,
    val rolling: Boolean = false,
    /** The entry whose Play-it patch is in flight. */
    val playing: String? = null,
    /** Re-roll state: ids each category has already shown and excluded. */
    val excludes: Map<String, List<String>> = emptyMap(),
) {
    /** A fresh budget starts from a clean slate of candidates, like the web. */
    fun forMinutes(newMinutes: Int): TonightUiState =
        copy(minutes = newMinutes, picks = null, excludes = emptyMap())

    /** The flat exclude list for the next fetch. */
    val flatExcludes: List<String> get() = excludes.values.flatten().distinct()
}

/**
 * "What should I play tonight?" — the anti-deliberation device. Every pick is
 * scored and explained server-side; this fetches per budget and re-rolls a
 * category by excluding what it just showed.
 */
class TonightViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(TonightUiState())
    val state: StateFlow<TonightUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun setMinutes(minutes: Int) {
        if (minutes == _state.value.minutes) return
        _state.update { it.forMinutes(minutes) }
        load()
    }

    fun load() {
        val current = _state.value
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).tonight(current.minutes, current.flatExcludes.takeIf { it.isNotEmpty() }?.joinToString(",")) }
                .onSuccess { picks -> _state.update { it.copy(picks = picks, loading = false) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    /** A category's re-roll: exclude what it just showed and ask again. */
    fun reroll(category: String) {
        val pick = _state.value.picks?.let { picksFor(it, category) } ?: return
        _state.update { s ->
            s.copy(excludes = s.excludes + (category to (s.excludes[category] ?: emptyList()) + pick.entry.id))
        }
        load()
    }

    fun roll() {
        viewModelScope.launch {
            _state.update { it.copy(rolling = true) }
            apiCall { session.api(baseUrl).pick() }
                .onSuccess { entry -> _state.update { it.copy(rolling = false, rolled = entry) } }
                .onFailure { e -> _state.update { it.copy(rolling = false, error = (e as ApiError).message) } }
        }
    }

    /** "Play it": mark playing, then hand the entry back for navigation. */
    fun play(entry: Entry, onPlayed: (Entry) -> Unit) {
        if (_state.value.playing != null) return
        _state.update { it.copy(playing = entry.id) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).patchEntry(entry.id, entryPatch { status(com.collinpendleton.backhog.api.EntryStatus.Playing) }) }
                .onSuccess {
                    _state.update { it.copy(playing = null) }
                    onPlayed(entry)
                }
                .onFailure { e -> _state.update { it.copy(playing = null, error = (e as ApiError).message) } }
        }
    }

    private fun picksFor(picks: TonightPicksResult, category: String) = when (category) {
        "continue" -> picks.continuePick
        "short_win" -> picks.shortWin
        "wildcard" -> picks.wildcard
        else -> picks.rescue
    }
}
