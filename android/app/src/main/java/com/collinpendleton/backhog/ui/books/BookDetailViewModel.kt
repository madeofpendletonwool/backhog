package com.collinpendleton.backhog.ui.books

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.AchievementStatus
import com.collinpendleton.backhog.api.AddSessionRequest
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.Book
import com.collinpendleton.backhog.api.BookFilesResponse
import com.collinpendleton.backhog.api.BookPosition
import com.collinpendleton.backhog.api.CopiesResponse
import com.collinpendleton.backhog.api.CopyResponse
import com.collinpendleton.backhog.api.CreateCopyRequest
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.api.PageAnchor
import com.collinpendleton.backhog.api.PassageRequest
import com.collinpendleton.backhog.api.PassageResult
import com.collinpendleton.backhog.api.PatchEntryResponse
import com.collinpendleton.backhog.api.PhysicalCopy
import com.collinpendleton.backhog.api.PlaySession
import com.collinpendleton.backhog.api.PositionWrite
import com.collinpendleton.backhog.api.SaveAnchorRequest
import com.collinpendleton.backhog.api.ShareCandidate
import com.collinpendleton.backhog.api.ShareRequest
import com.collinpendleton.backhog.api.AlignmentStatusView
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.api.entryPatch
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class BookDetailState(
    val entryId: String = "",
    val entry: Entry? = null,
    /** The work re-read for its printings and description. */
    val book: Book? = null,
    val loading: Boolean = true,
    val error: String? = null,
    val position: BookPosition? = null,
    val copies: CopiesResponse? = null,
    val files: BookFilesResponse? = null,
    val sessions: List<PlaySession> = emptyList(),
    val shareCandidates: List<ShareCandidate> = emptyList(),
    /** Where the entry's text↔audio alignment stands; null until asked for. */
    val align: AlignmentStatusView? = null,
    /** Achievements a mutation unlocked, queued for a toast. */
    val unlocks: List<AchievementStatus> = emptyList(),
    val busy: Boolean = false,
    val actionError: String? = null,
    val deleted: Boolean = false,
) {
    /** The copy whose page map the position reads; scans feed this one. */
    val drivingCopy: PhysicalCopy?
        get() = copies?.copies?.firstOrNull { it.drivesPages } ?: copies?.copies?.firstOrNull()
}

/**
 * One book, whole: the Open Library dossier, the entry's status/rating/notes,
 * the three-coordinate position, the printings in hand, the attached files,
 * and who the book is shared with. Mutations land and the affected reads are
 * re-fetched — the server owns all data, so a write is not believed until it
 * is read back.
 */
class BookDetailViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
    entryId: String,
) : ViewModel() {

    private val _state = MutableStateFlow(BookDetailState(entryId = entryId))
    val state: StateFlow<BookDetailState> = _state.asStateFlow()

    init {
        reload()
    }

    private fun api() = container.session.api(baseUrl)

    fun reload() {
        val id = _state.value.entryId
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { api().entry(id) }
                .onSuccess { entry ->
                    _state.update { it.copy(entry = entry) }
                    entry.book?.let { book ->
                        apiCall { api().getBook(book.id) }.onSuccess { full ->
                            _state.update { it.copy(book = full) }
                        }
                    }
                }
                .onFailure { e ->
                    _state.update { it.copy(loading = false, error = e.message) }
                    return@launch
                }
            // Position, copies, files, sessions and shares all key off the
            // entry; each may be absent honestly (nothing scanned, no files…)
            // and absence is a state, not a failure.
            apiCall { api().bookPosition(id) }.onSuccess { position ->
                _state.update { it.copy(position = position) }
            }
            apiCall { api().copies(id) }.onSuccess { copies ->
                _state.update { it.copy(copies = copies) }
            }
            apiCall { api().bookFiles(id) }.onSuccess { files ->
                _state.update { it.copy(files = files) }
            }
            apiCall { api().sessions(id) }.onSuccess { sessions ->
                _state.update { it.copy(sessions = sessions.sessions) }
            }
            apiCall { api().shareCandidates(id) }.onSuccess { shares ->
                _state.update { it.copy(shareCandidates = shares.candidates) }
            }
            _state.update { it.copy(loading = false) }
        }
    }

    /** Queued unlocks clear once shown. */
    fun consumeUnlocks() = _state.update { it.copy(unlocks = emptyList()) }
    fun clearActionError() = _state.update { it.copy(actionError = null) }

    /** The entry PATCH path: writes land, the entry is read back, unlocks queue. */
    private fun patch(body: kotlinx.serialization.json.JsonObject, after: (suspend () -> Unit)? = null) {
        runMutation(
            call = { api().patchEntry(_state.value.entryId, body) },
            onDone = { result: PatchEntryResponse ->
                _state.update { it.copy(entry = result.entry, unlocks = result.unlocks) }
                after?.invoke()
            },
        )
    }

    private fun <T> runMutation(call: suspend () -> T, onDone: (suspend (T) -> Unit)? = null) {
        if (_state.value.busy) return
        _state.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            apiCall(call)
                .onSuccess { value -> onDone?.invoke(value) }
                .onFailure { e ->
                    val conflict = (e as? ApiError)?.status == 409
                    _state.update {
                        it.copy(
                            actionError = if (conflict) {
                                "That copy is already registered — reopen it instead."
                            } else {
                                e.message
                            },
                        )
                    }
                }
            _state.update { it.copy(busy = false) }
        }
    }

    fun setStatus(status: EntryStatus) = patch(entryPatch { status(status) })

    fun setRating(score: Int?) = patch(entryPatch { rating(score) })

    fun setNotes(notes: String) = patch(entryPatch { notes(notes) })

    fun addSession(minutes: Int, note: String) {
        if (minutes <= 0) return
        runMutation(
            call = { api().addSession(_state.value.entryId, AddSessionRequest(minutes, note = note.takeIf { it.isNotBlank() })) },
            onDone = { result ->
                _state.update { it.copy(unlocks = result.unlocks) }
                refreshSessions()
            },
        )
    }

    fun deleteEntry() {
        viewModelScope.launch {
            apiCall { api().deleteEntry(_state.value.entryId) }
                .onSuccess { _state.update { it.copy(deleted = true) } }
                .onFailure { e -> _state.update { it.copy(actionError = e.message) } }
        }
    }

    // --- copies -----------------------------------------------------------

    fun registerCopy(editionId: String, borrowed: Boolean, dueAt: String?, notes: String) {
        val request = CreateCopyRequest(
            editionId = editionId,
            notes = notes,
            acquisition = if (borrowed) "borrowed" else "owned",
            dueAt = dueAt?.takeIf { it.isNotBlank() },
        )
        runMutation(
            call = { api().createCopy(_state.value.entryId, request) },
            onDone = { refreshCopiesAndPosition() },
        )
    }

    fun returnCopy(copyId: String) = copyTransition { api().returnCopy(_state.value.entryId, copyId) }
    fun reopenCopy(copyId: String) = copyTransition { api().reopenCopy(_state.value.entryId, copyId) }
    fun ownCopy(copyId: String) = copyTransition { api().ownCopy(_state.value.entryId, copyId) }

    fun deleteCopy(copyId: String) {
        runMutation(
            call = { api().deleteCopy(_state.value.entryId, copyId) },
            onDone = { refreshCopiesAndPosition() },
        )
    }

    private fun copyTransition(call: suspend () -> CopyResponse) {
        runMutation(call = call, onDone = { refreshCopiesAndPosition() })
    }

    private suspend fun refreshCopiesAndPosition() {
        apiCall { api().copies(_state.value.entryId) }.onSuccess { copies ->
            _state.update { it.copy(copies = copies) }
        }
        apiCall { api().bookPosition(_state.value.entryId) }.onSuccess { position ->
            _state.update { it.copy(position = position) }
        }
    }

    private suspend fun refreshSessions() {
        apiCall { api().sessions(_state.value.entryId) }.onSuccess { sessions ->
            _state.update { it.copy(sessions = sessions.sessions) }
        }
    }

    /** One printing's page map, for the anchors list. */
    suspend fun anchorsFor(copyId: String): Result<List<PageAnchor>> =
        apiCall { api().copyPages(_state.value.entryId, copyId) }.map { it.anchors }

    // --- page scanning ------------------------------------------------------

    /** Where a stretch of text read off paper sits in the ebook. */
    suspend fun matchPassage(text: String): Result<PassageResult> =
        apiCall { api().matchPassage(_state.value.entryId, PassageRequest(text)) }

    /**
     * Pin the page and (unless the reader declined) move progress to the
     * match: one scan answers "what page am I on" and "where am I" both.
     */
    fun saveScan(
        copyId: String,
        printedPage: Int,
        charOffset: Long,
        source: String,
        confidence: Double,
        alsoSetProgress: Boolean,
    ) {
        viewModelScope.launch {
            val anchor = apiCall {
                api().savePageAnchor(
                    _state.value.entryId,
                    copyId,
                    SaveAnchorRequest(
                        printedPage = printedPage,
                        charOffset = charOffset,
                        source = source,
                        confidence = confidence,
                    ),
                )
            }
            anchor.onFailure { e ->
                _state.update { it.copy(actionError = e.message) }
                return@launch
            }
            if (alsoSetProgress) {
                // 'scan' exists for exactly this: a position that came off
                // paper, distinguishable from one the reader wrote.
                val write = apiCall {
                    api().putBookPosition(
                        _state.value.entryId,
                        PositionWrite(charOffset = charOffset, source = if (source == "ocr") "scan" else "manual"),
                    )
                }
                write.onFailure { e -> _state.update { it.copy(actionError = e.message) } }
            }
            refreshCopiesAndPosition()
            apiCall { api().entry(_state.value.entryId) }.onSuccess { entry ->
                _state.update { it.copy(entry = entry) }
            }
        }
    }

    // --- sharing --------------------------------------------------------------

    fun share(userId: String) = runMutation(
        call = { api().shareBook(_state.value.entryId, ShareRequest(userId)) },
        onDone = { refreshShares() },
    )

    fun unshare(userId: String) = runMutation(
        call = { api().unshareBook(_state.value.entryId, userId) },
        onDone = { refreshShares() },
    )

    private suspend fun refreshShares() {
        apiCall { api().shareCandidates(_state.value.entryId) }.onSuccess { shares ->
            _state.update { it.copy(shareCandidates = shares.candidates) }
        }
    }

    // --- the file layer (member/admin) -----------------------------------

    private suspend fun refreshFiles() {
        apiCall { api().bookFiles(_state.value.entryId) }.onSuccess { files ->
            _state.update { it.copy(files = files) }
        }
        // The position's derived views move with the files that back them.
        apiCall { api().bookPosition(_state.value.entryId) }.onSuccess { position ->
            _state.update { it.copy(position = position) }
        }
    }

    /**
     * Makes one of the book's text files its canonical text. The server
     * migrates the stored position (exactly when the containers
     * canonicalize alike, by percentage when not) and drops any alignment,
     * because it was built against the text being left behind.
     */
    fun promoteTextFile(fileId: Long) {
        runMutation(
            call = { api().setPrimaryTextFile(_state.value.entryId, fileId) },
            onDone = { refreshFiles() },
        )
    }

    /** Makes one of the book's recordings the one that plays. */
    fun promoteAudioEdition(editionId: Long) {
        runMutation(
            call = { api().setPrimaryAudioEdition(_state.value.entryId, editionId) },
            onDone = { refreshFiles() },
        )
    }

    /**
     * Detach a file. A file flagged missing can still be detached — the
     * association is the thing being removed, not the bytes — but the UI
     * explains what missing means before it lets anyone touch anything.
     */
    fun detachFile(fileId: Long) {
        runMutation(
            call = { api().detachFile(_state.value.entryId, fileId) },
            onDone = { refreshFiles() },
        )
    }

    /** Where the alignment stands; polled while a job is running. */
    fun loadAlign() {
        viewModelScope.launch {
            apiCall { api().alignStatus(_state.value.entryId) }.onSuccess { status ->
                _state.update { it.copy(align = status) }
            }
            val active = _state.value.align?.job?.state in setOf("queued", "claimed", "transcribing", "aligning")
            if (active) {
                kotlinx.coroutines.delay(4000)
                loadAlign()
            }
        }
    }

    fun enqueueAlignment() {
        runMutation(
            call = { api().enqueueAlignment(_state.value.entryId) },
            onDone = { loadAlign() },
        )
    }

    fun clearAlignment() {
        viewModelScope.launch {
            apiCall { api().clearAlignment(_state.value.entryId) }.onSuccess { loadAlign() }
        }
    }

    class Factory(private val container: AppContainer, private val baseUrl: String, private val entryId: String) :
        ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            BookDetailViewModel(container, baseUrl, entryId) as T
    }
}
