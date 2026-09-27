package com.collinpendleton.backhog.ui.books.reader

import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.BookPosition
import com.collinpendleton.backhog.api.BookPagesResponse
import com.collinpendleton.backhog.api.BookTextChapters
import com.collinpendleton.backhog.api.BookTextDisplay
import com.collinpendleton.backhog.api.PositionWrite
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.books.ReaderBlock
import com.collinpendleton.backhog.books.chapterAt
import com.collinpendleton.backhog.books.percentAt
import com.collinpendleton.backhog.books.readerBlocks
import com.collinpendleton.backhog.books.readableChapters
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Which reader an entry opens into — a property of its classification, not its extension. */
enum class ReaderMode { Loading, Text, Paged, Failed }

data class ReaderState(
    val entryId: String = "",
    val mode: ReaderMode = ReaderMode.Loading,
    val position: BookPosition? = null,
    val chapters: BookTextChapters? = null,
    val spine: Int? = null,
    val display: BookTextDisplay? = null,
    val blocks: List<ReaderBlock> = emptyList(),
    val pages: BookPagesResponse? = null,
    val error: ApiError? = null,
    /** Where the UI should land once layout is ready; consumed by the restore scroll. */
    val pendingOffset: Long? = null,
    val pendingPage: Int? = null,
    /** A jump that never becomes "where you are": while it lasts, no position writes run. */
    val peek: Boolean = false,
    val liveOffset: Long = 0,
    /** The paged reader's live page. */
    val livePage: Int = 0,
    /** Writes stay shut until the restore has landed, so a page-one render cannot overwrite a real stored position. */
    val armed: Boolean = false,
) {
    val chapter get() = spine?.let { s -> chapters?.chapters?.firstOrNull { it.spineIndex == s } }
    val percent: Double get() = percentAt(chapters, liveOffset)
    val toc: List<com.collinpendleton.backhog.api.TextChapter>
        get() = readableChapters(chapters?.chapters ?: emptyList())
}

/**
 * The reading surface's data half: the position dispatch that picks the
 * reader, the per-spine text, and the position writes. The truth about where
 * you are is a canonical character offset; a page turn reports the offset of
 * the paragraph it lands on, and the writes ride the same checkpoint
 * discipline the web reader keeps — on the turn itself, so a client that
 * dies mid-swipe has already reported the page it left.
 */
class ReaderViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
    entryId: String,
    /** A deep link's landing offset, if the reader was opened from a jump. */
    private val jumpOffset: Long?,
    private val jumpPeek: Boolean,
) : ViewModel(), DefaultLifecycleObserver {

    private val _state = MutableStateFlow(ReaderState(entryId = entryId))
    val state: StateFlow<ReaderState> = _state.asStateFlow()

    /** The last offset the server has (a failed write clears, so the next checkpoint retries it). */
    private var lastWritten: Long? = null
    private var lastWrittenPage: Int? = null

    init {
        open()
    }

    private fun api() = container.session.api(baseUrl)

    /** The position query is the dispatch: page mode opens the paged reader, anything else the text one. */
    private fun open() {
        val id = _state.value.entryId
        viewModelScope.launch {
            val position = apiCall { api().bookPosition(id) }
            position.onSuccess { pos ->
                _state.update { it.copy(position = pos) }
                if (pos.positionMode == "page") openPaged(id, pos) else openText(id, pos)
            }
            position.onFailure { e ->
                // A position that will not load must not hold the book shut:
                // the start of the book is where a book with no stored
                // position starts anyway.
                if ((e as? ApiError)?.status == 404) {
                    _state.update { it.copy(position = null) }
                    openText(id, null)
                } else {
                    _state.update { it.copy(mode = ReaderMode.Failed, error = e as? ApiError ?: ApiError(-1, e.message ?: "")) }
                }
            }
        }
    }

    private fun openPaged(id: String, pos: BookPosition?) {
        viewModelScope.launch {
            apiCall { api().bookPages(id) }
                .onSuccess { pages ->
                    // A jump into a paged book carries the page axis (its only axis).
                    val jumpPage = jumpOffset?.takeIf { it >= 0 }?.toInt()
                    val at = jumpPage ?: pos?.pageIndex ?: 0
                    _state.update {
                        it.copy(
                            mode = ReaderMode.Paged,
                            pages = pages,
                            pendingPage = at,
                            livePage = at,
                            peek = jumpPeek && jumpPage != null,
                        )
                    }
                }
                .onFailure { e ->
                    _state.update { it.copy(mode = ReaderMode.Failed, error = e as? ApiError ?: ApiError(-1, e.message ?: "")) }
                }
        }
    }

    private fun jumpOffsetSafe(pos: BookPosition?): Long? = jumpOffset?.takeIf { it >= 0 }

    private fun openText(id: String, pos: BookPosition?) {
        viewModelScope.launch {
            apiCall { api().bookTextChapters(id) }
                .onSuccess { text ->
                    val at = jumpOffsetSafe(pos)
                        ?: pos?.charOffset ?: 0L
                    val chapters = text.chapters
                    val target = chapterAt(chapters, at)
                        ?: readableChapters(chapters).firstOrNull()
                        ?: chapters.firstOrNull()
                    _state.update {
                        it.copy(
                            mode = ReaderMode.Text,
                            chapters = text,
                            liveOffset = at,
                            pendingOffset = at,
                            peek = jumpPeek && jumpOffsetSafe(pos) != null,
                        )
                    }
                    if (target != null) loadSpine(target.spineIndex)
                }
                .onFailure { e ->
                    _state.update { it.copy(mode = ReaderMode.Failed, error = e as? ApiError ?: ApiError(-1, e.message ?: "")) }
                }
        }
    }

    private suspend fun loadSpine(spine: Int) {
        val id = _state.value.entryId
        apiCall { api().bookTextDisplay(id, spine) }
            .onSuccess { display ->
                val chapter = _state.value.chapters?.chapters?.firstOrNull { it.spineIndex == spine }
                val blocks = if (chapter != null) readerBlocks(chapter, display.blocks) else emptyList()
                _state.update { it.copy(spine = spine, display = display, blocks = blocks) }
            }
            .onFailure { e ->
                _state.update { it.copy(mode = ReaderMode.Failed, error = e as? ApiError ?: ApiError(-1, e.message ?: "")) }
            }
    }

    /** A deliberate navigation: TOC pick or chapter step. Ends any peek it interrupts. */
    fun goToChapter(target: com.collinpendleton.backhog.api.TextChapter) {
        val s = _state.value
        if (s.spine == target.spineIndex) {
            _state.update {
                it.copy(
                    peek = false,
                    liveOffset = target.charStart,
                    pendingOffset = target.charStart,
                    armed = false,
                )
            }
        } else {
            _state.update {
                it.copy(
                    peek = false,
                    spine = target.spineIndex,
                    display = null,
                    blocks = emptyList(),
                    liveOffset = target.charStart,
                    pendingOffset = target.charStart,
                    armed = false,
                )
            }
            viewModelScope.launch { loadSpine(target.spineIndex) }
        }
    }

    /** A search hit or "back to my place": land on a paragraph, optionally as a look that never writes. */
    fun jumpTo(offset: Long, peek: Boolean) {
        val s = _state.value
        // A paged book's only axis is the page; a jump into it carries one.
        if (s.mode == ReaderMode.Paged) {
            val pageCount = s.pages?.pageCount ?: 0
            val page = offset.toInt().coerceIn(0, (pageCount - 1).coerceAtLeast(0))
            _state.update { it.copy(peek = peek, pendingPage = page, livePage = page, armed = false) }
            return
        }
        val clamped = offset.coerceIn(0, s.chapters?.charCount ?: offset)
        val target = s.chapters?.chapters?.let { chapterAt(it, clamped) }
        _state.update {
            it.copy(peek = peek, liveOffset = clamped, pendingOffset = clamped, armed = false)
        }
        if (target != null && target.spineIndex != s.spine) {
            _state.update { it.copy(spine = target.spineIndex, display = null, blocks = emptyList()) }
            viewModelScope.launch { loadSpine(target.spineIndex) }
        }
    }

    fun endPeek() = _state.update { it.copy(peek = false) }

    fun backToMyPlace() {
        val home = _state.value.position?.charOffset ?: 0L
        jumpTo(home, peek = false)
    }

    /**
     * The UI has landed on the restore target; from here, page reports are
     * the honest progress signal. The pending offset is consumed here so a
     * later re-pagination (a type change, a rotation) re-anchors on the
     * paragraph the reader has reached, not the one the book opened at.
     */
    fun landed() = _state.update { it.copy(armed = true, pendingOffset = null) }

    /** A page turn in the text reader: deliberate by construction, so it ends a peek and writes. */
    fun reportTextPage(offset: Long) {
        _state.update { it.copy(peek = false, liveOffset = offset) }
        if (!_state.value.armed) return
        if (lastWritten == offset) return
        lastWritten = offset
        val id = _state.value.entryId
        viewModelScope.launch {
            apiCall { api().putBookPosition(id, PositionWrite(charOffset = offset, source = "read")) }
                .onFailure { lastWritten = null }
        }
    }

    /** A page turn in the paged reader — the page index is that model's whole truth. */
    fun reportPageTurn(page: Int) {
        _state.update { it.copy(peek = false, livePage = page) }
        if (!_state.value.armed) return
        if (lastWrittenPage == page) return
        lastWrittenPage = page
        val id = _state.value.entryId
        viewModelScope.launch {
            apiCall { api().putBookPosition(id, PositionWrite(pageIndex = page, source = "read")) }
                .onFailure { lastWrittenPage = null }
        }
    }

    /**
     * The app left the foreground. The POST form exists exactly for "the
     * client may die before its next request": fire the outstanding position
     * again without waiting for anything. A turn already wrote, so this
     * usually has nothing to do — it covers the race where the write is
     * still in flight when the process goes.
     */
    override fun onStop(owner: LifecycleOwner) {
        val s = _state.value
        if (!s.armed || s.peek) return
        val id = s.entryId
        when (s.mode) {
            ReaderMode.Text -> {
                val offset = s.liveOffset
                if (lastWritten != offset) {
                    lastWritten = offset
                    viewModelScope.launch {
                        apiCall { api().postBookPosition(id, PositionWrite(charOffset = offset, source = "read")) }
                            .onFailure { lastWritten = null }
                    }
                }
            }
            ReaderMode.Paged -> {
                val page = s.livePage
                if (lastWrittenPage != page) {
                    lastWrittenPage = page
                    viewModelScope.launch {
                        apiCall { api().postBookPosition(id, PositionWrite(pageIndex = page, source = "read")) }
                            .onFailure { lastWrittenPage = null }
                    }
                }
            }
            else -> {}
        }
    }

    class Factory(
        private val container: AppContainer,
        private val baseUrl: String,
        private val entryId: String,
        private val jumpOffset: Long?,
        private val jumpPeek: Boolean,
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            ReaderViewModel(container, baseUrl, entryId, jumpOffset, jumpPeek) as T
    }
}
