package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.CreateListRequest
import com.collinpendleton.backhog.api.ListSummary
import com.collinpendleton.backhog.api.RuleSet
import com.collinpendleton.backhog.api.SmartField
import com.collinpendleton.backhog.api.SmartLists
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class ListsUiState(
    val lists: List<ListSummary> = emptyList(),
    val loading: Boolean = true,
    val error: String? = null,
) {
    /** The games arena's slice: manual lists are shared; smart lists carry their arena in their rules. */
    val forGames: List<ListSummary>
        get() = lists.filter {
            it.kind != "smart" || SmartLists.ruleSetTarget(it.rules) == null ||
                SmartLists.ruleSetTarget(it.rules) == "game"
        }
    val smart: List<ListSummary> get() = forGames.filter { it.kind == "smart" }
    val manual: List<ListSummary> get() = forGames.filter { it.kind == "manual" }
}

data class CreateListUiState(
    val open: Boolean = false,
    val kind: String = "manual",
    val name: String = "",
    val description: String = "",
    val rules: RuleSet = SmartLists.defaultRules("game"),
    val fields: List<SmartField> = emptyList(),
    val busy: Boolean = false,
    val error: String? = null,
)

/** The lists index plus the create-list flow it owns. */
class ListsViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(ListsUiState())
    val state: StateFlow<ListsUiState> = _state.asStateFlow()

    private val _create = MutableStateFlow(CreateListUiState())
    val create: StateFlow<CreateListUiState> = _create.asStateFlow()

    init {
        load()
        loadFields()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).lists() }
                .onSuccess { response -> _state.update { s -> s.copy(lists = response.lists, loading = false) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    private fun loadFields() {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).smartFields() }
                .onSuccess { response -> _create.update { it.copy(fields = response.fields) } }
        }
    }

    fun openCreate() = _create.update { CreateListUiState(open = true) }

    fun closeCreate() = _create.update { CreateListUiState() }

    fun setCreateKind(kind: String) = _create.update { it.copy(kind = kind) }
    fun setCreateName(name: String) = _create.update { it.copy(name = name) }
    fun setCreateDescription(description: String) = _create.update { it.copy(description = description) }
    fun setCreateRules(rules: RuleSet) = _create.update { it.copy(rules = rules) }

    fun submitCreate(onCreated: (String) -> Unit) {
        val current = _create.value
        if (current.busy || current.name.isBlank()) return
        _create.update { it.copy(busy = true, error = null) }
        viewModelScope.launch {
            apiCall {
                session.api(baseUrl).createList(
                    CreateListRequest(
                        name = current.name.trim(),
                        description = current.description.trim(),
                        kind = current.kind,
                        rules = if (current.kind == "smart") current.rules else null,
                    ),
                )
            }
                .onSuccess { list ->
                    _create.update { CreateListUiState() }
                    _state.update { s -> s.copy(lists = s.lists + list) }
                    onCreated(list.id)
                }
                .onFailure { e -> _create.update { it.copy(busy = false, error = (e as ApiError).message) } }
        }
    }
}
