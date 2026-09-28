package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.BulkAddRequest
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.BulkAddResponse
import com.collinpendleton.backhog.api.SteamPreviewRequest
import com.collinpendleton.backhog.api.SteamPreviewResponse
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class SteamImportUiState(
    val steamId: String = "",
    val status: EntryStatus = EntryStatus.Backlog,
    /** healthz said the server has no Steam key — the not-configured gate. */
    val unavailable: Boolean = false,
    val preview: SteamPreviewResponse? = null,
    val previewing: Boolean = false,
    val previewError: String? = null,
    /** App-ids ticked for import, preselected to everything importable. */
    val selected: Set<Long> = emptySet(),
    val importing: Boolean = false,
    val result: BulkAddResponse? = null,
) {
    val matches get() = preview?.matches ?: emptyList()
}

/**
 * The Steam import: resolve a profile, map its games onto IGDB appid-exact,
 * let the user prune the selection, then bulk-add. Nothing is written until
 * the confirm step.
 */
class SteamImportViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(SteamImportUiState())
    val state: StateFlow<SteamImportUiState> = _state.asStateFlow()

    init {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).health() }
                .onSuccess { health -> _state.update { it.copy(unavailable = !health.steam) } }
        }
    }

    fun setSteamId(id: String) = _state.update { it.copy(steamId = id) }

    fun setStatus(status: EntryStatus) = _state.update { it.copy(status = status) }

    fun preview() {
        val id = _state.value.steamId.trim()
        if (id.isEmpty() || _state.value.previewing) return
        _state.update { it.copy(previewing = true, previewError = null, preview = null) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).steamPreview(SteamPreviewRequest(id)) }
                .onSuccess { response ->
                    // Preselect everything importable; deselecting a few is less work
                    // than ticking two hundred boxes.
                    val preselect = response.matches
                        .filter { it.game != null && !it.inLibrary }
                        .mapNotNull { it.game?.id }
                        .toSet()
                    _state.update { it.copy(previewing = false, preview = response, selected = preselect) }
                }
                .onFailure { e -> _state.update { it.copy(previewing = false, previewError = (e as ApiError).message) } }
        }
    }

    fun toggle(gameId: Long) {
        _state.update { s ->
            s.copy(selected = if (gameId in s.selected) s.selected - gameId else s.selected + gameId)
        }
    }

    fun selectAll() {
        _state.update { s ->
            s.copy(selected = s.matches.filter { it.game != null && !it.inLibrary }.mapNotNull { it.game?.id }.toSet())
        }
    }

    fun selectNone() = _state.update { it.copy(selected = emptySet()) }

    fun runImport(onLibraryChanged: () -> Unit) {
        val current = _state.value
        if (current.importing || current.selected.isEmpty()) return
        _state.update { it.copy(importing = true) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).bulkAdd(BulkAddRequest(gameIds = current.selected.toList(), status = current.status)) }
                .onSuccess { response ->
                    _state.update { it.copy(importing = false, result = response) }
                    onLibraryChanged()
                }
                .onFailure { e -> _state.update { it.copy(importing = false, previewError = (e as ApiError).message) } }
        }
    }
}
