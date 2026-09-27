package com.collinpendleton.backhog.ui.books

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.BookFacets
import com.collinpendleton.backhog.api.BookStats
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.data.BookShelfState
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** The shelf's sorts, matching the web's set. */
val SHELF_SORTS = listOf(
    "title" to "Title A–Z",
    "author" to "Author A–Z",
    "published" to "Newest first",
    "pages" to "Shortest first",
    "updated" to "Recently read",
)

data class BookLibraryState(
    val entries: List<Entry> = emptyList(),
    val total: Int = 0,
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
    val error: String? = null,
    val stats: BookStats? = null,
    val facets: BookFacets? = null,
    val shelf: BookShelfState = BookShelfState(),
    /** The free-text search box; debounced before it becomes a query. */
    val search: String = "",
    /** True once the remembered shelf state has arrived (and can be applied). */
    val restored: Boolean = false,
) {
    val hasMore: Boolean get() = entries.size < total
    val hasFilters: Boolean
        get() = shelf.status.isNotEmpty() || search.isNotBlank() ||
            shelf.author.isNotEmpty() || shelf.subject.isNotEmpty() || shelf.language.isNotEmpty()
}

private const val PAGE_SIZE = 24

/**
 * The shelf: the shared library routes with `media=book`. Filters, sort and
 * view are remembered in DataStore — the same behaviour the web gives between
 * visits.
 */
class BookLibraryViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
) : ViewModel() {

    private val _state = MutableStateFlow(BookLibraryState())
    val state: StateFlow<BookLibraryState> = _state.asStateFlow()

    private val queries = MutableStateFlow(0L)
    private var queryId = 0L

    init {
        // Remembered face first, then the first load against it. Our own
        // writes come back through this collect too — that is the reload
        // path — but a change that cannot alter the query (grid ↔ table)
        // does not re-ask the server.
        viewModelScope.launch {
            var previous: BookShelfState? = null
            container.preferences.bookShelf.collect { shelf ->
                val before = previous
                previous = shelf
                _state.update { it.copy(shelf = shelf, restored = true) }
                val queryMoved = before == null ||
                    before.status != shelf.status || before.sort != shelf.sort ||
                    before.author != shelf.author || before.subject != shelf.subject ||
                    before.language != shelf.language
                if (queryMoved) reload()
            }
        }
        viewModelScope.launch {
            apiCall { api().bookStats() }
                .onSuccess { stats -> _state.update { it.copy(stats = stats) } }
        }
        viewModelScope.launch {
            apiCall { api().bookFacets() }
                .onSuccess { facets -> _state.update { it.copy(facets = facets) } }
        }
        // The search box is debounced into the same reload path as the filters.
        @OptIn(FlowPreview::class)
        viewModelScope.launch {
            _state
                .debounce { if (it.restored) 250L else Long.MAX_VALUE }
                .distinctUntilChanged { before, after -> before.search == after.search }
                .collect { if (it.restored) reload() }
        }
    }

    private fun api() = container.session.api(baseUrl)

    private suspend fun load(offset: Int): Pair<List<Entry>, Int>? {
        val s = _state.value
        val id = ++queryId
        val result = apiCall {
            api().library(
                status = s.shelf.status.ifEmpty { null },
                query = s.search.trim().ifEmpty { null },
                sort = s.shelf.sort,
                author = s.shelf.author.ifEmpty { null },
                subject = s.shelf.subject.ifEmpty { null },
                language = s.shelf.language.ifEmpty { null },
                limit = PAGE_SIZE,
                offset = offset,
            )
        }
        // A stale page from a superseded query is dropped, not appended.
        return if (id == queryId) result.getOrNull()?.let { it.entries to it.total } else null
    }

    private fun reload() {
        if (!_state.value.restored) return
        _state.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            val loaded = load(0)
            if (loaded == null) {
                if (_state.value.loading) _state.update { it.copy(loading = false) }
                return@launch
            }
            val (entries, total) = loaded
            _state.update { it.copy(entries = entries, total = total, loading = false, loadingMore = false) }
            apiCall { api().bookStats() }.onSuccess { stats -> _state.update { it.copy(stats = stats) } }
        }
    }

    fun loadMore() {
        val s = _state.value
        if (s.loading || s.loadingMore || !s.hasMore) return
        _state.update { it.copy(loadingMore = true) }
        viewModelScope.launch {
            val loaded = load(s.entries.size)
            if (loaded == null) {
                _state.update { it.copy(loadingMore = false) }
                return@launch
            }
            val (page, total) = loaded
            _state.update {
                it.copy(
                    entries = it.entries + page.filter { e -> it.entries.none { old -> old.id == e.id } },
                    total = total,
                    loadingMore = false,
                )
            }
        }
    }

    fun setSearch(text: String) = _state.update { it.copy(search = text) }

    fun setStatus(status: String) {
        persist { it.copy(status = status) }
        // The books you are reading are wanted in the order you last touched
        // them, not alphabetically; the sort stays changeable.
        if (status == "playing") persist { it.copy(sort = "updated") }
    }

    fun setSort(sort: String) = persist { it.copy(sort = sort) }
    fun setAuthor(author: String) = persist { it.copy(author = author) }
    fun setSubject(subject: String) = persist { it.copy(subject = subject) }
    fun setLanguage(language: String) = persist { it.copy(language = language) }
    fun setGrid(grid: Boolean) = persist { it.copy(grid = grid) }

    fun clearFilters() {
        _state.update { it.copy(search = "") }
        persist { it.copy(status = "", author = "", subject = "", language = "") }
    }

    private fun persist(change: (BookShelfState) -> BookShelfState) {
        val next = change(_state.value.shelf)
        _state.update { it.copy(shelf = next) }
        viewModelScope.launch { container.preferences.setBookShelf(next) }
    }

    /** After an add or a delete elsewhere, the counts and the shelf move. */
    fun refresh() = reload()

    class Factory(private val container: AppContainer, private val baseUrl: String) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T = BookLibraryViewModel(container, baseUrl) as T
    }
}
