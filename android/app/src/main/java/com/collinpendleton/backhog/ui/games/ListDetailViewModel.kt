package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.ListItemsRequest
import com.collinpendleton.backhog.api.ListSummary
import com.collinpendleton.backhog.api.ReorderRequest
import com.collinpendleton.backhog.api.RuleSet
import com.collinpendleton.backhog.api.SmartField
import com.collinpendleton.backhog.api.SmartLists
import com.collinpendleton.backhog.api.apiCall
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class ListDetailUiState(
    val list: ListSummary? = null,
    val entries: List<Entry> = emptyList(),
    val loading: Boolean = true,
    val error: String? = null,
    val actionError: String? = null,
    val deleted: Boolean = false,
    /** Editing state: name draft + rules draft for smart lists. */
    val editing: Boolean = false,
    val draftName: String = "",
    val draftRules: RuleSet? = null,
    val fields: List<SmartField> = emptyList(),
    val busy: Boolean = false,
    /** Add-entries picker: candidate library entries not already members. */
    val adding: Boolean = false,
    val candidates: List<Entry> = emptyList(),
) {
    val isSmart: Boolean get() = list?.kind == "smart"
    val totalHours: Double get() = entries.sumOf { it.hours }
}

/**
 * One list, manual or smart: its entries, the reorder machinery for manual
 * lists, the rule editor for smart ones, and rename/delete for both.
 */
class ListDetailViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
    private val listId: String,
) : ViewModel() {
    private val _state = MutableStateFlow(ListDetailUiState())
    val state: StateFlow<ListDetailUiState> = _state.asStateFlow()

    init {
        load()
        viewModelScope.launch {
            apiCall { session.api(baseUrl).smartFields() }
                .onSuccess { response -> _state.update { it.copy(fields = response.fields) } }
        }
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).list(listId) }
                .onSuccess { response ->
                    _state.update {
                        it.copy(
                            list = response.list.copy(count = response.entries.size),
                            entries = response.entries,
                            loading = false,
                        )
                    }
                }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    fun startEdit() {
        val list = _state.value.list ?: return
        _state.update {
            it.copy(
                editing = true,
                draftName = list.name,
                draftRules = list.rules ?: RuleSet(match = "all"),
            )
        }
    }

    fun cancelEdit() = _state.update { it.copy(editing = false, draftName = "", draftRules = null) }

    fun setDraftName(name: String) = _state.update { it.copy(draftName = name) }

    fun setDraftRules(rules: RuleSet) = _state.update { it.copy(draftRules = rules) }

    fun saveEdit() {
        val current = _state.value
        if (current.busy || current.draftName.isBlank()) return
        _state.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            val body = buildJsonObject {
                put("name", current.draftName.trim())
                if (current.isSmart && current.draftRules != null) {
                    put("rules", SmartLists.encodeRuleSet(current.draftRules!!))
                }
            }
            apiCall { session.api(baseUrl).updateList(listId, body) }
                .onSuccess { list -> _state.update { it.copy(list = list, busy = false, editing = false) } }
                .onFailure { e -> _state.update { it.copy(busy = false, actionError = (e as ApiError).message) } }
        }
    }

    fun delete() {
        if (_state.value.busy) return
        _state.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).deleteList(listId) }
                .onSuccess { _state.update { it.copy(busy = false, deleted = true) } }
                .onFailure { e -> _state.update { it.copy(busy = false, actionError = (e as ApiError).message) } }
        }
    }

    fun removeEntry(entryId: String) {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).removeListItem(listId, entryId) }
                .onSuccess {
                    _state.update { s ->
                        s.copy(
                            entries = s.entries.filterNot { it.id == entryId },
                            list = s.list?.let { it.copy(count = (it.count - 1).coerceAtLeast(0)) },
                        )
                    }
                }
                .onFailure { e -> _state.update { it.copy(actionError = (e as ApiError).message) } }
        }
    }

    // --- manual reorder: optimistic move, then neighbours ---------------------

    fun moveLocal(from: Int, to: Int) {
        _state.update { it.copy(entries = QueueMoves.moved(it.entries, from, to)) }
    }

    fun commit(entryId: String) {
        val request = QueueMoves.requestFor(_state.value.entries, entryId) ?: return
        viewModelScope.launch {
            apiCall { session.api(baseUrl).reorderList(listId, request) }
                .onFailure { e ->
                    _state.update { it.copy(actionError = (e as ApiError).message) }
                    load()
                }
        }
    }

    // --- add-entries picker ----------------------------------------------------

    fun openAdd() {
        if (_state.value.adding) return
        _state.update { it.copy(adding = true, actionError = null) }
        viewModelScope.launch {
            apiCall { session.api(baseUrl).library(media = "game", limit = 500) }
                .onSuccess { response ->
                    _state.update { s ->
                        s.copy(candidates = response.entries.filter { e -> e.id !in s.entries.map { it.id } && e.status != EntryStatus.Wishlist })
                    }
                }
                .onFailure { e -> _state.update { it.copy(adding = false, actionError = (e as ApiError).message) } }
        }
    }

    fun closeAdd() = _state.update { it.copy(adding = false, candidates = emptyList()) }

    fun addEntries(ids: List<String>) {
        if (ids.isEmpty()) return
        viewModelScope.launch {
            apiCall { session.api(baseUrl).addListItem(listId, ListItemsRequest(entryIds = ids)) }
                .onSuccess {
                    _state.update { it.copy(adding = false, candidates = emptyList()) }
                    load()
                }
                .onFailure { e -> _state.update { it.copy(actionError = (e as ApiError).message) } }
        }
    }
}
