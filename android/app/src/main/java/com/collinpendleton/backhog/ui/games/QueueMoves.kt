package com.collinpendleton.backhog.ui.games

import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.ReorderRequest

/**
 * The queue's order math, pulled out of the UI so it can be tested like the
 * web's `applyMove`. A move is applied optimistically to the local list; the
 * server is then told the moved entry's new neighbours, where an empty
 * before/after id means "no neighbour on that side" (top/bottom).
 */
object QueueMoves {
    /** The reordered list after moving `from` to `to`; bounds-checked, identity for no-ops. */
    fun moved(entries: List<Entry>, from: Int, to: Int): List<Entry> {
        if (from !in entries.indices || to !in entries.indices || from == to) return entries
        return entries.toMutableList().apply { add(to, removeAt(from)) }
    }

    /**
     * The reorder request that persists `entryId`'s position against the
     * neighbours it has in `entries` (which must already reflect the move).
     * Null when the entry isn't in the list — nothing to persist.
     */
    fun requestFor(entries: List<Entry>, entryId: String): ReorderRequest? {
        val index = entries.indexOfFirst { it.id == entryId }
        if (index < 0) return null
        return ReorderRequest(
            entryId = entryId,
            beforeId = entries.getOrNull(index - 1)?.id ?: "",
            afterId = entries.getOrNull(index + 1)?.id ?: "",
        )
    }

    /** Where a quick-move button sends an entry: top/up/down/bottom. */
    enum class Kind { Top, Up, Down, Bottom }

    fun targetIndex(index: Int, size: Int, kind: Kind): Int = when (kind) {
        Kind.Top -> 0
        Kind.Bottom -> size - 1
        Kind.Up -> index - 1
        Kind.Down -> index + 1
    }.coerceIn(0, size - 1)
}
