package com.collinpendleton.backhog.ui.books

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.AddBookRequest
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.Book
import com.collinpendleton.backhog.api.BookSearchResponse
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.books.sortEditions
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Three ways onto the shelf, in the order they are fastest. */
enum class AddMode { Scan, Isbn, Search }

data class AddBookState(
    val mode: AddMode = AddMode.Scan,
    /** The work once found, whichever route found it. */
    val picked: Book? = null,
    /** The ISBN that found it, so its printing can be preselected. */
    val pickedIsbn: String = "",
    val search: String = "",
    val results: List<Book> = emptyList(),
    val searching: Boolean = false,
    val searchError: String? = null,
    val isbn: String = "",
    val resolving: Boolean = false,
    val isbnError: String? = null,
    /** The printings of the picked work, newest-and-counted first. */
    val editions: List<com.collinpendleton.backhog.api.BookEdition> = emptyList(),
    val editionId: String = "",
    val status: EntryStatus = EntryStatus.Backlog,
    val adding: Boolean = false,
    val addError: String? = null,
    /** A code the scanner read that is not a book ISBN. */
    val notABook: String = "",
) {
    val isbnValid: Boolean get() = com.collinpendleton.backhog.books.looksLikeIsbn(isbn)
}

/**
 * Add a book: scan the barcode, type the ISBN, or search by title — then the
 * same last step for all three, saying which printing you own, because page
 * numbers belong to a printing and not to the work.
 */
class AddBookViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
) : ViewModel() {

    private val _state = MutableStateFlow(AddBookState())
    val state: StateFlow<AddBookState> = _state.asStateFlow()

    init {
        @OptIn(FlowPreview::class)
        viewModelScope.launch {
            _state
                .debounce { if (it.mode == AddMode.Search) 300L else Long.MAX_VALUE }
                .collect { runSearch(it.search) }
        }
    }

    private fun api() = container.session.api(baseUrl)

    fun setMode(mode: AddMode) = _state.update {
        it.copy(mode = mode, notABook = "", isbnError = null, searchError = null)
    }

    fun setSearch(term: String) = _state.update { it.copy(search = term, searchError = null) }

    fun setIsbn(raw: String) =
        _state.update { it.copy(isbn = com.collinpendleton.backhog.books.normalizeIsbn(raw), isbnError = null) }

    fun scannedCode(raw: String) {
        val isbn = com.collinpendleton.backhog.books.normalizeIsbn(raw)
        if (com.collinpendleton.backhog.books.looksLikeIsbn(isbn)) {
            _state.update { it.copy(isbn = isbn, notABook = "") }
            resolveIsbn(isbn)
        } else {
            _state.update { it.copy(notABook = raw) }
        }
    }

    fun resolveIsbn(isbn: String = _state.value.isbn) {
        if (!com.collinpendleton.backhog.books.looksLikeIsbn(isbn)) return
        _state.update { it.copy(resolving = true, isbnError = null, pickedIsbn = isbn) }
        viewModelScope.launch {
            apiCall { api().bookByIsbn(isbn) }
                .onSuccess { book -> pick(book, isbn) }
                .onFailure { e ->
                    _state.update {
                        it.copy(
                            resolving = false,
                            isbnError = if ((e as? ApiError)?.status == 404) {
                                "Open Library doesn't know $isbn. Search by title and author instead — the work is probably there under a different printing."
                            } else {
                                e.message
                            },
                        )
                    }
                }
        }
    }

    fun pick(book: Book, isbn: String = "") {
        _state.update {
            it.copy(
                picked = book,
                pickedIsbn = isbn,
                resolving = false,
                searching = false,
                addError = null,
                editionId = "",
            )
        }
        // Search hits are cached lean; the work is re-read for its printings.
        viewModelScope.launch {
            apiCall { api().getBook(book.id) }
                .onSuccess { full ->
                    val editions = sortEditions(full.editions ?: emptyList())
                    _state.update { s ->
                        val preselected = if (s.pickedIsbn.isNotEmpty()) {
                            editions.firstOrNull { edition ->
                                com.collinpendleton.backhog.books.editionIsbn(edition) == s.pickedIsbn
                            }?.id.orEmpty()
                        } else ""
                        s.copy(editions = editions, editionId = preselected)
                    }
                }
        }
    }

    fun setEdition(id: String) = _state.update { it.copy(editionId = id) }
    fun setStatus(status: EntryStatus) = _state.update { it.copy(status = status) }

    fun add(onAdded: () -> Unit) {
        val s = _state.value
        val book = s.picked ?: return
        if (s.adding) return
        _state.update { it.copy(adding = true, addError = null) }
        viewModelScope.launch {
            apiCall {
                api().addBook(
                    AddBookRequest(
                        bookId = book.id,
                        editionId = s.editionId.ifEmpty { null },
                        status = s.status,
                    ),
                )
            }.onSuccess {
                _state.update { AddBookState() }
                onAdded()
            }.onFailure { e ->
                _state.update {
                    it.copy(
                        adding = false,
                        addError = if ((e as? ApiError)?.status == 409) {
                            "That book is already on your shelf."
                        } else {
                            e.message
                        },
                    )
                }
            }
        }
    }

    /** Back out of the confirm step to the mode tabs, empty-handed. */
    fun back() = _state.update {
        AddBookState(mode = it.mode, search = it.search, isbn = it.isbn, results = it.results)
    }

    private fun runSearch(term: String) {
        if (_state.value.mode != AddMode.Search) return
        if (term.trim().length < 2) {
            _state.update { it.copy(results = emptyList(), searchError = null, searching = false) }
            return
        }
        _state.update { it.copy(searching = true, searchError = null) }
        viewModelScope.launch {
            apiCall { api().searchBooks(term.trim()) }
                .onSuccess { response: BookSearchResponse ->
                    // Only the latest keystroke's answer is shown.
                    if (_state.value.search.trim() == term.trim()) {
                        _state.update { it.copy(results = response.results.map { r -> r.book }, searching = false) }
                    }
                }
                .onFailure { e ->
                    if (_state.value.search.trim() == term.trim()) {
                        _state.update {
                            it.copy(
                                searching = false,
                                searchError = if ((e as? ApiError)?.status == 503) {
                                    "The book metadata provider isn't reachable right now."
                                } else {
                                    e.message
                                },
                            )
                        }
                    }
                }
        }
    }

    class Factory(private val container: AppContainer, private val baseUrl: String) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T = AddBookViewModel(container, baseUrl) as T
    }
}
