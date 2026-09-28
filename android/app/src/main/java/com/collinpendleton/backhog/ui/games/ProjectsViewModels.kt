package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.CreateProjectRequest
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.ListItemsRequest
import com.collinpendleton.backhog.api.Project
import com.collinpendleton.backhog.api.ProjectItem
import com.collinpendleton.backhog.api.RuleSet
import com.collinpendleton.backhog.api.SmartField
import com.collinpendleton.backhog.api.SmartLists
import com.collinpendleton.backhog.api.apiCall
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class ProjectsUiState(
    val projects: List<Project> = emptyList(),
    val loading: Boolean = true,
    val error: String? = null,
) {
    /** Projects are arena-scoped at creation; the games arena sees its own. */
    val forGames: List<Project> get() = projects.filter { it.mediaScope == "game" }
    val active: List<Project> get() = forGames.filter { it.completedAt == null }
    val completed: List<Project> get() = forGames.filter { it.completedAt != null }
}

data class CreateProjectUiState(
    val open: Boolean = false,
    val kind: String = "checklist",
    val name: String = "",
    val description: String = "",
    val targetCount: String = "",
    val rules: RuleSet = SmartLists.defaultRules("game"),
    val fields: List<SmartField> = emptyList(),
    val busy: Boolean = false,
    val error: String? = null,
) {
    val wantsTarget: Boolean get() = kind == "count_goal" || kind == "rule_goal"
    val wantsRules: Boolean get() = kind == "rule_goal"
}

/** The projects index plus the create flow — kinds checklist / count_goal / rule_goal. */
class ProjectsViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
) : ViewModel() {
    private val _state = MutableStateFlow(ProjectsUiState())
    val state: StateFlow<ProjectsUiState> = _state.asStateFlow()

    private val _create = MutableStateFlow(CreateProjectUiState())
    val create: StateFlow<CreateProjectUiState> = _create.asStateFlow()

    init {
        load()
        loadFields()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).projects() }
                .onSuccess { response -> _state.update { s -> s.copy(projects = response.projects, loading = false) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    private fun loadFields() {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).smartFields() }
                .onSuccess { response -> _create.update { it.copy(fields = response.fields) } }
        }
    }

    fun openCreate() = _create.update { CreateProjectUiState(open = true) }

    fun closeCreate() = _create.update { CreateProjectUiState() }

    fun setCreateKind(kind: String) = _create.update { it.copy(kind = kind) }
    fun setCreateName(name: String) = _create.update { it.copy(name = name) }
    fun setCreateDescription(description: String) = _create.update { it.copy(description = description) }
    fun setCreateTarget(target: String) = _create.update { it.copy(targetCount = target.filter { c -> c.isDigit() }) }
    fun setCreateRules(rules: RuleSet) = _create.update { it.copy(rules = rules) }

    fun submitCreate(onCreated: (String) -> Unit) {
        val current = _create.value
        if (current.busy || current.name.isBlank()) return
        _create.update { it.copy(busy = true, error = null) }
        viewModelScope.launch {
            apiCall {
                session.api(baseUrl).createProject(
                    CreateProjectRequest(
                        name = current.name.trim(),
                        description = current.description.trim(),
                        kind = current.kind,
                        media = "game",
                        targetCount = current.targetCount.toIntOrNull(),
                        rules = if (current.wantsRules) current.rules else null,
                    ),
                )
            }
                .onSuccess { project ->
                    _create.update { CreateProjectUiState() }
                    _state.update { s -> s.copy(projects = s.projects + project) }
                    onCreated(project.id)
                }
                .onFailure { e -> _create.update { it.copy(busy = false, error = (e as ApiError).message) } }
        }
    }
}

data class ProjectDetailUiState(
    val project: Project? = null,
    val items: List<ProjectItem> = emptyList(),
    val loading: Boolean = true,
    val error: String? = null,
    val actionError: String? = null,
    val deleted: Boolean = false,
    /** The entry whose done-toggle is in flight. */
    val toggling: String? = null,
) {
    val isChecklist: Boolean get() = project?.kind == "checklist"
    val isRuleGoal: Boolean get() = project?.kind == "rule_goal"
    val complete: Boolean get() = project?.completedAt != null
}

/** One project: the progress panel, the checklist rows, or the rule match pool. */
class ProjectDetailViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
    private val projectId: String,
) : ViewModel() {
    private val _state = MutableStateFlow(ProjectDetailUiState())
    val state: StateFlow<ProjectDetailUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { session.api(baseUrl).project(projectId) }
                .onSuccess { response ->
                    _state.update { it.copy(project = response.project, items = response.items, loading = false) }
                }
                .onFailure { e -> _state.update { it.copy(loading = false, error = (e as ApiError).message) } }
        }
    }

    /** The manual per-item override: true/false force it, null returns to status-derived. */
    fun setDone(entryId: String, done: Boolean?) {
        if (_state.value.toggling != null) return
        _state.update { it.copy(toggling = entryId, actionError = null) }
        viewModelScope.launch {
            val element: JsonElement = done?.let { JsonPrimitive(it) } ?: JsonNull
            val body = buildJsonObject { put("done", element) }
            apiCall { session.api(baseUrl).setProjectItemDone(projectId, entryId, body) }
                .onSuccess {
                    _state.update { s ->
                        s.copy(
                            toggling = null,
                            items = s.items.map { item ->
                                if (item.entry.id == entryId) item.copy(done = done) else item
                            },
                        )
                    }
                    refreshProgress()
                }
                .onFailure { e -> _state.update { it.copy(toggling = null, actionError = (e as ApiError).message) } }
        }
    }

    fun removeItem(entryId: String) {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).removeProjectItem(projectId, entryId) }
                .onSuccess {
                    _state.update { s -> s.copy(items = s.items.filterNot { it.entry.id == entryId }) }
                    refreshProgress()
                }
                .onFailure { e -> _state.update { it.copy(actionError = (e as ApiError).message) } }
        }
    }

    fun addItems(ids: List<String>) {
        if (ids.isEmpty()) return
        viewModelScope.launch {
            apiCall { session.api(baseUrl).addProjectItem(projectId, ListItemsRequest(entryIds = ids)) }
                .onSuccess { load() }
                .onFailure { e -> _state.update { it.copy(actionError = (e as ApiError).message) } }
        }
    }

    fun moveLocal(from: Int, to: Int) {
        _state.update { s ->
            s.copy(items = s.items.toMutableList().apply { add(to, removeAt(from)) })
        }
    }

    fun commit(entryId: String) {
        val items = _state.value.items
        val index = items.indexOfFirst { it.entry.id == entryId }
        if (index < 0) return
        val request = com.collinpendleton.backhog.api.ReorderRequest(
            entryId = entryId,
            beforeId = items.getOrNull(index - 1)?.entry?.id ?: "",
            afterId = items.getOrNull(index + 1)?.entry?.id ?: "",
        )
        viewModelScope.launch {
            apiCall { session.api(baseUrl).reorderProject(projectId, request) }
                .onFailure { e ->
                    _state.update { it.copy(actionError = (e as ApiError).message) }
                    load()
                }
        }
    }

    /** Mark done / reopen — the manual close of the target. */
    fun toggleComplete() {
        val project = _state.value.project ?: return
        viewModelScope.launch {
            val body = buildJsonObject { put("completed", project.completedAt == null) }
            apiCall { session.api(baseUrl).updateProject(projectId, body) }
                .onSuccess { updated ->
                    _state.update { s -> s.copy(project = updated) }
                }
                .onFailure { e -> _state.update { it.copy(actionError = (e as ApiError).message) } }
        }
    }

    /** Edit the target count from the detail screen. */
    fun setTarget(count: Int?) {
        viewModelScope.launch {
            val element: JsonElement = count?.let { JsonPrimitive(it) } ?: JsonNull
            val body = buildJsonObject { put("target_count", element) }
            apiCall { session.api(baseUrl).updateProject(projectId, body) }
                .onSuccess { updated -> _state.update { s -> s.copy(project = updated) } }
                .onFailure { e -> _state.update { it.copy(actionError = (e as ApiError).message) } }
        }
    }

    fun delete() {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).deleteProject(projectId) }
                .onSuccess { _state.update { it.copy(deleted = true) } }
                .onFailure { e -> _state.update { it.copy(actionError = (e as ApiError).message) } }
        }
    }

    /** Recompute the header's derived counts after a membership change. */
    private fun refreshProgress() {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).project(projectId) }
                .onSuccess { response -> _state.update { it.copy(project = response.project) } }
        }
    }

    /** Candidates for the add picker: the games library, minus current members. */
    fun loadCandidates(onLoaded: (List<Entry>) -> Unit) {
        viewModelScope.launch {
            apiCall { session.api(baseUrl).library(media = "game", limit = 500) }
                .onSuccess { response ->
                    onLoaded(
                        response.entries.filter { e ->
                            e.id !in _state.value.items.map { it.entry.id } && e.status != EntryStatus.Wishlist
                        },
                    )
                }
        }
    }
}
