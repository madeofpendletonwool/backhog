package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.PlaySession
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.api.AddSessionRequest
import com.collinpendleton.backhog.api.entryPatch
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.time.LocalTime

data class GameDetailUiState(
    val entry: Entry? = null,
    val loading: Boolean = true,
    val error: String? = null,
    /** Non-patch failures: shown inline without hiding the page. */
    val actionError: String? = null,
    val busy: Boolean = false,
    val sessions: List<PlaySession> = emptyList(),
    /** The names of the lists this entry belongs to, manual or smart. */
    val listNames: List<String> = emptyList(),
    val notesDraft: String? = null,
    val deleted: Boolean = false,
) {
    val totalMinutes: Int get() = sessions.sumOf { it.minutes }
    val notesDirty: Boolean get() = notesDraft != null && notesDraft != entry?.notes
}

/**
 * One library entry's page: the game dossier plus the user's column — status,
 * rating, notes, platform, sessions, lists membership — everything the web's
 * GameDetailPage renders. Status transitions ride the server's automatic
 * start/finish timestamps; the client never writes them.
 */
class GameDetailViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
    private val entryId: String,
) : ViewModel() {
    private val _state = MutableStateFlow(GameDetailUiState())
    val state: StateFlow<GameDetailUiState> = _state.asStateFlow()

    /** Injectable so the night-owl test can pin the hour. */
    internal var nowHour: () -> Int = { LocalTime.now().hour }

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).entry(entryId) }
                .onSuccess { entry ->
                    _state.update { it.copy(entry = entry, loading = false, notesDraft = entry.notes) }
                    loadSessions()
                    loadListNames()
                }
                .onFailure { e ->
                    _state.update { it.copy(loading = false, error = (e as ApiError).message) }
                }
        }
    }

    fun patch(block: com.collinpendleton.backhog.api.EntryPatch.() -> Unit) {
        if (_state.value.busy) return
        _state.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).patchEntry(entryId, entryPatch(block)) }
                .onSuccess { response ->
                    _state.update {
                        it.copy(
                            entry = response.entry,
                            busy = false,
                            // A patch can't change notes in this UI except through saveNotes.
                            notesDraft = response.entry.notes,
                        )
                    }
                }
                .onFailure { e ->
                    _state.update { it.copy(busy = false, actionError = (e as ApiError).message) }
                }
        }
    }

    fun setNotesDraft(text: String) = _state.update { it.copy(notesDraft = text) }

    fun saveNotes() {
        val draft = _state.value.notesDraft ?: return
        patch { notes(draft) }
    }

    fun addSession(minutes: Int, playedOn: String?, note: String?) {
        if (_state.value.busy) return
        _state.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).addSession(entryId, AddSessionRequest(minutes, playedOn, note?.takeIf { s -> s.isNotBlank() })) }
                .onSuccess { response ->
                    _state.update { it.copy(busy = false, sessions = listOf(response.session) + it.sessions) }
                    // Logging a session auto-flips backlog/wishlist to playing server-side;
                    // refresh so the status column reflects it.
                    refreshEntryOnly()
                    maybeNightOwl()
                }
                .onFailure { e ->
                    _state.update { it.copy(busy = false, actionError = (e as ApiError).message) }
                }
        }
    }

    fun deleteSession(sessionId: String) {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).deleteSession(sessionId) }
                .onSuccess { _state.update { s -> s.copy(sessions = s.sessions.filterNot { it.id == sessionId }) } }
                .onFailure { e -> _state.update { it.copy(actionError = (e as ApiError).message) } }
        }
    }

    fun delete() {
        if (_state.value.busy) return
        _state.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).deleteEntry(entryId) }
                .onSuccess { _state.update { it.copy(busy = false, deleted = true) } }
                .onFailure { e -> _state.update { it.copy(busy = false, actionError = (e as ApiError).message) } }
        }
    }

    private fun refreshEntryOnly() {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).entry(entryId) }
                .onSuccess { entry -> _state.update { it.copy(entry = entry) } }
        }
    }

    private suspend fun loadSessions() {
        apiCall { session.api(baseUrl).sessions(entryId) }
            .onSuccess { response -> _state.update { it.copy(sessions = response.sessions) } }
    }

    private suspend fun loadListNames() {
        val api = session.api(baseUrl)
        val memberships = apiCall { api.entryLists(entryId) }.getOrNull()?.listIds ?: return
        if (memberships.isEmpty()) {
            _state.update { it.copy(listNames = emptyList()) }
            return
        }
        val names = apiCall { api.lists() }.getOrNull()?.lists
            ?.filter { it.id in memberships }
            ?.map { it.name }
            .orEmpty()
        _state.update { it.copy(listNames = names) }
    }

    /**
     * The night-owl egg is judged by the user's local clock at logging time,
     * on purpose (web `useSessions.ts`): 3:00–4:59 AM counts as night owl.
     * Idempotent and rate-limited server-side; the result surfaces in Stage 3.
     */
    private fun maybeNightOwl() {
        val hour = nowHour()
        if (hour !in 3..4) return
        viewModelScope.launch {
            apiCall { session.api(baseUrl).egg("night_owl") }
        }
    }
}
