package com.collinpendleton.backhog.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
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

    private companion object {
        val BASE_URL = stringPreferencesKey("base_url")
        val THEME_LINKED = booleanPreferencesKey("theme:linked")
        val ARENA = stringPreferencesKey("arena")
        val LIBRARY_STATUS = stringPreferencesKey("library:games:status")
        val LIBRARY_SORT = stringPreferencesKey("library:games:sort")
        val LIBRARY_PLATFORM = stringPreferencesKey("library:games:platform")
        val LIBRARY_GENRE = stringPreferencesKey("library:games:genre")
        val LIBRARY_VIEW = stringPreferencesKey("library:games:view")

        fun themeKey(arena: Arena) = stringPreferencesKey("theme:${arena.key}")
    }
}
