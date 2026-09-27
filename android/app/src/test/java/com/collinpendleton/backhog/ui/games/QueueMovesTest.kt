package com.collinpendleton.backhog.ui.games

import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.Game
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class QueueMovesTest {
    private fun entry(id: String, beatSeconds: Long? = null) = Entry(
        id = id,
        createdAt = "2026-01-01T00:00:00Z",
        updatedAt = "2026-01-01T00:00:00Z",
        game = Game(id = id.hashCode().toLong(), name = "Game $id", timeToBeatMain = beatSeconds),
    )

    private val queue = listOf(entry("a"), entry("b"), entry("c"), entry("d"))

    @Test fun `moving reorders and bounds-check`() {
        assertEquals(listOf("b", "a", "c", "d"), QueueMoves.moved(queue, 0, 1).map { it.id })
        assertEquals(listOf("d", "a", "b", "c"), QueueMoves.moved(queue, 3, 0).map { it.id })
        assertEquals(queue, QueueMoves.moved(queue, 1, 1))
        assertEquals(queue, QueueMoves.moved(queue, -1, 2))
        assertEquals(queue, QueueMoves.moved(queue, 2, 9))
    }

    @Test fun `the request names the new neighbours`() {
        val moved = QueueMoves.moved(queue, 2, 0) // c to top
        val request = QueueMoves.requestFor(moved, "c")!!
        assertEquals("c", request.entryId)
        assertEquals("", request.beforeId) // no neighbour above: top
        assertEquals("a", request.afterId)
    }

    @Test fun `bottom means no neighbour below`() {
        val moved = QueueMoves.moved(queue, 0, 3) // a to bottom
        val request = QueueMoves.requestFor(moved, "a")!!
        assertEquals("d", request.beforeId)
        assertEquals("", request.afterId)
    }

    @Test fun `a middle move pins both sides`() {
        val moved = QueueMoves.moved(queue, 0, 2) // a lands between c and d
        assertEquals(listOf("b", "c", "a", "d"), moved.map { it.id })
        val request = QueueMoves.requestFor(moved, "a")!!
        assertEquals("c", request.beforeId)
        assertEquals("d", request.afterId)
    }

    @Test fun `an unknown entry has nothing to persist`() {
        assertNull(QueueMoves.requestFor(queue, "zz"))
    }

    @Test fun `quick-move targets map and clamp`() {
        val size = queue.size
        assertEquals(0, QueueMoves.targetIndex(2, size, QueueMoves.Kind.Top))
        assertEquals(3, QueueMoves.targetIndex(2, size, QueueMoves.Kind.Bottom))
        assertEquals(1, QueueMoves.targetIndex(2, size, QueueMoves.Kind.Up))
        assertEquals(3, QueueMoves.targetIndex(2, size, QueueMoves.Kind.Down))
        assertEquals(0, QueueMoves.targetIndex(0, size, QueueMoves.Kind.Up)) // clamped
    }

    @Test fun `hours come from time-to-beat and books contribute nothing`() {
        val gameEntry = entry("g", beatSeconds = 7200) // 2h
        assertEquals(2.0, gameEntry.hours, 0.001)
        val bookEntry = Entry(
            id = "bk",
            mediaType = "book",
            book = com.collinpendleton.backhog.api.BookBrief(id = "OL1W", title = "A Book"),
            createdAt = "2026-01-01T00:00:00Z",
            updatedAt = "2026-01-01T00:00:00Z",
        )
        assertEquals(0.0, bookEntry.hours, 0.001)
        assertEquals("A Book", bookEntry.title)
    }
}
