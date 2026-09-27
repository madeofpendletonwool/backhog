package com.collinpendleton.backhog.ui.books

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.MenuBook
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Apps
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.FilterList
import androidx.compose.material.icons.filled.Insights
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.ViewList
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.Status
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/**
 * The shelf. Deliberately the same controls the games shelf has — status
 * tabs, search, facets, sort, grid ↔ table — because the two arenas are one
 * app and a reader should not have to learn a second set of controls.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BookLibraryScreen(
    container: AppContainer,
    baseUrl: String,
    onOpen: (String) -> Unit,
    onDashboard: () -> Unit,
) {
    val vm: BookLibraryViewModel = viewModel(factory = BookLibraryViewModel.Factory(container, baseUrl))
    val state by vm.state.collectAsState()
    val p = Backhog.palette
    var showAdd by remember { mutableStateOf(false) }
    var showFilters by remember { mutableStateOf(false) }

    Scaffold(
        containerColor = p.c950,
        topBar = {
            TopAppBar(
                title = { Text("Shelf") },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = p.c950,
                    titleContentColor = p.c100,
                    navigationIconContentColor = p.c300,
                    actionIconContentColor = p.c300,
                ),
                actions = {
                    IconButton(onClick = onDashboard) {
                        Icon(Icons.Filled.Insights, contentDescription = "Reading dashboard")
                    }
                    IconButton(onClick = { vm.setGrid(!state.shelf.grid) }) {
                        Icon(
                            if (state.shelf.grid) Icons.Filled.ViewList else Icons.Filled.Apps,
                            contentDescription = if (state.shelf.grid) "Table view" else "Grid view",
                        )
                    }
                },
            )
        },
        floatingActionButton = {
            androidx.compose.material3.ExtendedFloatingActionButton(
                onClick = { showAdd = true },
                containerColor = p.hlMid,
                contentColor = p.hlInk,
                icon = { Icon(Icons.Filled.Add, contentDescription = null) },
                text = { Text("Add a book") },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            StatsStrip(state.stats)

            OutlinedTextField(
                value = state.search,
                onValueChange = vm::setSearch,
                placeholder = { Text("Search your shelf…") },
                leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null) },
                trailingIcon = {
                    if (state.search.isNotEmpty()) {
                        IconButton(onClick = { vm.setSearch("") }) {
                            Icon(Icons.Filled.Clear, contentDescription = "Clear search")
                        }
                    }
                },
                singleLine = true,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 10.dp),
            )

            Row(
                Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                StatusTab("", "All", state.shelf.status, vm::setStatus)
                Status.Quick.forEach { status ->
                    StatusTab(status.name.lowercase(), status.bookLabel, state.shelf.status, vm::setStatus)
                }
                AssistChip(
                    onClick = { showFilters = true },
                    label = { Text("Filters") },
                    leadingIcon = { Icon(Icons.Filled.FilterList, contentDescription = null, modifier = Modifier.size(16.dp)) },
                )
                SortMenu(state.shelf.sort, vm::setSort)
                Spacer(Modifier.size(4.dp))
            }

            val loadError = state.error
            if (loadError != null) {
                Text(
                    loadError,
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.padding(16.dp),
                )
            }

            when {
                state.loading && state.entries.isEmpty() -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
                state.entries.isEmpty() && state.error == null -> EmptyShelf(state.hasFilters) {
                    showAdd = true
                }
                else -> {
                    val grid = state.entries
                    if (state.shelf.grid) {
                        BookGrid(grid, baseUrl, state.total, onOpen, vm::loadMore, state.loadingMore, state.hasMore)
                    } else {
                        BookTable(grid, baseUrl, state.total, onOpen, vm::loadMore, state.loadingMore, state.hasMore)
                    }
                }
            }
        }
    }

    if (showAdd) {
        ModalBottomSheet(onDismissRequest = { showAdd = false }) {
            AddBookSheet(
                container = container,
                baseUrl = baseUrl,
                onAdded = {
                    showAdd = false
                    vm.refresh()
                },
            )
        }
    }

    if (showFilters) {
        val sheetState = rememberModalBottomSheetState()
        ModalBottomSheet(onDismissRequest = { showFilters = false }, sheetState = sheetState) {
            FiltersSheet(
                state = state,
                onAuthor = vm::setAuthor,
                onSubject = vm::setSubject,
                onLanguage = vm::setLanguage,
                onClear = {
                    vm.clearFilters()
                    showFilters = false
                },
            )
        }
    }
}

@Composable
private fun StatusTab(value: String, label: String, current: String, pick: (String) -> Unit) {
    val p = Backhog.palette
    val active = current == value
    Text(
        label,
        style = MaterialTheme.typography.labelMedium,
        color = if (active) p.c100 else p.c400,
        modifier = Modifier
            .clip(CircleShape)
            .background(if (active) p.fillActive else p.c850)
            .clickable { pick(value) }
            .padding(horizontal = 14.dp, vertical = 7.dp),
    )
}

@Composable
private fun SortMenu(current: String, pick: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        AssistChip(
            onClick = { open = true },
            label = {
                Text(
                    SHELF_SORTS.firstOrNull { it.first == current }?.second ?: "Sort",
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            },
        )
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            SHELF_SORTS.forEach { (value, label) ->
                DropdownMenuItem(
                    text = { Text(label) },
                    onClick = {
                        open = false
                        pick(value)
                    },
                )
            }
        }
    }
}

@Composable
private fun BookGrid(
    entries: List<Entry>,
    baseUrl: String,
    total: Int,
    onOpen: (String) -> Unit,
    onMore: () -> Unit,
    loadingMore: Boolean,
    hasMore: Boolean,
) {
    val listState = rememberLazyGridState()
    // The next page loads as the end of the shelf comes into view.
    val atEnd by remember {
        derivedStateOf {
            val layout = listState.layoutInfo
            val last = layout.visibleItemsInfo.lastOrNull()?.index ?: 0
            layout.totalItemsCount > 0 && last >= layout.totalItemsCount - 6
        }
    }
    LaunchedEffect(atEnd, entries.size) {
        if (atEnd && hasMore && !loadingMore) onMore()
    }

    LazyVerticalGrid(
        columns = GridCells.Adaptive(104.dp),
        state = listState,
        contentPadding = androidx.compose.foundation.layout.PaddingValues(
            start = 16.dp, end = 16.dp, top = 12.dp, bottom = 96.dp,
        ),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(entries, key = { it.id }) { entry ->
            Column(
                Modifier
                    .fillMaxWidth()
                    .clickable { onOpen(entry.id) },
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                BookCover(
                    entry.book ?: return@items,
                    baseUrl,
                    Modifier
                        .fillMaxWidth()
                        .aspectRatio(2f / 3f),
                )
                BookCaption(entry, compact = true)
                EntryProgress(entry)
            }
        }
        if (hasMore || loadingMore) {
            item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(maxLineSpan) }) {
                Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) {
                    if (loadingMore) CircularProgressIndicator(Modifier.size(22.dp))
                    else TextButton(onClick = onMore) { Text("Load more — showing ${entries.size} of $total") }
                }
            }
        }
    }
}

@Composable
private fun BookTable(
    entries: List<Entry>,
    baseUrl: String,
    total: Int,
    onOpen: (String) -> Unit,
    onMore: () -> Unit,
    loadingMore: Boolean,
    hasMore: Boolean,
) {
    val listState = androidx.compose.foundation.lazy.rememberLazyListState()
    val atEnd by remember {
        derivedStateOf {
            val layout = listState.layoutInfo
            val last = layout.visibleItemsInfo.lastOrNull()?.index ?: 0
            layout.totalItemsCount > 0 && last >= layout.totalItemsCount - 8
        }
    }
    LaunchedEffect(atEnd, entries.size) {
        if (atEnd && hasMore && !loadingMore) onMore()
    }

    val p = Backhog.palette
    androidx.compose.foundation.lazy.LazyColumn(
        state = listState,
        contentPadding = androidx.compose.foundation.layout.PaddingValues(
            start = 16.dp, end = 16.dp, top = 12.dp, bottom = 96.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(4.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(entries.size, key = { entries[it].id }) { index ->
            val entry = entries[index]
            val book = entry.book ?: return@items
            Row(
                Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .clickable { onOpen(entry.id) }
                    .padding(horizontal = 8.dp, vertical = 6.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                BookCover(book, baseUrl, Modifier.size(width = 38.dp, height = 56.dp))
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    BookCaption(entry)
                }
                Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    com.collinpendleton.backhog.ui.components.ToneChip(
                        entry.status.bookLabel,
                        when (entry.status) {
                            Status.Backlog -> Tones.Backlog
                            Status.Playing -> Tones.Playing
                            Status.Played -> Tones.Played
                            Status.Dropped, Status.Ignored -> Tones.Dropped
                            Status.Wishlist -> Tones.Silver
                        },
                    )
                    entry.loggedMinutes.takeIf { it > 0 }?.let {
                        Text("${it / 60}h ${it % 60}m", style = MaterialTheme.typography.labelSmall, color = p.c500)
                    }
                }
            }
        }
        if (hasMore) {
            item {
                Box(Modifier.fillMaxWidth().padding(12.dp), contentAlignment = Alignment.Center) {
                    if (loadingMore) CircularProgressIndicator(Modifier.size(22.dp))
                    else TextButton(onClick = onMore) { Text("Load more — showing ${entries.size} of $total") }
                }
            }
        }
    }
}

@Composable
private fun EmptyShelf(hasFilters: Boolean, onAdd: () -> Unit) {
    val p = Backhog.palette
    Column(
        Modifier
            .fillMaxSize()
            .padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            Icons.AutoMirrored.Filled.MenuBook,
            contentDescription = null,
            tint = p.c600,
            modifier = Modifier.size(42.dp),
        )
        Text(
            if (hasFilters) "Nothing matches your filters" else "Nothing on the shelf yet",
            style = MaterialTheme.typography.titleMedium,
            color = p.c200,
        )
        if (!hasFilters) {
            Text(
                "Scan a barcode, type an ISBN, or search by title.",
                style = MaterialTheme.typography.bodyMedium,
                color = p.c500,
            )
            TextButton(onClick = onAdd) { Text("Add your first book") }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun FiltersSheet(
    state: BookLibraryState,
    onAuthor: (String) -> Unit,
    onSubject: (String) -> Unit,
    onLanguage: (String) -> Unit,
    onClear: () -> Unit,
) {
    val p = Backhog.palette
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp)
            .padding(bottom = 32.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Text("Filters", style = MaterialTheme.typography.titleLarge, color = p.c100)
        val facets = state.facets
        if (facets == null) {
            Text("Loading facets…", color = p.c500)
        } else {
            FilterRail("Author", facets.authors, state.shelf.author, onAuthor)
            FilterRail("Subject", facets.subjects.take(30), state.shelf.subject, onSubject)
            FilterRail("Language", facets.languages, state.shelf.language, onLanguage)
        }
        TextButton(onClick = onClear, modifier = Modifier.fillMaxWidth()) { Text("Clear all filters") }
    }
}

@Composable
private fun FilterRail(label: String, values: List<String>, current: String, pick: (String) -> Unit) {
    val p = Backhog.palette
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionLabel(label)
        if (values.isEmpty()) Text("None on file yet", color = p.c500, style = MaterialTheme.typography.bodySmall)
        Row(
            Modifier.horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            values.forEach { value ->
                FilterChip(
                    selected = current == value,
                    onClick = { pick(if (current == value) "" else value) },
                    label = { Text(value) },
                )
            }
        }
    }
}
