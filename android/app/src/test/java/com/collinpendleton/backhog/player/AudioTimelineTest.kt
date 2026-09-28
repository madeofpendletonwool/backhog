package com.collinpendleton.backhog.player

import com.collinpendleton.backhog.api.AudioTimeline
import com.collinpendleton.backhog.api.AudioTrack
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * The timeline arithmetic, tested against the web's semantics
 * (web/src/hooks/useAudioPlayer.tsx): boundaries, unmeasured guards, the
 * last-track clamp, and the write payload's measured clamp.
 */
class AudioTimelineTest {

    private fun track(
        id: Long,
        start: Double,
        duration: Double,
        measured: Boolean = true,
        missing: Boolean = false,
        size: Long = 0,
    ) = AudioTrack(
        id = id,
        trackNumber = id.toInt(),
        title = "Track $id",
        sizeBytes = size,
        durationSeconds = duration,
        globalStart = start,
        measured = measured,
        missing = missing,
    )

    private val simple = AudioTimeline(
        tracks = listOf(track(1, 0.0, 100.0), track(2, 100.0, 200.0)),
        totalDuration = 300.0,
    )

    // --- trackAt: the faithful port -------------------------------------

    @Test
    fun `a second inside the first track belongs to the first track`() {
        assertEquals(0, trackAt(simple, 0.0))
        assertEquals(0, trackAt(simple, 99.9))
    }

    @Test
    fun `a boundary second belongs to the track that is starting`() {
        // [start, start + duration): 100.0 is track 2's first second.
        assertEquals(1, trackAt(simple, 100.0))
        assertEquals(1, trackAt(simple, 299.9))
    }

    @Test
    fun `past the end clamps to the last track that holds time`() {
        assertEquals(1, trackAt(simple, 300.0))
        assertEquals(1, trackAt(simple, 10_000.0))
    }

    @Test
    fun `an unmeasured track owns no time`() {
        val timeline = AudioTimeline(
            tracks = listOf(
                track(1, 0.0, 100.0),
                track(2, 100.0, 0.0, measured = false),
                track(3, 100.0, 50.0),
            ),
            totalDuration = 150.0,
        )
        // The unmeasured middle holds its place but no seconds: 100 is still
        // the next measured track's.
        assertEquals(0, trackAt(timeline, 99.9))
        assertEquals(2, trackAt(timeline, 100.0))
        assertEquals(2, trackAt(timeline, 149.9))
        assertEquals(2, trackAt(timeline, 150.0))
    }

    @Test
    fun `a timeline where nothing could be measured answers the first file`() {
        val timeline = AudioTimeline(
            tracks = listOf(track(1, 0.0, 0.0, measured = false), track(2, 0.0, 0.0, measured = false)),
            totalDuration = 0.0,
        )
        assertEquals(0, trackAt(timeline, 0.0))
    }

    @Test
    fun `an empty timeline answers -1`() {
        assertEquals(-1, trackAt(AudioTimeline(), 0.0))
    }

    @Test
    fun `zero and negative seconds belong to the first track`() {
        assertEquals(0, trackAt(simple, -5.0))
    }

    // --- BookTape: the window the engine plays ---------------------------

    @Test
    fun `a global second maps through missing tracks into the window`() {
        val timeline = AudioTimeline(
            tracks = listOf(
                track(1, 0.0, 100.0),
                track(2, 100.0, 50.0, missing = true), // on the server, gone from the NAS
                track(3, 150.0, 200.0),
            ),
            totalDuration = 350.0,
        )
        val tape = BookTape(timeline)
        assertEquals(listOf(1L, 3L), tape.present.map { it.id })
        // Track 3 starts at the server's 150s, but the window is 100s wide
        // when it begins — the missing 50s is subtracted on the way past it.
        assertEquals(100_000L, tape.windowPositionMs(150.0))
        assertEquals(0L, tape.windowPositionMs(0.0))
        assertEquals(50_000L, tape.windowPositionMs(50.0))
        // Deep in track 3: server 250s = window 200s.
        assertEquals(200_000L, tape.windowPositionMs(250.0))
        // Past the end clamps into the last present track.
        assertEquals(300_000L, tape.windowPositionMs(9_999.0))
    }

    @Test
    fun `the window spans the present tracks' measured lengths`() {
        val timeline = AudioTimeline(
            tracks = listOf(track(1, 0.0, 100.0), track(2, 100.0, 200.0, missing = true), track(3, 300.0, 60.0)),
            totalDuration = 360.0,
        )
        assertEquals(160_000L, BookTape(timeline).windowDurationMs)
    }

    @Test
    fun `an unmeasured track gets a size-based guess until the engine corrects it`() {
        val timeline = AudioTimeline(
            tracks = listOf(track(1, 0.0, 0.0, measured = false, size = 960_000)),
            totalDuration = 0.0,
            degraded = true,
        )
        val tape = BookTape(timeline)
        // 960 KB at ~96 kbps is 80 seconds of guessing room.
        assertEquals(80_000L, tape.windowDurationMs)
    }

    @Test
    fun `the clock reads the server's arithmetic, not the window's`() {
        val timeline = AudioTimeline(
            tracks = listOf(track(1, 0.0, 100.0), track(2, 100.0, 50.0, missing = true), track(3, 150.0, 200.0)),
            totalDuration = 350.0,
        )
        val tape = BookTape(timeline)
        // 30s into track 3 (window index 1): the server says 150 + 30, the
        // web's `track.global_start + currentTime` exactly.
        assertEquals(180.0, tape.serverGlobalSeconds(1, 30.0), 0.001)
    }

    // --- the write payload ------------------------------------------------

    @Test
    fun `a write carries the file and its in-track second`() {
        val tape = BookTape(simple)
        val write = tape.writeFor(trackIndex = 1, offsetSeconds = 25.5)!!
        assertEquals(2L, write.audioFileId)
        assertEquals(25.5, write.audioSeconds!!, 0.001)
        // The server holds "listen" as the audio write's source; sending a
        // client-invented one is a 400 the web never risks.
        assertNull(write.source)
        assertNull(write.charOffset)
    }

    @Test
    fun `a measured track clamps its write to the measured length`() {
        val tape = BookTape(simple)
        val write = tape.writeFor(trackIndex = 0, offsetSeconds = 120.0)!!
        assertEquals(100.0, write.audioSeconds!!, 0.001)
        assertEquals(0.0, tape.writeFor(0, -5.0)!!.audioSeconds!!, 0.001)
    }

    @Test
    fun `an unmeasured track passes its second through, like the web`() {
        val timeline = AudioTimeline(
            tracks = listOf(track(1, 0.0, 0.0, measured = false)),
            totalDuration = 0.0,
        )
        val write = BookTape(timeline).writeFor(0, 1234.5)!!
        assertEquals(1234.5, write.audioSeconds!!, 0.001)
    }

    @Test
    fun `a write off the tape answers null`() {
        assertNull(BookTape(simple).writeFor(trackIndex = 9, offsetSeconds = 0.0))
        assertNull(BookTape(AudioTimeline()).writeFor(0, 0.0))
    }

    // --- the period walk --------------------------------------------------

    @Test
    fun `the period walk finds the owning track and its offset`() {
        val durations = longArrayOf(100_000, 200_000)
        assertEquals(0 to 0.0, trackAndOffset(durations, 0L))
        assertEquals(0 to 99.999, trackAndOffset(durations, 99_999L))
        assertEquals(1 to 0.0, trackAndOffset(durations, 100_000L))
        assertEquals(1 to 42.5, trackAndOffset(durations, 142_500L))
    }

    @Test
    fun `a walk past the end holds at the last second of the last track`() {
        val durations = longArrayOf(100_000, 200_000)
        assertEquals(1 to 200.0, trackAndOffset(durations, 999_999L))
    }

    @Test
    fun `an empty tape answers null`() {
        assertNull(trackAndOffset(longArrayOf(), 0L))
    }

    // --- the ladder -------------------------------------------------------

    @Test
    fun `a rate off the ladder clamps to the web's band`() {
        assertEquals(0.75f, clampRate(0.5f))
        assertEquals(3f, clampRate(4f))
        assertEquals(1.75f, clampRate(1.75f))
    }
}
