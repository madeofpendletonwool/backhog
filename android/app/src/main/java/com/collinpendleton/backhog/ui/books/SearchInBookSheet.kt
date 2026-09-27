package com.collinpendleton.backhog.ui.books

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.BookSearchAny
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.books.chapterTitle
import com.collinpendleton.backhog.books.formatPage
import com.collinpendleton.backhog.books.formatTimecode
import com.collinpendleton.backhog.ui.theme.Backhog
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class SearchInBookState(
    val query: String = "",
    val running: Boolean = false,
    val error: String? = null,
    val results: BookSearchAny? = null,
)

class SearchInBookViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
    private val entryId: String,
) : ViewModel() {

    private val _state = MutableStateFlow(SearchInBookState())
    val state: StateFlow<SearchInBookState> = _state.asStateFlow()

    init {
        @OptIn(FlowPreview::class)
        viewModelScope.launch {
            _state
                .debounce { if (it.query.trim().length >= 3) 300L else Long.MAX_VALUE }
                .collect { run(it.query) }
        }
    }

    fun setQuery(text: String) = _state.update { it.copy(query = text, error = null) }

    private fun run(term: String) {
        if (term.trim().length < 3) {
            _state.update { it.copy(results = null, running = false, error = null) }
            return
        }
        _state.update { it.copy(running = true, error = null) }
        viewModelScope.launch {
            apiCall { container.session.api(baseUrl).searchInBook(entryId, term.trim()) }
                .onSuccess { results ->
                    if (_state.value.query.trim() == term.trim()) {
                        _state.update { it.copy(results = results, running = false) }
                    }
                }
                .onFailure { e ->
                    if (_state.value.query.trim() == term.trim()) {
                        // 422 is an answer in words — too short, or lettering
                        // not read yet — not a failure.
                        _state.update {
                            it.copy(
                                running = false,
                                error = if ((e as? ApiError)?.status == 422) e.message else e.message,
                            )
                        }
                    }
                }
        }
    }

    class Factory(
        private val container: AppContainer,
        private val baseUrl: String,
        private val entryId: String,
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            SearchInBookViewModel(container, baseUrl, entryId) as T
    }
}

/**
 * Search inside one book. Every hit comes back already placed in the other
 * coordinates — chapter, printed page with its error bar, audio timestamp —
 * because the offset is the answer and those are the same answer said in the
 * spaces a reader can act on. Text hits jump into the reader as a peek — a
 * look that never overwrites the stored place; page hits jump the paged
 * reader the same way. The player jump arrives with Stage 6.
 */
@Composable
fun SearchInBookSheet(
    container: AppContainer,
    baseUrl: String,
    entryId: String,
    entry: Entry?,
    onOpen: (String) -> Unit,
    onJump: (offset: Long) -> Unit = {},
) {
    val vm: SearchInBookViewModel = viewModel(factory = SearchInBookViewModel.Factory(container, baseUrl, entryId))
    val state by vm.state.collectAsState()
    val p = Backhog.palette
    // The jump targets arrive with Stages 5–6; the buttons say so honestly.
    var notice by remember { mutableStateOf<String?>(null) }

    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp)
            .padding(bottom = 40.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Search in this book", style = MaterialTheme.typography.titleLarge, color = p.c100)

        OutlinedTextField(
            value = state.query,
            onValueChange = vm::setQuery,
            placeholder = { Text("A few remembered words…") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )

        notice?.let {
            Text(it, style = MaterialTheme.typography.labelMedium, color = p.hlBright)
        }

        val searchError = state.error
        when {
            searchError != null -> Text(
                searchError,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodyMedium,
            )
            state.running -> Row(
                Modifier.fillMaxWidth().padding(vertical = 20.dp),
                horizontalArrangement = Arrangement.Center,
            ) { CircularProgressIndicator() }
            state.results == null -> Text(
                "Three characters or more. The book is searched folded — case and punctuation don't matter.",
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
            )
            else -> {
                val results = state.results!!
                Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(
                        when {
                            results.total == 0 -> "No matches."
                            results.mode == "loose" -> "Not found as typed — closest passages instead:"
                            else -> "${results.total} match${if (results.total == 1) "" else "es"}" +
                                if (results.truncated) " (showing the first ${results.results.size + results.pageResults.size})" else ""
                        },
                        style = MaterialTheme.typography.labelMedium,
                        color = p.c400,
                    )
                }
                LazyColumn(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    items(results.results) { hit ->
                        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(
                                buildString {
                                    append(hit.context.before)
                                    append(hit.context.passage)
                                    append(hit.context.after)
                                },
                                style = MaterialTheme.typography.bodySmall,
                                color = p.c300,
                                maxLines = 3,
                                overflow = TextOverflow.Ellipsis,
                            )
                            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                                hit.chapter?.let { Text(chapterTitle(it), style = MaterialTheme.typography.labelSmall, color = p.c500) }
                                hit.page?.let {
                                    Text(
                                        formatPage(it) ?: "",
                                        style = MaterialTheme.typography.labelSmall,
                                        color = p.c500,
                                    )
                                }
                                hit.audio?.let {
                                    Text(
                                        "at ${formatTimecode(it.seconds)}",
                                        style = MaterialTheme.typography.labelSmall,
                                        color = p.c500,
                                    )
                                }
                            }
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                OutlinedButton(onClick = { onJump(hit.charOffset) }) {
                                    Text("Read here")
                                }
                                if (hit.audio != null) {
                                    OutlinedButton(onClick = { notice = "The player lands in Stage 6." }) {
                                        Text("Listen here")
                                    }
                                }
                            }
                        }
                    }
                    items(results.pageResults) { hit ->
                        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(
                                buildString {
                                    append(hit.context.before)
                                    append(hit.context.passage)
                                    append(hit.context.after)
                                },
                                style = MaterialTheme.typography.bodySmall,
                                color = p.c300,
                                maxLines = 3,
                                overflow = TextOverflow.Ellipsis,
                            )
                            Text(
                                "page ${hit.pageIndex + 1} · ${Math.round(hit.percent)}%",
                                style = MaterialTheme.typography.labelSmall,
                                color = p.c500,
                            )
                            OutlinedButton(onClick = { onJump(hit.pageIndex.toLong()) }) {
                                Text("View page")
                            }
                        }
                    }
                }
            }
        }
    }
}
