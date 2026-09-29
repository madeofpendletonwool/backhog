package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items as lazyItems
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Casino
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.CloudDownload
import androidx.compose.material.icons.filled.EmojiEvents
import androidx.compose.material.icons.filled.GridView
import androidx.compose.material.icons.filled.HourglassEmpty
import androidx.compose.material.icons.filled.Insights
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Layers
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Reorder
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.TrackChanges
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material.icons.filled.ViewList
import androidx.compose.material.icons.filled.VideogameAsset
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.data.LibraryLayout
import com.collinpendleton.backhog.data.LibrarySorts
import com.collinpendleton.backhog.data.ServerUrl
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones

/** The web's brand-purple fallback when no accent has been sampled yet. */
internal val AccentFallback = Color(0xFF8B5CF6)

/**
 * The games library: status tabs, title filter, facet panel, cover grid or
 * dense table — the web's LibraryPage, arena-pinned to `media=game`. The
 * header also carries the tonight dice and the arena's other destinations.
 */
@Composable
fun LibraryScreen(
    container: AppContainer,
    baseUrl: String,
    onOpenGame: (String) -> Unit,
    onOpenQueue: () -> Unit,
    onOpenDashboard: () -> Unit = {},
    onOpenLists: () -> Unit = {},
    onOpenSeries: () -> Unit = {},
    onOpenProjects: () -> Unit = {},
    onOpenAchievements: () -> Unit = {},
    onOpenDebt: () -> Unit = {},
) {
    val vm: LibraryViewModel = viewModel(key = "library|$baseUrl") {
        LibraryViewModel(container.session, baseUrl, container.preferences)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    var addOpen by rememberSaveable { mutableStateOf(false) }
    var filtersOpen by rememberSaveable { mutableStateOf(false) }
    var tonightOpen by rememberSaveable { mutableStateOf(false) }
    var menuOpen by rememberSaveable { mutableStateOf(false) }
    var steamOpen by rememberSaveable { mutableStateOf(false) }
    val p = Backhog.palette

    // Status/rating edits in detail should be visible the moment we come back.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.onReturned() }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text("Library", style = MaterialTheme.typography.headlineSmall, color = p.c100)
                Text(
                    if (ui.loading && ui.entries.isEmpty()) "Loading…"
                    else pluralGames(ui.total) + (if (ui.hasFilters) " matching your filters" else "") +
                        (if (ui.entries.size < ui.total) " · showing ${ui.entries.size}" else ""),
                    style = MaterialTheme.typography.bodySmall,
                    color = p.c400,
                )
            }
            IconButton(onClick = { tonightOpen = true }) {
                Icon(Icons.Filled.Casino, contentDescription = "What should I play tonight?", tint = p.hlBright)
            }
            IconButton(onClick = onOpenQueue) {
                Icon(Icons.Filled.Reorder, contentDescription = "Play queue", tint = p.c300)
            }
            IconButton(onClick = { menuOpen = true }) {
                Icon(Icons.Filled.Menu, contentDescription = "More", tint = p.c300)
            }
            IconButton(onClick = { addOpen = true }) {
                Icon(Icons.Filled.Add, contentDescription = "Add a game", tint = p.hlBright)
            }
        }

        Spacer(Modifier.height(12.dp))
        StatusTabs(ui.filters.status, vm::setStatus)

        Row(
            Modifier.padding(vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            OutlinedTextField(
                value = ui.search,
                onValueChange = vm::setSearch,
                placeholder = { Text("Filter by title…") },
                singleLine = true,
                leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null, tint = p.c500) },
                modifier = Modifier.weight(1f),
            )
            val filterLit = filtersOpen || ui.filters.platformId != null || ui.filters.genreId != null
            IconButton(onClick = { filtersOpen = !filtersOpen }) {
                Icon(Icons.Filled.Tune, contentDescription = "More filters", tint = if (filterLit) p.hlBright else p.c300)
            }
            val table = ui.filters.view == LibraryLayout.Table
            IconButton(onClick = { vm.setView(if (table) LibraryLayout.Grid else LibraryLayout.Table) }) {
                Icon(
                    if (table) Icons.Filled.GridView else Icons.Filled.ViewList,
                    contentDescription = if (table) "Grid view" else "Table view",
                    tint = p.c300,
                )
            }
        }

        if (filtersOpen) {
            FilterPanel(ui, vm)
            Spacer(Modifier.height(10.dp))
        }

        ErrorText(ui.error?.takeIf { ui.entries.isEmpty() })

        when {
            ui.loading && ui.entries.isEmpty() -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator()
            }
            ui.entries.isEmpty() && ui.error == null -> EmptyLibrary(ui.hasFilters, vm::clearFilters) { addOpen = true }
            else -> if (ui.filters.view == LibraryLayout.Grid) {
                GameGrid(ui, baseUrl, onOpenGame, vm::loadMore)
            } else {
                GameTable(ui, baseUrl, onOpenGame, vm::loadMore)
            }
        }
    }

    if (addOpen) {
        AddGameSheet(container, baseUrl, onDismiss = { addOpen = false })
    }
    if (tonightOpen) {
        TonightSheet(container, baseUrl, onDismiss = { tonightOpen = false }, onOpenGame = onOpenGame)
    }
    if (steamOpen) {
        SteamImportSheet(
            container,
            baseUrl,
            onDismiss = { steamOpen = false },
            onLibraryChanged = { vm.onReturned() },
        )
    }
    if (menuOpen) {
        ArenaMenuSheet(
            onDismiss = { menuOpen = false },
            onOpenDashboard = onOpenDashboard,
            onOpenLists = onOpenLists,
            onOpenSeries = onOpenSeries,
            onOpenProjects = onOpenProjects,
            onOpenAchievements = onOpenAchievements,
            onOpenDebt = onOpenDebt,
            onOpenSteam = {
                menuOpen = false
                steamOpen = true
            },
        )
    }
}

/** The arena's other destinations — the mobile stand-in for the web's sidebar. */
@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
private fun ArenaMenuSheet(
    onDismiss: () -> Unit,
    onOpenDashboard: () -> Unit,
    onOpenLists: () -> Unit,
    onOpenSeries: () -> Unit,
    onOpenProjects: () -> Unit,
    onOpenAchievements: () -> Unit,
    onOpenDebt: () -> Unit,
    onOpenSteam: () -> Unit,
) {
    val p = Backhog.palette
    androidx.compose.material3.ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = p.c950,
    ) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 4.dp)) {
            Text("Games", style = MaterialTheme.typography.titleLarge, color = p.c100)
            Spacer(Modifier.height(12.dp))
            MenuRow("Your Gaming Problem", "The ridiculous-stats dashboard", Icons.Filled.Insights) { onDismiss(); onOpenDashboard() }
            MenuRow("Lists", "Manual and smart lists", Icons.Filled.List) { onDismiss(); onOpenLists() }
            MenuRow("Series", "Franchises played as journeys", Icons.Filled.Layers) { onDismiss(); onOpenSeries() }
            MenuRow("Projects", "Temporary objectives", Icons.Filled.TrackChanges) { onDismiss(); onOpenProjects() }
            MenuRow("Achievements", "The trophy wall", Icons.Filled.EmojiEvents) { onDismiss(); onOpenAchievements() }
            MenuRow("Backlog debt", "What you owe yourself", Icons.Filled.HourglassEmpty) { onDismiss(); onOpenDebt() }
            MenuRow("Import from Steam", "Bulk-add your Steam library", Icons.Filled.CloudDownload) { onOpenSteam() }
            Spacer(Modifier.height(24.dp))
        }
    }
}

@Composable
private fun MenuRow(title: String, caption: String, icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    val p = Backhog.palette
    Row(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .clickable(onClick = onClick)
            .padding(horizontal = 8.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Icon(icon, contentDescription = null, tint = p.hlBright, modifier = Modifier.size(22.dp))
        Column {
            Text(title, style = MaterialTheme.typography.bodyLarge, color = p.c100)
            Text(caption, style = MaterialTheme.typography.labelSmall, color = p.c500)
        }
    }
}

private fun pluralGames(n: Int): String = "$n game${if (n == 1) "" else "s"}"

@Composable
private fun StatusTabs(selected: EntryStatus?, onSelect: (EntryStatus?) -> Unit) {
    val p = Backhog.palette
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        TabChip("All", selected == null, p) { onSelect(null) }
        EntryStatus.quick.forEach { status ->
            TabChip(status.gameLabel, selected == status, p) { onSelect(status) }
        }
    }
}

@Composable
private fun TabChip(text: String, active: Boolean, p: com.collinpendleton.backhog.ui.theme.Palette, onClick: () -> Unit) {
    Text(
        text,
        style = MaterialTheme.typography.labelMedium,
        color = if (active) p.c100 else p.c400,
        modifier = Modifier
            .clip(CircleShape)
            .background(if (active) p.fillActive else p.c850)
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 7.dp),
    )
}

/** A button that opens a dropdown sheet — also used for the filter panel toggle. */
@Composable
private fun FilterPanel(ui: LibraryUiState, vm: LibraryViewModel) {
    val p = Backhog.palette
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, MaterialTheme.shapes.medium)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        FilterSelect(
            label = "Sort by",
            selected = LibrarySorts.label(ui.filters.sort),
            options = LibrarySorts.all.map { it.second },
        ) { picked -> LibrarySorts.all.first { it.second == picked }.first.let(vm::setSort) }

        FilterSelect(
            label = "Platform",
            selected = ui.facets.platforms.firstOrNull { it.id == ui.filters.platformId }?.name ?: "Any platform",
            options = listOf("Any platform") + ui.facets.platforms.map { it.name },
        ) { picked ->
            vm.setPlatform(ui.facets.platforms.firstOrNull { it.name == picked }?.id)
        }

        FilterSelect(
            label = "Genre",
            selected = ui.facets.genres.firstOrNull { it.id == ui.filters.genreId }?.name ?: "Any genre",
            options = listOf("Any genre") + ui.facets.genres.map { it.name },
        ) { picked ->
            vm.setGenre(ui.facets.genres.firstOrNull { it.name == picked }?.id)
        }

        TextButton(onClick = vm::clearFilters, enabled = ui.hasFilters) {
            Icon(Icons.Filled.Close, contentDescription = null, modifier = Modifier.size(16.dp))
            Spacer(Modifier.width(4.dp))
            Text("Clear")
        }
    }
}

@Composable
private fun FilterSelect(label: String, selected: String, options: List<String>, onPick: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    val p = Backhog.palette
    Column {
        Text(label, style = MaterialTheme.typography.labelMedium, color = p.c400)
        Box {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .border(1.dp, p.edgeStrong, MaterialTheme.shapes.small)
                    .clickable { open = true }
                    .padding(horizontal = 12.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(selected, style = MaterialTheme.typography.bodyMedium, color = p.c200, modifier = Modifier.weight(1f))
                Icon(Icons.Filled.KeyboardArrowDown, contentDescription = null, tint = p.c500)
            }
            DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                options.forEach { option ->
                    DropdownMenuItem(
                        text = { Text(option) },
                        trailingIcon = if (option == selected) {
                            { Icon(Icons.Filled.Check, contentDescription = null, tint = p.hlBright) }
                        } else null,
                        onClick = {
                            open = false
                            onPick(option)
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun EmptyLibrary(hasFilters: Boolean, onClear: () -> Unit, onAdd: () -> Unit) {
    val p = Backhog.palette
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(vertical = 48.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        if (hasFilters) {
            Text("No games match", style = MaterialTheme.typography.titleMedium, color = p.c100)
            Text(
                "Try loosening the filters, or search for something new to add.",
                style = MaterialTheme.typography.bodyMedium,
                color = p.c400,
            )
            TextButton(onClick = onClear) { Text("Clear filters") }
        } else {
            Text("Your backlog is empty", style = MaterialTheme.typography.titleMedium, color = p.c100)
            Text(
                "Suspiciously empty. Add the games you own but haven't gotten around to — that's what this is for.",
                style = MaterialTheme.typography.bodyMedium,
                color = p.c400,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
            Button(onClick = onAdd) { Text("Add your first game") }
        }
    }
}

// --- the grid ---------------------------------------------------------------

@Composable
private fun GameGrid(ui: LibraryUiState, baseUrl: String, onOpen: (String) -> Unit, onMore: () -> Unit) {
    LazyVerticalGrid(
        columns = GridCells.Adaptive(minSize = 150.dp),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(top = 4.dp, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(ui.entries, key = { it.id }) { entry ->
            GameCard(entry, baseUrl) { onOpen(entry.id) }
        }
        if (ui.hasMore) {
            item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(maxLineSpan) }) {
                LoadMore(ui, onMore)
            }
        }
    }
}

/** A cover-led card: accent-tinted chrome, status dot, rating chip, scrim title. */
@Composable
private fun GameCard(entry: Entry, baseUrl: String, onOpen: () -> Unit) {
    val p = Backhog.palette
    val game = entry.game ?: return
    val accent = game.accentHexOrNull?.let { parseHex(it) } ?: AccentFallback
    val shape = MaterialTheme.shapes.medium

    Column(
        modifier = Modifier
            .clip(shape)
            .border(1.dp, accent.copy(alpha = 0.35f), shape)
            .background(p.c900)
            .clickable(onClick = onOpen),
    ) {
        Box(
            Modifier
                .fillMaxWidth()
                .aspectRatio(3f / 4f),
        ) {
            CoverImage(entry, baseUrl, Modifier.fillMaxSize())
            // Bottom scrim carrying the title, always legible over any art.
            Box(
                Modifier
                    .fillMaxWidth()
                    .align(Alignment.BottomCenter)
                    .background(
                        Brush.verticalGradient(
                            0f to Color.Transparent,
                            0.35f to Color.Black.copy(alpha = 0.72f),
                            1f to Color.Black.copy(alpha = 0.88f),
                        ),
                    )
                    .padding(horizontal = 8.dp, vertical = 6.dp),
            ) {
                Column {
                    Text(
                        game.name,
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = Color.White,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    val meta = listOfNotNull(
                        Format.releaseYear(game.firstReleaseDate).takeIf { it.isNotEmpty() },
                        game.timeToBeatMain?.let { Format.duration(it) }?.takeIf { d -> d != "—" },
                    ).joinToString(" · ")
                    if (meta.isNotEmpty()) {
                        Text(meta, style = MaterialTheme.typography.labelSmall, color = Color(0xFFCBD5E1))
                    }
                }
            }
            // Status dot, top-left — the colour carries the state.
            Box(
                Modifier
                    .align(Alignment.TopStart)
                    .padding(6.dp)
                    .size(12.dp)
                    .clip(CircleShape)
                    .background(Tones.forStatus(entry.status))
                    .border(1.dp, Color.Black.copy(alpha = 0.4f), CircleShape),
            )
            if (entry.userRating != null) {
                Row(
                    modifier = Modifier
                        .align(Alignment.TopEnd)
                        .padding(6.dp)
                        .clip(MaterialTheme.shapes.extraSmall)
                        .background(Color.Black.copy(alpha = 0.65f))
                        .padding(horizontal = 5.dp, vertical = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(2.dp),
                ) {
                    Icon(Icons.Filled.Star, contentDescription = null, tint = Tones.Wishlist, modifier = Modifier.size(11.dp))
                    Text("${entry.userRating}", style = MaterialTheme.typography.labelSmall, color = Tones.Wishlist)
                }
            }
        }
    }
}

/** Cover via the server's public cache; a tinted slab while it loads or is missing. */
@Composable
fun CoverImage(entry: Entry, baseUrl: String, modifier: Modifier = Modifier) {
    val accent = (if (entry.isBook) entry.book?.accentHex else entry.game?.accentHexOrNull)
        ?.takeIf { it.isNotBlank() }
        ?.let { parseHex(it) } ?: AccentFallback
    val url = when {
        entry.isBook -> entry.book?.let { "${baseUrl.trimEnd('/')}/api/covers/book/${it.id}" }
                ?: ServerUrl.gameCoverUrl(baseUrl, entry.game?.id ?: 0L)
        else -> entry.game?.let { ServerUrl.gameCoverUrl(baseUrl, it.id) }
    }
    Box(modifier.background(accent.copy(alpha = 0.14f)), contentAlignment = Alignment.Center) {
        if (url != null) {
            AsyncImage(
                model = url,
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            Icon(Icons.Filled.VideogameAsset, contentDescription = null, tint = accent.copy(alpha = 0.6f))
        }
    }
}

/** "#8b5cf6" → Color, null on anything malformed. */
internal fun parseHex(hex: String): Color? = runCatching {
    Color(android.graphics.Color.parseColor(hex.trim()))
}.getOrNull()

// --- the dense table ----------------------------------------------------------

@Composable
private fun GameTable(ui: LibraryUiState, baseUrl: String, onOpen: (String) -> Unit, onMore: () -> Unit) {
    val p = Backhog.palette
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(top = 4.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        lazyItems(ui.entries, key = { it.id }) { entry ->
            val game = entry.game ?: return@lazyItems
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.small)
                    .background(p.c900)
                    .clickable { onOpen(entry.id) }
                    .padding(horizontal = 10.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                CoverImage(entry, baseUrl, Modifier.size(36.dp).clip(MaterialTheme.shapes.extraSmall))
                Column(Modifier.weight(1f)) {
                    Text(
                        game.name,
                        style = MaterialTheme.typography.bodyMedium,
                        color = p.c100,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    val line = listOfNotNull(
                        Format.releaseYear(game.firstReleaseDate).takeIf { it.isNotEmpty() } ?: "—",
                        game.genres.joinToString(", ").takeIf { it.isNotEmpty() } ?: "—",
                        game.timeToBeatMain?.let { Format.duration(it) }?.takeIf { it != "—" },
                    ).joinToString(" · ")
                    Text(line, style = MaterialTheme.typography.labelSmall, color = p.c500, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(3.dp)) {
                    ToneChip(entry.status.gameLabel, Tones.forStatus(entry.status))
                    val rating = entry.userRating?.let { "★ $it" }
                        ?: game.igdbRating?.let { "${Math.round(it)}" } ?: "—"
                    Text(rating, style = MaterialTheme.typography.labelSmall, color = if (entry.userRating != null) Tones.Wishlist else p.c500)
                }
            }
        }
        if (ui.hasMore) {
            item { LoadMore(ui, onMore) }
        }
    }
}

@Composable
private fun LoadMore(ui: LibraryUiState, onMore: () -> Unit) {
    val p = Backhog.palette
    Column(
        modifier = Modifier.fillMaxWidth().padding(vertical = 14.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Button(onClick = onMore, enabled = !ui.loadingMore) {
            if (ui.loadingMore) {
                CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary)
            } else {
                Text("Load more")
            }
        }
        Text("${ui.entries.size} of ${ui.total}", style = MaterialTheme.typography.labelSmall, color = p.c500)
    }
}
