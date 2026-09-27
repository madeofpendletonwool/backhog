package com.collinpendleton.backhog.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.AddBookRequest
import com.collinpendleton.backhog.api.BookShare
import com.collinpendleton.backhog.api.SharesOverview
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.ui.theme.Backhog
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class SharedBooksState(
    val loading: Boolean = true,
    val received: List<BookShare> = emptyList(),
    val sharedCount: Int = 0,
    /** entry ids the shelf took in this sitting — for the added checkmark. */
    val added: Set<String> = emptySet(),
    val error: String? = null,
)

/**
 * Shared books in Settings: the "shared with me" shelf. A share grants
 * access, never rows in someone else's library — a book appears here until
 * they add it themselves, and nothing of the lender's is visible beyond the
 * badge the API puts on the entry.
 */
class SharedBooksViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
) : ViewModel() {

    private val _state = MutableStateFlow(SharedBooksState())
    val state: StateFlow<SharedBooksState> = _state.asStateFlow()

    init {
        load()
    }

    private fun api() = container.session.api(baseUrl)

    fun load() {
        viewModelScope.launch {
            apiCall { api().sharesOverview() }
                .onSuccess { overview: SharesOverview ->
                    _state.update {
                        it.copy(
                            loading = false,
                            received = overview.received,
                            sharedCount = overview.shared.size,
                        )
                    }
                }
                .onFailure { e -> _state.update { it.copy(loading = false, error = e.message) } }
        }
    }

    fun addToShelf(share: BookShare) {
        viewModelScope.launch {
            apiCall { api().addBook(AddBookRequest(bookId = share.bookId)) }
                .onSuccess { entry ->
                    _state.update { it.copy(added = it.added + entry.id) }
                }
        }
    }

    class Factory(private val container: AppContainer, private val baseUrl: String) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            SharedBooksViewModel(container, baseUrl) as T
    }
}

@Composable
fun SharedBooksSection(container: AppContainer, baseUrl: String) {
    val vm: SharedBooksViewModel = viewModel(factory = SharedBooksViewModel.Factory(container, baseUrl))
    val state by vm.state.collectAsStateWithLifecycle()
    val p = Backhog.palette

    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        val loadError = state.error
        when {
            state.loading -> Text("Loading shared books…", style = MaterialTheme.typography.bodySmall, color = p.c500)
            loadError != null -> Text(loadError, style = MaterialTheme.typography.bodySmall, color = p.c500)
            else -> {
                if (state.received.isEmpty() && state.sharedCount == 0) {
                    Text(
                        "Books shared to you land here, with the files behind them. Nothing of the sharer's is ever visible.",
                        style = MaterialTheme.typography.bodySmall,
                        color = p.c500,
                    )
                }
                state.received.forEach { share ->
                    Row(
                        Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                            Text(
                                share.bookTitle ?: "A book",
                                style = MaterialTheme.typography.labelMedium,
                                color = p.c200,
                            )
                            Text(
                                "From ${share.ownerUsername ?: "someone"}",
                                style = MaterialTheme.typography.labelSmall,
                                color = p.c500,
                            )
                        }
                        if (share.inLibrary || share.bookId in state.added) {
                            Text("On your shelf", style = MaterialTheme.typography.labelSmall, color = p.c600)
                        } else {
                            TextButton(onClick = { vm.addToShelf(share) }) { Text("Add to shelf") }
                        }
                    }
                }
                if (state.sharedCount > 0) {
                    Text(
                        "You have shared ${state.sharedCount} book${if (state.sharedCount == 1) "" else "s"} out.",
                        style = MaterialTheme.typography.labelSmall,
                        color = p.c600,
                    )
                }
            }
        }
    }
}
