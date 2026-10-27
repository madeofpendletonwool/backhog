package com.gooseberrydevelopment.backhog.ui.books.reader

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.MenuBook
import androidx.compose.material.icons.filled.Headphones
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import com.gooseberrydevelopment.backhog.AppContainer
import com.gooseberrydevelopment.backhog.api.apiCall
import com.gooseberrydevelopment.backhog.books.chapterTitle
import com.gooseberrydevelopment.backhog.books.describeToc
import com.gooseberrydevelopment.backhog.books.isInferredTitle
import com.gooseberrydevelopment.backhog.data.ReaderPrefs
import com.gooseberrydevelopment.backhog.data.ServerUrl
import com.gooseberrydevelopment.backhog.ui.books.SearchInBookSheet
import com.gooseberrydevelopment.backhog.ui.theme.Backhog
import com.gooseberrydevelopment.backhog.ui.theme.ThemeFamily
import com.gooseberrydevelopment.backhog.ui.theme.ThemeId
import kotlinx.coroutines.launch
import kotlin.math.min
import kotlin.math.roundToInt

/**
 * The reading surfaces — the one place in the app that names colours instead
 * of reaching for an ink token, because the column is not a themed panel: it
 * is paper, and it stays the same paper in every theme. Every pairing clears
 * the contrast floor against its own fill, the same rule the ink ladder
 * keeps it by, solved once here.
 *
 * What the *theme* decides is which paper you start on: "auto" picks the
 * paper that matches the room (Library families read sepia, Midnight dark).
 */
data class ReadingSurface(val label: String, val bg: Color, val fg: Color, val muted: Color, val rule: Color) {
    companion object {
        val Night = ReadingSurface("Night", Color(0xFF101118), Color(0xFFE7E8EE), Color(0xFF8B8FA0), Color(0xFF262935))
        val Paper = ReadingSurface("Paper", Color(0xFFF3ECDD), Color(0xFF292217), Color(0xFF6B6049), Color(0xFFDCD0B8))
        val Day = ReadingSurface("Day", Color(0xFFFBFBFD), Color(0xFF15161B), Color(0xFF5B5F6E), Color(0xFFE1E2E9))

        /** Which paper a theme implies when the reader is left on "auto". */
        fun autoFor(theme: ThemeId): ReadingSurface = when (theme.family) {
            ThemeFamily.Library -> Paper
            ThemeFamily.Flat -> Night
        }

        fun named(key: String): ReadingSurface? = when (key) {
            "dark" -> Night
            "sepia" -> Paper
            "light" -> Day
            else -> null
        }
    }
}

/**
 * The in-app reader. Which half opens is a property of the entry's
 * classification: an image-native PDF answers `position_mode: "page"` and
 * reads as pages; every other book — EPUB, MOBI, a text-native PDF — reads
 * as paginated prose.
 *
 * Pagination here is a *view* over the paragraph offsets that are the truth:
 * pages are built from measured block heights, a page turn reports the
 * offset of the paragraph it lands on, and restore re-opens the page holding
 * the stored paragraph. Change the type and the paragraph you were on is
 * still the paragraph you are on — the acceptance criterion, kept by
 * construction.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ReaderScreen(
    container: AppContainer,
    baseUrl: String,
    entryId: String,
    jumpOffset: Long?,
    jumpPeek: Boolean,
    onBack: () -> Unit,
) {
    val vm: ReaderViewModel = viewModel(
        factory = ReaderViewModel.Factory(container, baseUrl, entryId, jumpOffset, jumpPeek),
    )
    val state by vm.state.collectAsState()

    val prefs by container.preferences.readerPrefs.collectAsState(initial = ReaderPrefs())
    val theme by container.preferences.themes.collectAsState(initial = com.gooseberrydevelopment.backhog.data.ThemeSettings())
    val surface = remember(prefs.surface, theme.books) {
        ReadingSurface.named(prefs.surface) ?: ReadingSurface.autoFor(theme.books)
    }

    val lifecycleOwner = androidx.compose.ui.platform.LocalLifecycleOwner.current
    androidx.compose.runtime.DisposableEffect(lifecycleOwner) {
        lifecycleOwner.lifecycle.addObserver(vm)
        onDispose { lifecycleOwner.lifecycle.removeObserver(vm) }
    }

    var showContents by remember { mutableStateOf(false) }
    var showType by remember { mutableStateOf(false) }
    var showSearch by remember { mutableStateOf(false) }

    /**
     * "Start narration here": the reader's half of the handoff. The stored
     * position's audio view is derived exactly when an alignment exists —
     * the same check the web's player makes — and the translation from the
     * paragraph on screen to a second on the tape is the server's.
     */
    val aligned = state.position?.audio?.derived == true
    val listenFromHere: (() -> Unit)? = if (aligned) {
        {
            val from = state.liveOffset
            container.appScope.launch {
                val api = container.session.api(baseUrl) ?: return@launch
                val seconds = apiCall { api.translatePosition(entryId, char = from) }
                    .getOrNull()
                    ?.audio
                    ?.seconds
                if (seconds != null) container.player.open(entryId, startAt = seconds)
            }
        }
    } else {
        null
    }

    Box(Modifier.fillMaxSize().background(surface.bg)) {
        when (state.mode) {
            ReaderMode.Loading -> Column(
                Modifier.fillMaxSize(),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) { CircularProgressIndicator(color = surface.muted) }

            ReaderMode.Failed -> ReaderProblem(state.error, surface, onBack)

            ReaderMode.Paged -> PagedReader(
                state = state,
                surface = surface,
                baseUrl = baseUrl,
                onBack = onBack,
                onSearch = { showSearch = true },
                onTurn = vm::reportPageTurn,
                onBackToPlace = { vm.jumpTo((state.position?.pageIndex ?: 0).toLong(), peek = false) },
                onReadFromHere = vm::endPeek,
                onLanded = vm::landed,
            )

            ReaderMode.Text -> TextReader(
                state = state,
                surface = surface,
                prefs = prefs,
                baseUrl = baseUrl,
                onBack = onBack,
                onSearch = { showSearch = true },
                onContents = { showContents = true },
                onType = { showType = true },
                onTurn = vm::reportTextPage,
                onGoToChapter = vm::goToChapter,
                onGoToChapterEnd = vm::goToChapterEnd,
                onBackToPlace = vm::backToMyPlace,
                onReadFromHere = vm::endPeek,
                onLanded = vm::landed,
                onListenFromHere = listenFromHere,
            )
        }
    }

    if (showContents) {
        ModalBottomSheet(onDismissRequest = { showContents = false }) {
            ContentsDrawer(state, surface) { chapter ->
                showContents = false
                vm.goToChapter(chapter)
            }
        }
    }
    if (showType) {
        ModalBottomSheet(onDismissRequest = { showType = false }) {
            TypeControls(container, prefs, surface)
        }
    }
    if (showSearch) {
        ModalBottomSheet(onDismissRequest = { showSearch = false }) {
            SearchInBookSheet(
                container = container,
                baseUrl = baseUrl,
                entryId = entryId,
                entry = null,
                onOpen = {},
                onJump = { offset ->
                    showSearch = false
                    vm.jumpTo(offset, peek = true)
                },
            )
        }
    }
}

/* --------------------------------------------------------------- chrome */

@Composable
private fun ReaderChrome(
    surface: ReadingSurface,
    title: String,
    progress: Double,
    progressLabel: String?,
    onBack: () -> Unit,
    actions: (@Composable () -> Unit)? = null,
    peekBanner: (@Composable () -> Unit)? = null,
) {
    Column(Modifier.fillMaxWidth().background(surface.bg).statusBarsPadding()) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 4.dp, vertical = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back", tint = surface.muted)
            }
            Text(
                title,
                style = MaterialTheme.typography.labelLarge,
                color = surface.fg,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            actions?.invoke()
        }
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            LinearProgressIndicator(
                progress = { (progress / 100.0).toFloat().coerceIn(0f, 1f) },
                modifier = Modifier.weight(1f).height(2.dp).clip(CircleShape),
                color = surface.muted,
                trackColor = surface.rule,
            )
            Text(
                progressLabel ?: "${progress.roundToInt()}%",
                style = MaterialTheme.typography.labelSmall,
                color = surface.muted,
            )
        }
        peekBanner?.invoke()
    }
}

/* ---------------------------------------------------------- text reader */

/** One thing laid on a page: a block's lines, an illustration, or an ornament. */
private sealed interface PageItem {
    val blockIndex: Int

    data class Text(override val blockIndex: Int, val look: BlockLook) : PageItem
    data class Image(override val blockIndex: Int, val href: String, val alt: String?) : PageItem

    /** A scene break's ⁂, or the rule closing a chapter opener. */
    data class Ornament(override val blockIndex: Int, val rule: Boolean) : PageItem

    /** The air above a chapter's opener, kept even at the top of a page. */
    data class Lead(override val blockIndex: Int) : PageItem
}

@Composable
private fun TextReader(
    state: ReaderState,
    surface: ReadingSurface,
    prefs: ReaderPrefs,
    baseUrl: String,
    onBack: () -> Unit,
    onSearch: () -> Unit,
    onContents: () -> Unit,
    onType: () -> Unit,
    onTurn: (Long) -> Unit,
    onGoToChapter: (com.gooseberrydevelopment.backhog.api.TextChapter) -> Unit,
    onGoToChapterEnd: (com.gooseberrydevelopment.backhog.api.TextChapter) -> Unit,
    onBackToPlace: () -> Unit,
    onReadFromHere: () -> Unit,
    onLanded: () -> Unit,
    onListenFromHere: (() -> Unit)? = null,
) {
    val face = if (prefs.face == "serif") FontFamily.Serif else FontFamily.Default
    val base = TextStyle(
        fontFamily = face,
        fontSize = prefs.fontSize.sp,
        lineHeight = (prefs.fontSize * prefs.lineHeight).sp,
        color = surface.fg,
    )
    val toc = state.toc
    val tocIndex = toc.indexOfFirst { it.spineIndex == state.spine }
    val previous = if (tocIndex > 0) toc[tocIndex - 1] else null
    val next = if (tocIndex in 0..(toc.size - 2)) toc[tocIndex + 1] else null

    // Every block's look, computed once: the measuring and the drawing below
    // both read these, so a page can never come out taller than measured.
    val items: List<PageItem> = remember(state.blocks, prefs, surface) {
        val laid = com.gooseberrydevelopment.backhog.books.layoutBlocks(state.blocks)
        buildList {
            val opener = laid.firstOrNull()?.role
            if (opener == com.gooseberrydevelopment.backhog.books.BlockRole.Label ||
                opener == com.gooseberrydevelopment.backhog.books.BlockRole.Title
            ) {
                add(PageItem.Lead(0))
            }
            state.blocks.forEachIndexed { index, block ->
                block.images.forEach { image -> add(PageItem.Image(index, image.href, image.alt)) }
                if (laid[index].breakBefore) add(PageItem.Ornament(index, rule = false))
                if (block.text.isNotEmpty() || block.images.isEmpty()) {
                    add(PageItem.Text(index, blockLook(block, laid[index], base, prefs, surface.muted)))
                }
                if (laid[index].endsOpener) add(PageItem.Ornament(index, rule = true))
            }
        }
    }

    Column(Modifier.fillMaxSize()) {
        ReaderChrome(
            surface = surface,
            title = state.chapter?.let { chapterTitle(it) } ?: "",
            progress = state.percent,
            progressLabel = null,
            onBack = onBack,
            actions = {
                onListenFromHere?.let { listen ->
                    IconButton(onClick = listen) {
                        Icon(Icons.Filled.Headphones, contentDescription = "Start narration here", tint = surface.muted)
                    }
                }
                IconButton(onClick = onSearch) {
                    Icon(Icons.Filled.Search, contentDescription = "Search inside", tint = surface.muted)
                }
                IconButton(onClick = onContents) {
                    Icon(Icons.AutoMirrored.Filled.MenuBook, contentDescription = "Contents", tint = surface.muted)
                }
                IconButton(onClick = onType) {
                    Icon(Icons.Filled.Tune, contentDescription = "Reading settings", tint = surface.muted)
                }
            },
            peekBanner = if (state.peek) {
                { PeekBanner(surface = surface, state = state, onBackToPlace = onBackToPlace, onReadFromHere = onReadFromHere) }
            } else null,
        )

        // The page is what is left between the chrome and the chapter bar —
        // measured here, where the pages actually draw, not across the
        // whole screen.
        BoxWithConstraints(Modifier.weight(1f).fillMaxWidth()) {
            when {
                state.display == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(color = surface.muted)
                }
                items.isEmpty() -> Column(
                    Modifier.fillMaxSize().padding(32.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.Center,
                ) {
                    Text(
                        "This section has no text — usually a cover or a plate page.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = surface.muted,
                        textAlign = TextAlign.Center,
                    )
                }
                prefs.layout == "scroll" -> ScrolledText(
                    state = state, items = items, surface = surface, base = base, baseUrl = baseUrl,
                    next = next, onTurn = onTurn, onGoToChapter = onGoToChapter, onLanded = onLanded,
                )
                else -> PagedText(
                    state = state, items = items, surface = surface, base = base, baseUrl = baseUrl,
                    pageWidth = maxWidth, pageHeight = maxHeight,
                    previous = previous, next = next,
                    onTurn = onTurn, onGoToChapter = onGoToChapter, onGoToChapterEnd = onGoToChapterEnd,
                    onLanded = onLanded,
                )
            }
        }

        ChapterSteps(state, surface, onPick = onGoToChapter)
    }
}

private val PAGE_GUTTER = 20.dp
private val PAGE_TOP = 12.dp
private val PAGE_BOTTOM = 16.dp

@Composable
private fun PagedText(
    state: ReaderState,
    items: List<PageItem>,
    surface: ReadingSurface,
    base: TextStyle,
    baseUrl: String,
    pageWidth: androidx.compose.ui.unit.Dp,
    pageHeight: androidx.compose.ui.unit.Dp,
    previous: com.gooseberrydevelopment.backhog.api.TextChapter?,
    next: com.gooseberrydevelopment.backhog.api.TextChapter?,
    onTurn: (Long) -> Unit,
    onGoToChapter: (com.gooseberrydevelopment.backhog.api.TextChapter) -> Unit,
    onGoToChapterEnd: (com.gooseberrydevelopment.backhog.api.TextChapter) -> Unit,
    onLanded: () -> Unit,
) {
    val density = LocalDensity.current
    val textMeasurer = rememberTextMeasurer()
    val em = with(density) { base.fontSize.toPx() }
    val columnPx = with(density) { (pageWidth - PAGE_GUTTER * 2).toPx() }
    val capacity = with(density) { (pageHeight - PAGE_TOP - PAGE_BOTTOM).toPx() }.toDouble()
    val imageHeight = capacity * 0.42

    // Measure every item for real, at the width it will draw at.
    val layouts: List<TextLayoutResult?> = remember(items, columnPx) {
        items.map { item ->
            (item as? PageItem.Text)?.let {
                val inset = (it.look.insetEm * em * 2).toInt()
                textMeasurer.measure(
                    it.look.text,
                    it.look.style,
                    constraints = Constraints(maxWidth = (columnPx.toInt() - inset).coerceAtLeast(1)),
                )
            }
        }
    }
    val pages = remember(layouts, capacity) {
        val measured = items.mapIndexed { index, item ->
            when (item) {
                is PageItem.Text -> {
                    val layout = layouts[index]!!
                    com.gooseberrydevelopment.backhog.books.LinePages.Item(
                        List(layout.lineCount) { (layout.getLineBottom(it) - layout.getLineTop(it)).toDouble() },
                        marginTop = item.look.marginTopEm * em.toDouble(),
                        marginBottom = item.look.marginBottomEm * em.toDouble(),
                    )
                }
                is PageItem.Image -> com.gooseberrydevelopment.backhog.books.LinePages.Item(imageHeight, em * 0.8, em * 0.8)
                is PageItem.Ornament -> com.gooseberrydevelopment.backhog.books.LinePages.Item(em * 3.0)
                is PageItem.Lead -> com.gooseberrydevelopment.backhog.books.LinePages.Item(em * 2.5)
            }
        }
        com.gooseberrydevelopment.backhog.books.LinePages.paginate(measured, capacity)
    }

    // A page reports the paragraph its first line belongs to.
    val pageOffsets = remember(pages, items) {
        pages.map { slices -> state.blocks[items[slices.first().item].blockIndex].offset }
    }
    // A page either side of the chapter, when there is a chapter there, so
    // swiping past the end of one turns into the next — as a book does.
    val lead = if (previous != null) 1 else 0
    val trail = if (next != null) 1 else 0
    val pagerState = rememberPagerState(initialPage = lead, pageCount = { pages.size + lead + trail })

    LaunchedEffect(pages, state.spine) {
        if (pages.isEmpty()) return@LaunchedEffect
        val target = state.pendingOffset ?: state.liveOffset
        val blockIndex = com.gooseberrydevelopment.backhog.books.blockIndexAt(state.blocks, target)
        val itemIndex = items.indexOfFirst { it is PageItem.Text && it.blockIndex == blockIndex }
            .let { if (it >= 0) it else items.indexOfFirst { item -> item.blockIndex >= blockIndex } }
            .let { if (it >= 0) it else items.lastIndex }
        pagerState.scrollToPage(lead + com.gooseberrydevelopment.backhog.books.LinePages.pageForItem(pages, itemIndex))
        onLanded()
    }

    LaunchedEffect(pagerState.settledPage, pages) {
        if (pages.isEmpty() || !state.armed) return@LaunchedEffect
        val page = pagerState.settledPage - lead
        when {
            page < 0 -> previous?.let(onGoToChapterEnd)
            page >= pages.size -> next?.let(onGoToChapter)
            else -> onTurn(pageOffsets[page])
        }
    }

    HorizontalPager(state = pagerState, modifier = Modifier.fillMaxSize(), beyondViewportPageCount = 1) { index ->
        val page = index - lead
        val slices = pages.getOrNull(page)
        if (slices == null) {
            ChapterTurn(surface, if (page < 0) previous else next, forward = page >= 0)
            return@HorizontalPager
        }
        Column(
            Modifier
                .fillMaxSize()
                .padding(horizontal = PAGE_GUTTER)
                .padding(top = PAGE_TOP, bottom = PAGE_BOTTOM),
        ) {
            for (slice in slices) {
                val top = with(density) { slice.marginTop.toFloat().toDp() }
                val bottom = with(density) { slice.marginBottom.toFloat().toDp() }
                when (val item = items[slice.item]) {
                    is PageItem.Text -> {
                        val layout = layouts[slice.item]!!
                        val from = layout.getLineStart(slice.lineStart)
                        val to = layout.getLineEnd(slice.lineEnd - 1)
                        // A continuation is not a new paragraph: no first-line indent.
                        val style = if (slice.lineStart > 0) item.look.style.copy(textIndent = null) else item.look.style
                        Text(
                            text = item.look.text.subSequence(from, to),
                            style = style,
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = top, bottom = bottom)
                                .padding(horizontal = with(density) { (item.look.insetEm * em).toDp() }),
                        )
                    }
                    is PageItem.Image -> AsyncImage(
                        model = assetUrl(baseUrl, state.entryId, item.href),
                        contentDescription = item.alt,
                        contentScale = ContentScale.Fit,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = top, bottom = bottom)
                            .height(with(density) { imageHeight.toFloat().toDp() }),
                    )
                    is PageItem.Ornament -> Ornament(surface, item.rule, with(density) { (em * 3).toDp() })
                    is PageItem.Lead -> Spacer(Modifier.height(with(density) { (em * 2.5f).toDp() }))
                }
            }
        }
    }
}

/** The scrolling layout: the same looks, one continuous column per chapter. */
@Composable
private fun ScrolledText(
    state: ReaderState,
    items: List<PageItem>,
    surface: ReadingSurface,
    base: TextStyle,
    baseUrl: String,
    next: com.gooseberrydevelopment.backhog.api.TextChapter?,
    onTurn: (Long) -> Unit,
    onGoToChapter: (com.gooseberrydevelopment.backhog.api.TextChapter) -> Unit,
    onLanded: () -> Unit,
) {
    val density = LocalDensity.current
    val em = with(density) { base.fontSize.toPx() }
    val listState = rememberLazyListState()

    LaunchedEffect(items, state.spine) {
        if (items.isEmpty()) return@LaunchedEffect
        val target = state.pendingOffset ?: state.liveOffset
        val blockIndex = com.gooseberrydevelopment.backhog.books.blockIndexAt(state.blocks, target)
        val itemIndex = items.indexOfFirst { it.blockIndex >= blockIndex }.coerceAtLeast(0)
        listState.scrollToItem(itemIndex)
        onLanded()
    }
    // The block at the top of the screen is where you are, once scrolling
    // rests. The collector outlives recompositions, so it reads the live
    // state, not the one it started with.
    val current by androidx.compose.runtime.rememberUpdatedState(state)
    val report by androidx.compose.runtime.rememberUpdatedState(onTurn)
    LaunchedEffect(listState, items) {
        androidx.compose.runtime.snapshotFlow { listState.isScrollInProgress to listState.firstVisibleItemIndex }
            .collect { (scrolling, first) ->
                if (!scrolling && current.armed) {
                    items.getOrNull(first)?.let { report(current.blocks[it.blockIndex].offset) }
                }
            }
    }

    LazyColumn(
        state = listState,
        modifier = Modifier.fillMaxSize(),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = PAGE_GUTTER, vertical = PAGE_TOP),
    ) {
        items(items.size) { index ->
            when (val item = items[index]) {
                is PageItem.Text -> Text(
                    text = item.look.text,
                    style = item.look.style,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(
                            top = with(density) { (item.look.marginTopEm * em).toDp() },
                            bottom = with(density) { (item.look.marginBottomEm * em).toDp() },
                        )
                        .padding(horizontal = with(density) { (item.look.insetEm * em).toDp() }),
                )
                is PageItem.Image -> AsyncImage(
                    model = assetUrl(baseUrl, state.entryId, item.href),
                    contentDescription = item.alt,
                    contentScale = ContentScale.FillWidth,
                    modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
                )
                is PageItem.Ornament -> Ornament(surface, item.rule, with(density) { (em * 3).toDp() })
                is PageItem.Lead -> Spacer(Modifier.height(with(density) { (em * 2.5f).toDp() }))
            }
        }
        if (next != null) {
            item {
                OutlinedButton(
                    onClick = { onGoToChapter(next) },
                    modifier = Modifier.fillMaxWidth().padding(top = 32.dp, bottom = 48.dp),
                ) { Text("Next: ${chapterTitle(next)}", color = surface.fg, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            }
        }
    }
}

/** A scene break (⁂) or the short rule that closes a chapter's opener. */
@Composable
private fun Ornament(surface: ReadingSurface, rule: Boolean, height: androidx.compose.ui.unit.Dp) {
    Box(Modifier.fillMaxWidth().height(height), contentAlignment = Alignment.Center) {
        if (rule) {
            Box(Modifier.width(56.dp).height(1.dp).background(surface.rule))
        } else {
            Text("⁂", color = surface.muted, style = MaterialTheme.typography.bodyLarge)
        }
    }
}

/** The page past a chapter's edge, shown for the moment the next one loads. */
@Composable
private fun ChapterTurn(surface: ReadingSurface, chapter: com.gooseberrydevelopment.backhog.api.TextChapter?, forward: Boolean) {
    Column(
        Modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            if (forward) "Next" else "Previous",
            style = MaterialTheme.typography.labelMedium,
            color = surface.muted,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            chapter?.let { chapterTitle(it) } ?: "",
            style = MaterialTheme.typography.titleLarge,
            color = surface.fg,
            textAlign = TextAlign.Center,
        )
    }
}

@Composable
private fun ChapterSteps(
    state: ReaderState,
    surface: ReadingSurface,
    onPick: (com.gooseberrydevelopment.backhog.api.TextChapter) -> Unit,
) {
    val toc = state.toc
    val position = toc.indexOfFirst { it.spineIndex == state.spine }
    val previous = if (position > 0) toc[position - 1] else null
    val next = if (position in 0..(toc.size - 2)) toc[position + 1] else null
    Row(
        Modifier
            .fillMaxWidth()
            .background(surface.bg)
            .navigationBarsPadding()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (previous != null) {
            TextButton(onClick = { onPick(previous) }) {
                Text(chapterTitle(previous), color = surface.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.width(120.dp))
            }
        } else {
            Spacer(Modifier.width(1.dp))
        }
        if (next != null) {
            TextButton(onClick = { onPick(next) }) {
                Text(chapterTitle(next), color = surface.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.width(120.dp))
            }
        }
    }
}

@Composable
private fun PeekBanner(
    surface: ReadingSurface,
    state: ReaderState,
    onBackToPlace: () -> Unit,
    onReadFromHere: () -> Unit,
) {
    val storedPercent = state.position?.percent?.roundToInt()
    Column(
        Modifier
            .fillMaxWidth()
            .background(surface.rule.copy(alpha = 0.35f))
            .padding(horizontal = 16.dp, vertical = 6.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(
            "Viewing a search result · your place is ${storedPercent ?: 0}%",
            style = MaterialTheme.typography.labelSmall,
            color = surface.muted,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                "Back to my place",
                style = MaterialTheme.typography.labelMedium,
                color = surface.fg,
                modifier = Modifier
                    .clip(MaterialTheme.shapes.small)
                    .background(surface.rule)
                    .clickable { onBackToPlace() }
                    .padding(horizontal = 10.dp, vertical = 4.dp),
            )
            Text(
                "Read from here",
                style = MaterialTheme.typography.labelMedium,
                color = surface.fg,
                modifier = Modifier
                    .clip(MaterialTheme.shapes.small)
                    .clickable { onReadFromHere() }
                    .padding(horizontal = 10.dp, vertical = 4.dp),
            )
        }
    }
}

/* --------------------------------------------------------- paged reader */

@Composable
private fun PagedReader(
    state: ReaderState,
    surface: ReadingSurface,
    baseUrl: String,
    onBack: () -> Unit,
    onSearch: () -> Unit,
    onTurn: (Int) -> Unit,
    onBackToPlace: () -> Unit,
    onReadFromHere: () -> Unit,
    onLanded: () -> Unit,
) {
    val pages = state.pages ?: return
    val pageCount = pages.pageCount.coerceAtLeast(1)
    val pagerState = rememberPagerState(initialPage = state.pendingPage ?: 0, pageCount = { pageCount })

    LaunchedEffect(state.pendingPage) {
        val target = state.pendingPage ?: return@LaunchedEffect
        if (target != pagerState.currentPage) pagerState.scrollToPage(target)
        onLanded()
    }
    LaunchedEffect(pagerState.settledPage) {
        onTurn(pagerState.settledPage)
    }

    Column(Modifier.fillMaxSize()) {
        ReaderChrome(
            surface = surface,
            title = "page ${(pagerState.currentPage + 1)} of $pageCount",
            progress = if (pageCount > 1) pagerState.currentPage.toDouble() / (pageCount - 1) * 100.0 else 0.0,
            progressLabel = null,
            onBack = onBack,
            actions = {
                IconButton(onClick = onSearch) {
                    Icon(Icons.Filled.Search, contentDescription = "Search lettering", tint = surface.muted)
                }
            },
            peekBanner = if (state.peek) {
                {
                    PeekBanner(
                        surface = surface,
                        state = state,
                        onBackToPlace = onBackToPlace,
                        onReadFromHere = onReadFromHere,
                    )
                }
            } else null,
        )
        HorizontalPager(state = pagerState, modifier = Modifier.weight(1f), beyondViewportPageCount = 1) { page ->
            val info = pages.pages.getOrNull(page)
            when {
                info == null -> {}
                !info.hasImage -> PageRefusal(surface, "This page is vector art or blank — the reader can only show pages that carry an image.")
                else -> Box(
                    Modifier.fillMaxSize().padding(8.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    AsyncImage(
                        model = pageImageUrl(baseUrl, state.entryId, page),
                        contentDescription = "Page ${page + 1} of $pageCount",
                        contentScale = ContentScale.Fit,
                        modifier = Modifier
                            .fillMaxSize()
                            .then(
                                if (info.width > 0 && info.height > 0) {
                                    Modifier.aspectRatio(info.width.toFloat() / info.height.toFloat())
                                } else Modifier
                            ),
                    )
                }
            }
        }
    }
}

/** A labeled refusal where a page should be — honesty over a blank. */
@Composable
private fun PageRefusal(surface: ReadingSurface, label: String) {
    Box(Modifier.fillMaxSize().padding(32.dp), contentAlignment = Alignment.Center) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = surface.muted, textAlign = TextAlign.Center)
    }
}

/* ------------------------------------------------------------ contents */

@Composable
private fun ContentsDrawer(
    state: ReaderState,
    surface: ReadingSurface,
    onPick: (com.gooseberrydevelopment.backhog.api.TextChapter) -> Unit,
) {
    val chapters = state.toc
    val listState = rememberLazyListState()
    val inferred = chapters.count { isInferredTitle(it) }
    val note = describeToc(state.chapters?.toc, inferred)

    Column(Modifier.padding(bottom = 32.dp)) {
        Text(
            "Contents",
            style = MaterialTheme.typography.titleMedium,
            color = surface.fg,
            modifier = Modifier.padding(horizontal = 20.dp).padding(bottom = 8.dp),
        )
        if (chapters.isEmpty()) {
            Text(
                "This book's spine has no readable sections.",
                style = MaterialTheme.typography.bodyMedium,
                color = surface.muted,
                modifier = Modifier.padding(horizontal = 20.dp),
            )
        } else {
            note?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.labelSmall,
                    color = surface.muted,
                    modifier = Modifier.padding(horizontal = 20.dp).padding(bottom = 8.dp),
                )
            }
            LazyColumn(state = listState, modifier = Modifier.height(420.dp)) {
                items(chapters, key = { it.spineIndex }) { chapter ->
                    val current = chapter.spineIndex == state.spine
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable { onPick(chapter) }
                            .padding(
                                start = (20 + min(chapter.depth, 4) * 14).dp,
                                end = 20.dp,
                                top = 8.dp,
                                bottom = 8.dp,
                            ),
                    ) {
                        Text(
                            chapterTitle(chapter),
                            style = MaterialTheme.typography.bodyMedium,
                            color = if (current) surface.fg else surface.muted,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (isInferredTitle(chapter)) {
                            Spacer(Modifier.width(6.dp))
                            Text(
                                "inferred",
                                style = MaterialTheme.typography.labelSmall,
                                color = surface.muted.copy(alpha = 0.6f),
                            )
                        }
                    }
                }
            }
        }
    }
}

/* --------------------------------------------------------- type controls */

@Composable
private fun TypeControls(container: AppContainer, prefs: ReaderPrefs, surface: ReadingSurface) {
    val scope = rememberCoroutineScope()
    Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 40.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        Text("Reading settings", style = MaterialTheme.typography.titleMedium, color = surface.fg)
        Stepper("Text size", "${prefs.fontSize}sp", surface,
            onDown = { scope.launch { container.preferences.setReaderPrefs(prefs.copy(fontSize = (prefs.fontSize - 1).coerceIn(14, 28))) } },
            onUp = { scope.launch { container.preferences.setReaderPrefs(prefs.copy(fontSize = (prefs.fontSize + 1).coerceIn(14, 28))) } })
        Stepper("Line height", String.format(java.util.Locale.US, "%.2f", prefs.lineHeight), surface,
            onDown = { scope.launch { container.preferences.setReaderPrefs(prefs.copy(lineHeight = (prefs.lineHeight - 0.05f).coerceIn(1.3f, 2.2f))) } },
            onUp = { scope.launch { container.preferences.setReaderPrefs(prefs.copy(lineHeight = (prefs.lineHeight + 0.05f).coerceIn(1.3f, 2.2f))) } })
        Choice("Face", surface, listOf("serif" to "Serif", "sans" to "Sans"), prefs.face) { value ->
            scope.launch { container.preferences.setReaderPrefs(prefs.copy(face = value)) }
        }
        Choice(
            "Paper", surface,
            listOf("auto" to "Auto", "dark" to ReadingSurface.Night.label, "sepia" to ReadingSurface.Paper.label, "light" to ReadingSurface.Day.label),
            prefs.surface,
        ) { value ->
            scope.launch { container.preferences.setReaderPrefs(prefs.copy(surface = value)) }
        }
        Choice("Layout", surface, listOf("pages" to "Pages", "scroll" to "Scroll"), prefs.layout) { value ->
            scope.launch { container.preferences.setReaderPrefs(prefs.copy(layout = value)) }
        }
        Choice("Paragraphs", surface, listOf("indent" to "Indented", "spaced" to "Spaced"), prefs.paragraphs) { value ->
            scope.launch { container.preferences.setReaderPrefs(prefs.copy(paragraphs = value)) }
        }
        Choice("Alignment", surface, listOf("left" to "Ragged", "justify" to "Justified"), if (prefs.justify) "justify" else "left") { value ->
            scope.launch { container.preferences.setReaderPrefs(prefs.copy(justify = value == "justify")) }
        }
        Choice("Chapter openings", surface, listOf("on" to "Small caps", "off" to "Plain"), if (prefs.opening) "on" else "off") { value ->
            scope.launch { container.preferences.setReaderPrefs(prefs.copy(opening = value == "on")) }
        }
    }
}

@Composable
private fun Stepper(label: String, value: String, surface: ReadingSurface, onDown: () -> Unit, onUp: () -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = surface.muted)
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
            OutlinedButton(onClick = onDown) { Text("−", color = surface.fg) }
            Text(value, style = MaterialTheme.typography.labelMedium, color = surface.fg)
            OutlinedButton(onClick = onUp) { Text("+", color = surface.fg) }
        }
    }
}

@Composable
private fun Choice(label: String, surface: ReadingSurface, options: List<Pair<String, String>>, value: String, onChange: (String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = surface.muted)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            options.forEach { (key, text) ->
                val selected = key == value
                Text(
                    text,
                    style = MaterialTheme.typography.labelMedium,
                    color = if (selected) surface.fg else surface.muted,
                    modifier = Modifier
                        .clip(MaterialTheme.shapes.small)
                        .background(if (selected) surface.rule else Color.Transparent)
                        .clickable { onChange(key) }
                        .padding(horizontal = 12.dp, vertical = 6.dp),
                )
            }
        }
    }
}

/* ------------------------------------------------------------- problems */

@Composable
private fun ReaderProblem(error: com.gooseberrydevelopment.backhog.api.ApiError?, surface: ReadingSurface, onBack: () -> Unit) {
    Column(
        Modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        val title = when (error?.status) {
            404 -> "No ebook attached"
            422 -> "This book is DRM-protected"
            else -> "Couldn't open this book"
        }
        val body = when (error?.status) {
            404 -> "This book has no EPUB attached yet, so there is nothing to read here. Attach one from Book files and the reader opens."
            422 -> "${error.message} Backhog does not break DRM; a DRM-free copy of the same book will open here without any further setup."
            else -> error?.message ?: "Something went wrong."
        }
        Text(title, style = MaterialTheme.typography.titleLarge, color = surface.fg, textAlign = TextAlign.Center)
        Spacer(Modifier.height(12.dp))
        Text(body, style = MaterialTheme.typography.bodyMedium, color = surface.muted, textAlign = TextAlign.Center)
        Spacer(Modifier.height(20.dp))
        OutlinedButton(onClick = onBack) { Text("Back to the book", color = surface.fg) }
    }
}

/* ---------------------------------------------------------------- urls */

/** One illustration out of a book's EPUB — same-origin, cookie-authenticated. */
internal fun assetUrl(baseUrl: String, entryId: String, href: String): String =
    "${ServerUrl.apiRoot(baseUrl)}books/$entryId/text/asset?href=${java.net.URLEncoder.encode(href, "UTF-8")}"

/** One page of a paged book, served from our own API. */
internal fun pageImageUrl(baseUrl: String, entryId: String, page: Int): String =
    "${ServerUrl.apiRoot(baseUrl)}books/$entryId/pages/$page"
