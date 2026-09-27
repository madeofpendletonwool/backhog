package com.collinpendleton.backhog.ui.media

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.AddBookRequest
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.AttachFilesRequest
import com.collinpendleton.backhog.api.BookSearchResult
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.IgnoreFilesRequest
import com.collinpendleton.backhog.api.MediaCandidate
import com.collinpendleton.backhog.api.MediaFilesResponse
import com.collinpendleton.backhog.api.MediaScanStatus
import com.collinpendleton.backhog.api.MediaSuggestion
import com.collinpendleton.backhog.api.MediaCandidatesResponse
import com.collinpendleton.backhog.api.apiCall
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class MediaFilesState(
    val loading: Boolean = true,
    val scan: MediaScanStatus? = null,
    val queue: MediaCandidatesResponse? = null,
    /** The raw path browser's slice of the inventory. */
    val inventory: MediaFilesResponse? = null,
    val inventoryKind: String = "",
    val inventoryUnattached: Boolean = false,
    val error: String? = null,
    val status: String? = null,
    /** Candidate keys with a mutation in flight. */
    val busy: Set<String> = emptySet(),
    val bulkRunning: Boolean = false,
)

/**
 * The attach review queue's data half: the scan, the candidates, the raw
 * inventory, and the confirm / ignore / bulk-confirm mutations. The matcher
 * fills suggestions in at its own pace, so the queue is re-peeked while any
 * candidate sits unmatched — matches surface without a manual reload.
 */
class MediaFilesViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
) : ViewModel() {

    private val _state = MutableStateFlow(MediaFilesState())
    val state: StateFlow<MediaFilesState> = _state.asStateFlow()

    private var polling = false

    init {
        reload()
    }

    private fun api() = container.session.api(baseUrl)

    fun reload() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { api().mediaScanStatus() }.onSuccess { scan ->
                _state.update { it.copy(scan = scan) }
            }
            apiCall { api().mediaCandidates() }.onSuccess { queue ->
                _state.update { it.copy(queue = queue, loading = false) }
            }.onFailure { e ->
                _state.update { it.copy(loading = false, error = e.message) }
            }
            loadInventory()
            watch()
        }
    }

    /** While a scan runs, poll progress and the queue it is filling. */
    private fun watch() {
        if (polling) return
        polling = true
        viewModelScope.launch {
            while (true) {
                val s = _state.value
                val scanRunning = s.scan?.running == true
                val unmatched = s.queue?.candidates?.any { (it.suggestions ?: emptyList()).isEmpty() } == true
                if (!scanRunning && !unmatched) {
                    polling = false
                    return@launch
                }
                delay(if (scanRunning) 1500L else 10_000L)
                if (scanRunning) {
                    apiCall { api().mediaScanStatus() }.onSuccess { scan ->
                        _state.update { it.copy(scan = scan) }
                    }
                }
                apiCall { api().mediaCandidates() }.onSuccess { queue ->
                    _state.update { it.copy(queue = queue) }
                }
            }
        }
    }

    fun kickScan() {
        viewModelScope.launch {
            apiCall { api().kickMediaScan() }.onSuccess { started ->
                if (started.started) {
                    _state.update { it.copy(status = null) }
                    apiCall { api().mediaScanStatus() }.onSuccess { scan ->
                        _state.update { it.copy(scan = scan) }
                    }
                    watch()
                }
            }
        }
    }

    fun setInventoryFilters(kind: String, unattached: Boolean) {
        _state.update { it.copy(inventoryKind = kind, inventoryUnattached = unattached) }
        viewModelScope.launch { loadInventory() }
    }

    private suspend fun loadInventory() {
        val s = _state.value
        apiCall {
            api().mediaFiles(
                kind = s.inventoryKind.ifEmpty { null },
                unattached = if (s.inventoryUnattached) true else null,
                includeMissing = true,
            )
        }.onSuccess { inventory ->
            _state.update { it.copy(inventory = inventory) }
        }
    }

    /**
     * Resolves the library entry to attach a suggestion's files to, adding
     * the book first when the user does not own it yet. A 409 on the add
     * means the book is already owned — find the entry instead of surfacing
     * a conflict the user has no way to act on.
     */
    private suspend fun entryForSuggestion(suggestion: MediaSuggestion): Result<String> {
        suggestion.entryId?.let { return Result.success(it) }
        val added = apiCall { api().addBook(AddBookRequest(bookId = suggestion.book.id)) }
        return added.map { it.id }.recoverCatching { e ->
            if ((e as? ApiError)?.status != 409) throw e
            findBookEntry(suggestion.book.id) ?: throw e
        }
    }

    /** Finds the user's existing entry for a book they already own. */
    private suspend fun findBookEntry(bookId: String): String? {
        val pageSize = 200
        var offset = 0
        while (true) {
            val page = apiCall {
                api().library(media = "book", limit = pageSize, offset = offset)
            }.getOrElse { return null }
            page.entries.firstOrNull { it.book?.id == bookId }?.let { return it.id }
            if (page.entries.size < pageSize) return null
            offset += pageSize
        }
    }

    private fun invalidate() {
        viewModelScope.launch {
            apiCall { api().mediaCandidates() }.onSuccess { queue ->
                _state.update { it.copy(queue = queue) }
            }
            loadInventory()
            watch()
        }
    }

    private fun markBusy(key: String, on: Boolean) {
        _state.update { s ->
            val next = s.busy.toMutableSet()
            if (on) next.add(key) else next.remove(key)
            s.copy(busy = next)
        }
    }

    fun confirm(candidate: MediaCandidate, suggestion: MediaSuggestion) {
        markBusy(candidate.key, true)
        viewModelScope.launch {
            val entry = entryForSuggestion(suggestion)
            entry.fold(
                onSuccess = { entryId ->
                    apiCall {
                        api().attachFiles(entryId, AttachFilesRequest(candidate.files.map { it.id }, candidate.kind))
                    }.fold(
                        onSuccess = {
                            _state.update { s ->
                                s.copy(status = "Attached to ${suggestion.book.title}.")
                            }
                        },
                        onFailure = { e -> attachError(e) },
                    )
                },
                onFailure = { e -> attachError(e) },
            )
            markBusy(candidate.key, false)
            invalidate()
        }
    }

    private fun attachError(e: Throwable) {
        _state.update { s ->
            s.copy(
                status = if ((e as? ApiError)?.status == 409) {
                    "One of these files is already attached to another book — detach it there first."
                } else {
                    e.message
                },
            )
        }
    }

    /** Attach to a book chosen by hand from Open Library search. */
    fun attachToBook(candidate: MediaCandidate, result: BookSearchResult) {
        markBusy(candidate.key, true)
        viewModelScope.launch {
            val entryId = result.entryId ?: run {
                val added = apiCall {
                    api().addBook(AddBookRequest(bookId = result.book.id, status = EntryStatus.Backlog))
                }
                added.map { it.id }.getOrNull()
            }
            if (entryId == null) {
                _state.update { it.copy(status = "Could not add that book to your shelf.") }
            } else {
                apiCall {
                    api().attachFiles(entryId, AttachFilesRequest(candidate.files.map { it.id }, candidate.kind))
                }.fold(
                    onSuccess = { _state.update { s -> s.copy(status = "Attached to ${result.book.title}.") } },
                    onFailure = { e -> attachError(e) },
                )
            }
            markBusy(candidate.key, false)
            invalidate()
        }
    }

    fun ignore(candidate: MediaCandidate) {
        viewModelScope.launch {
            apiCall { api().ignoreMediaFiles(IgnoreFilesRequest(candidate.files.map { it.id })) }
                .onSuccess { invalidate() }
        }
    }

    /** Confirm every high-confidence candidate with a suggestion, in one go. */
    fun bulkConfirm() {
        val bulkable = _state.value.queue?.candidates
            ?.filter { it.highConfidence && !(it.suggestions ?: emptyList()).isEmpty() }
            ?: return
        if (bulkable.isEmpty()) return
        _state.update { it.copy(bulkRunning = true, status = null) }
        viewModelScope.launch {
            for (candidate in bulkable) {
                val suggestion = candidate.suggestions?.first() ?: continue
                markBusy(candidate.key, true)
                val outcome = try {
                    val entryId = entryForSuggestion(suggestion).getOrThrow()
                    apiCall {
                        api().attachFiles(entryId, AttachFilesRequest(candidate.files.map { it.id }, candidate.kind))
                    }.getOrThrow()
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Throwable) {
                    e
                }
                if (outcome is Throwable) {
                    if ((outcome as? ApiError)?.status == 409) {
                        markBusy(candidate.key, false)
                        continue
                    }
                    _state.update { s ->
                        s.copy(
                            status = "Bulk confirm stopped at \"${candidate.titleGuess.ifEmpty { candidate.dirPath }}\": ${outcome.message}",
                        )
                    }
                    markBusy(candidate.key, false)
                    break
                }
                markBusy(candidate.key, false)
            }
            _state.update { it.copy(bulkRunning = false) }
            invalidate()
        }
    }

    fun clearStatus() = _state.update { it.copy(status = null) }

    class Factory(private val container: AppContainer, private val baseUrl: String) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            MediaFilesViewModel(container, baseUrl) as T
    }
}
