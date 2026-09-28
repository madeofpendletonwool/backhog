package com.collinpendleton.backhog.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.floatPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.collinpendleton.backhog.api.EntryStatus
import com.collinpendleton.backhog.ui.theme.ThemeId
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

/** The two halves of the app. Each keeps its own theme and its own back stack. */
enum class Arena(val key: String, val label: String) {
    Games("games", "Games"),
    Books("books", "Books");

    companion object {
        fun fromKey(key: String?): Arena? = entries.firstOrNull { it.key == key }
    }
}

/**
 * The web's per-arena theme semantics: one slot per arena, and `linked`
 * (on by default) collapsing them to one choice — setting a theme while
 * linked writes both slots.
 */
data class ThemeSettings(
    val games: ThemeId = ThemeId.Default,
    val books: ThemeId = ThemeId.Default,
    val linked: Boolean = true,
) {
    fun forArena(arena: Arena): ThemeId = when (arena) {
        Arena.Games -> games
        Arena.Books -> books
    }
}

/** The shelf's remembered face: filters, sort and grid/table, like the web's localStorage. */
data class BookShelfState(
    val status: String = "",
    val sort: String = "title",
    val author: String = "",
    val subject: String = "",
    val language: String = "",
    val grid: Boolean = true,
)

/**
 * The reader's type controls, the web's `backhog:reader` shape: body size in
 * sp, line height as a multiplier, the face, and which paper the column is —
 * "auto" takes whichever surface the app's own theme implies.
 */
data class ReaderPrefs(
    val fontSize: Int = 19,
    val lineHeight: Float = 1.65f,
    /** "serif" or "sans". */
    val face: String = "serif",
    /** "auto" | "dark" | "sepia" | "light". */
    val surface: String = "auto",
)

/**
 * The library state the web persists in localStorage
 * (`backhog:library:{status,sort,platform,genre,view}`), so the library looks
 * the same on every visit. The search box and the filters-panel toggle stay
 * transient, exactly like the web.
 */
data class LibraryFilters(
    val status: EntryStatus? = null,
    /** The web's default sort is Title A–Z. */
    val sort: String = "name",
    val platformId: Long? = null,
    val genreId: Long? = null,
    val view: LibraryLayout = LibraryLayout.Grid,
)

enum class LibraryLayout(val key: String) {
    Grid("grid"),
    Table("table");

    companion object {
        fun fromKey(key: String?): LibraryLayout? = entries.firstOrNull { it.key == key }
    }
}

/** The sort options, exactly the web's list and order (web/src/pages/LibraryPage.tsx). */
object LibrarySorts {
    val all: List<Pair<String, String>> = listOf(
        "added" to "Recently added",
        "name" to "Title A–Z",
        "released" to "Newest release",
        "rating" to "Highest rated",
        "shortest" to "Shortest first",
        "longest" to "Longest first",
        "updated" to "Recently updated",
    )

    fun label(key: String): String = all.firstOrNull { it.first == key }?.second ?: key
}

private val Context.dataStore: DataStore<Preferences> by preferencesDataStore(name = "backhog")

/** Everything the device remembers. The server owns all data; this is only where to find it and how it looks. */
class AppPreferences(private val store: DataStore<Preferences>) {
    constructor(context: Context) : this(context.dataStore)

    val baseUrl: Flow<String?> = store.data.map { it[BASE_URL] }

    suspend fun currentBaseUrl(): String? = baseUrl.first()

    suspend fun setBaseUrl(url: String?) {
        store.edit { if (url == null) it.remove(BASE_URL) else it[BASE_URL] = url }
    }

    val themes: Flow<ThemeSettings> = store.data.map { prefs ->
        ThemeSettings(
            games = ThemeId.fromKey(prefs[themeKey(Arena.Games)]) ?: ThemeId.Default,
            books = ThemeId.fromKey(prefs[themeKey(Arena.Books)]) ?: ThemeId.Default,
            // Absent means "has never chosen", which is linked.
            linked = prefs[THEME_LINKED] ?: true,
        )
    }

    suspend fun setTheme(theme: ThemeId, arena: Arena) {
        store.edit { prefs ->
            val linked = prefs[THEME_LINKED] ?: true
            val targets = if (linked) Arena.entries else listOf(arena)
            targets.forEach { prefs[themeKey(it)] = theme.key }
        }
    }

    /** Linking copies `from`'s theme across, so the app looks the same the moment the switch flips. */
    suspend fun setLinked(linked: Boolean, from: Arena) {
        store.edit { prefs ->
            prefs[THEME_LINKED] = linked
            if (linked) {
                val theme = prefs[themeKey(from)] ?: ThemeId.Default.key
                Arena.entries.forEach { prefs[themeKey(it)] = theme }
            }
        }
    }

    val arena: Flow<Arena> = store.data.map { Arena.fromKey(it[ARENA]) ?: Arena.Games }

    suspend fun setArena(arena: Arena) {
        store.edit { it[ARENA] = arena.key }
    }

    /** The books shelf remembers its filters between visits, the web's `backhog:books:*` keys. */
    val bookShelf: Flow<BookShelfState> = store.data.map { prefs ->
        BookShelfState(
            status = prefs[SHELF_STATUS] ?: "",
            sort = prefs[SHELF_SORT] ?: "title",
            author = prefs[SHELF_AUTHOR] ?: "",
            subject = prefs[SHELF_SUBJECT] ?: "",
            language = prefs[SHELF_LANGUAGE] ?: "",
            grid = prefs[SHELF_GRID] ?: true,
        )
    }

    suspend fun setBookShelf(state: BookShelfState) {
        store.edit {
            it[SHELF_STATUS] = state.status
            it[SHELF_SORT] = state.sort
            it[SHELF_AUTHOR] = state.author
            it[SHELF_SUBJECT] = state.subject
            it[SHELF_LANGUAGE] = state.language
            it[SHELF_GRID] = state.grid
        }
    }

    // --- the reader's type controls --------------------------------------

    val readerPrefs: Flow<ReaderPrefs> = store.data.map { prefs ->
        ReaderPrefs(
            fontSize = (prefs[READER_FONT] ?: 19f).toInt(),
            lineHeight = prefs[READER_LINE_HEIGHT] ?: 1.65f,
            face = prefs[READER_FACE] ?: "serif",
            surface = prefs[READER_SURFACE] ?: "auto",
        )
    }

    suspend fun setReaderPrefs(prefs: ReaderPrefs) {
        store.edit {
            it[READER_FONT] = prefs.fontSize.toFloat()
            it[READER_LINE_HEIGHT] = prefs.lineHeight
            it[READER_FACE] = prefs.face
            it[READER_SURFACE] = prefs.surface
        }
    }

    // --- the games library's remembered state ---------------------------

    val libraryFilters: Flow<LibraryFilters> = store.data.map { prefs ->
        LibraryFilters(
            status = EntryStatus.fromKey(prefs[LIBRARY_STATUS]),
            sort = prefs[LIBRARY_SORT] ?: "name",
            platformId = prefs[LIBRARY_PLATFORM]?.toLongOrNull(),
            genreId = prefs[LIBRARY_GENRE]?.toLongOrNull(),
            view = LibraryLayout.fromKey(prefs[LIBRARY_VIEW]) ?: LibraryLayout.Grid,
        )
    }

    suspend fun setLibraryFilters(filters: LibraryFilters) {
        store.edit { prefs ->
            prefs[LIBRARY_STATUS] = filters.status?.key ?: ""
            prefs[LIBRARY_SORT] = filters.sort
            filters.platformId?.let { prefs[LIBRARY_PLATFORM] = it.toString() } ?: prefs.remove(LIBRARY_PLATFORM)
            filters.genreId?.let { prefs[LIBRARY_GENRE] = it.toString() } ?: prefs.remove(LIBRARY_GENRE)
            prefs[LIBRARY_VIEW] = filters.view.key
        }
    }

    // --- the audiobook player ---------------------------------------------

    /**
     * Speed is a preference, not a per-book setting (the web's
     * `backhog:audio-rate`): whoever listens at 1.75x listens at 1.75x to the
     * next one too. Stored pre-clamped; the ladder is the UI's.
     */
    val audioRate: Flow<Float> = store.data.map { it[AUDIO_RATE] ?: 1f }

    suspend fun setAudioRate(rate: Float) {
        store.edit { it[AUDIO_RATE] = rate }
    }

    /**
     * The entry the tape last held, so a service restarted after process
     * death re-opens the book that was playing instead of an empty session.
     * Cleared when the listener closes the player.
     */
    val lastAudioEntry: Flow<String?> = store.data.map { it[LAST_AUDIO_ENTRY] }

    suspend fun setLastAudioEntry(entryId: String?) {
        store.edit {
            if (entryId == null) it.remove(LAST_AUDIO_ENTRY) else it[LAST_AUDIO_ENTRY] = entryId
        }
    }

    private companion object {
        val BASE_URL = stringPreferencesKey("base_url")
        val THEME_LINKED = booleanPreferencesKey("theme:linked")
        val ARENA = stringPreferencesKey("arena")
        val SHELF_STATUS = stringPreferencesKey("books:status")
        val SHELF_SORT = stringPreferencesKey("books:sort")
        val SHELF_AUTHOR = stringPreferencesKey("books:author")
        val SHELF_SUBJECT = stringPreferencesKey("books:subject")
        val SHELF_LANGUAGE = stringPreferencesKey("books:language")
        val SHELF_GRID = booleanPreferencesKey("books:grid")
        val READER_FONT = floatPreferencesKey("reader:font")
        val READER_LINE_HEIGHT = floatPreferencesKey("reader:lineHeight")
        val READER_FACE = stringPreferencesKey("reader:face")
        val READER_SURFACE = stringPreferencesKey("reader:surface")
        val LIBRARY_STATUS = stringPreferencesKey("library:games:status")
        val LIBRARY_SORT = stringPreferencesKey("library:games:sort")
        val LIBRARY_PLATFORM = stringPreferencesKey("library:games:platform")
        val LIBRARY_GENRE = stringPreferencesKey("library:games:genre")
        val LIBRARY_VIEW = stringPreferencesKey("library:games:view")
        val AUDIO_RATE = floatPreferencesKey("audio:rate")
        val LAST_AUDIO_ENTRY = stringPreferencesKey("audio:last-entry")

        fun themeKey(arena: Arena) = stringPreferencesKey("theme:${arena.key}")
    }
}
