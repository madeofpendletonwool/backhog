package com.collinpendleton.backhog.ui.games

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.FacetsResponse
import com.collinpendleton.backhog.api.LibraryResponse
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.data.AppPreferences
import com.collinpendleton.backhog.data.LibraryFilters
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** The web's page size (`useLibrary.ts`). */
const val LIBRARY_PAGE_SIZE = 60

data class LibraryUiState(
    val filters: LibraryFilters = LibraryFilters(),
    /** The search box's live text; the query itself is debounced before it reloads. */
    val search: String = "",
    val facets: FacetsResponse = FacetsResponse(),
    val entries: List<Entry> = emptyList(),
    val total: Int = 0,
    val loading: Boolean = true,
    val loadingMore: Boolean = false,
    val error: String? = null,
) {
    val hasMore: Boolean get() = entries.size < total

    /** Drives the filtered-vs-unfiltered empty states, like the web. */
    val hasFilters: Boolean
        get() = filters.status != null || search.isNotBlank() ||
            filters.platformId != null || filters.genreId != null
}

/**
 * The games library: paged `GET /api/library?media=game` over the remembered
 * filter state, with facets for the filter panel. Filter changes persist
 * through [AppPreferences] and survive app restarts, like the web's
 * localStorage keys.
 */
class LibraryViewModel(
    private val session: SessionManager,
    private val baseUrl: String,
    private val prefs: AppPreferences,
) : ViewModel() {
    private val _state = MutableStateFlow(LibraryUiState())
    val state: StateFlow<LibraryUiState> = _state.asStateFlow()

    private val search = MutableStateFlow("")
    private var loadJob: Job? = null

    init {
        viewModelScope.launch { loadFacets() }
        viewModelScope.launch {
            // Filters persist immediately; the search box debounces 250 ms. One
            // combined stream means a filter change and a pause in typing that
            // land together only reload once.
            combine(prefs.libraryFilters, search.debounce(250)) { filters, query ->
                filters to query
            }.distinctUntilChanged().collect { reload() }
        }
    }

    fun setSearch(text: String) {
        _state.update { it.copy(search = text) }
        search.value = text
    }

    fun setStatus(status: EntryStatus?) = persist { it.copy(status = status) }

    fun setSort(sort: String) = persist { it.copy(sort = sort) }

    fun setPlatform(platformId: Long?) = persist { it.copy(platformId = platformId) }

    fun setGenre(genreId: Long?) = persist { it.copy(genreId = genreId) }

    fun setView(view: com.collinpendleton.backhog.data.LibraryLayout) = persist { it.copy(view = view) }

    fun clearFilters() {
        setSearch("")
        persist { it.copy(status = null, platformId = null, genreId = null) }
    }

    fun refresh() {
        viewModelScope.launch { loadFacets() }
        reload()
    }

    fun loadMore() {
        val current = _state.value
        if (current.loadingMore || !current.hasMore) return
        val filters = current.filters
        val query = search.value.takeIf { it.isNotBlank() }
        _state.update { it.copy(loadingMore = true) }
        viewModelScope.launch {
            apiCall {
                session.api(baseUrl).library(
                    status = filters.status?.key,
                    q = query,
                    sort = filters.sort,
                    platform = filters.platformId,
                    genre = filters.genreId,
                    limit = LIBRARY_PAGE_SIZE,
                    offset = current.entries.size,
                )
            }.onSuccess { page: LibraryResponse ->
                _state.update {
                    it.copy(
                        // Offset-paged: append, but never duplicate a key the
                        // list already holds (a delete elsewhere can shift page boundaries).
                        entries = (it.entries + page.entries).distinctBy { e -> e.id },
                        total = page.total,
                        loadingMore = false,
                    )
                }
            }.onFailure { e ->
                _state.update { it.copy(loadingMore = false, error = (e as ApiError).message) }
            }
        }
    }

    /** Called when returning from detail so status/rating edits show immediately. */
    fun onReturned() = reload()

    private fun persist(change: (LibraryFilters) -> LibraryFilters) {
        val next = change(_state.value.filters)
        _state.update { it.copy(filters = next) }
        viewModelScope.launch { prefs.setLibraryFilters(next) }
    }

    private fun reload() {
        loadJob?.cancel()
        loadJob = viewModelScope.launch {
            val filters = _state.value.filters
            val query = search.value.takeIf { it.isNotBlank() }
            _state.update { it.copy(loading = true, error = null) }
            apiCall {
                session.api(baseUrl).library(
                    status = filters.status?.key,
                    q = query,
                    sort = filters.sort,
                    platform = filters.platformId,
                    genre = filters.genreId,
                    limit = LIBRARY_PAGE_SIZE,
                    offset = 0,
                )
            }.onSuccess { page ->
                _state.update { it.copy(entries = page.entries, total = page.total, loading = false) }
            }.onFailure { e ->
                _state.update { it.copy(loading = false, error = (e as ApiError).message) }
            }
        }
    }

    private suspend fun loadFacets() {
        apiCall { session.api(baseUrl).facets() }
            .onSuccess { facets -> _state.update { it.copy(facets = facets) } }
    }
}
