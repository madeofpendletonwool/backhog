package com.collinpendleton.backhog.ui.home

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.BookStats
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.ReadingNowBook
import com.collinpendleton.backhog.api.Stats
import com.collinpendleton.backhog.api.apiCall
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class HomeState(
    val loading: Boolean = true,
    val reading: List<ReadingNowBook> = emptyList(),
    val playing: List<Entry> = emptyList(),
    val upNext: List<Entry> = emptyList(),
    val gameStats: Stats? = null,
    val bookStats: BookStats? = null,
    /** True when every read failed — the server is unreachable, not merely empty. */
    val offline: Boolean = false,
)

/**
 * Home: what you're in the middle of, across both arenas. Each read stands
 * alone — a failed queue shouldn't blank the books you're reading.
 */
class HomeViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
) : ViewModel() {

    private val _state = MutableStateFlow(HomeState())
    val state: StateFlow<HomeState> = _state.asStateFlow()

    init {
        reload()
    }

    fun reload() {
        viewModelScope.launch {
            val reading = async { apiCall { api().readingNow() } }
            val playing = async { apiCall { api().library(media = "game", status = "playing", limit = 12) } }
            val queue = async { apiCall { api().queue() } }
            val games = async { apiCall { api().stats("game") } }
            val books = async { apiCall { api().bookStats() } }

            val results = listOf(reading.await(), playing.await(), queue.await(), games.await(), books.await())
            _state.update { current ->
                current.copy(
                    loading = false,
                    reading = reading.await().getOrNull()?.books
                        ?.sortedByDescending { it.lastReadAt ?: "" }
                        ?: current.reading,
                    playing = playing.await().getOrNull()?.entries ?: current.playing,
                    // The queue is the backlog in play order; what's already on the go
                    // is shown above it, so it isn't "next".
                    upNext = queue.await().getOrNull()?.entries
                        ?.filter { it.status != com.collinpendleton.backhog.api.EntryStatus.Playing }
                        ?.take(5)
                        ?: current.upNext,
                    gameStats = games.await().getOrNull() ?: current.gameStats,
                    bookStats = books.await().getOrNull() ?: current.bookStats,
                    offline = results.all { it.isFailure },
                )
            }
        }
    }

    private fun api() = container.session.api(baseUrl)

    class Factory(private val container: AppContainer, private val baseUrl: String) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            HomeViewModel(container, baseUrl) as T
    }
}
