package com.collinpendleton.backhog.player

import com.collinpendleton.backhog.api.AudioTimeline
import com.collinpendleton.backhog.api.AudioTrack
import com.collinpendleton.backhog.api.PositionWrite

/**
 * Backhog's audiobook timeline arithmetic, ported from
 * `web/src/hooks/useAudioPlayer.tsx` — the one source of truth for
 * global-time ↔ (track, offset) translation on Android, exactly as the web
 * hook is on the browser side.
 *
 * A book is N files that must behave like one tape. Every number that crosses
 * this module's edge — the seek bar, the timecodes, the OS scrubber, the
 * position write — is a second from 0 to the whole book's length, measured in
 * the *server's* arithmetic. Track boundaries exist here and nowhere else.
 */

/** Playback speeds, the web's ladder (`RATES`). */
val RATES = listOf(0.75f, 1f, 1.25f, 1.5f, 1.75f, 2f, 2.5f, 3f)

/** The sleep timer's ready-made durations, in minutes (`SLEEP_MINUTES`). */
val SLEEP_MINUTES = listOf(5, 15, 30, 45, 60)

/** The one message a listener whose NAS went away should ever see. */
const val OFFLINE_MESSAGE =
    "Backhog can't reach this audiobook's files. The drive they live on is probably offline."

/** How often a running player checkpoints its position (WRITE_EVERY_MS). */
const val WRITE_EVERY_MS = 15_000L

/** A rate clamps to the web's [0.75, 3] band, like the web's setRate. */
fun clampRate(rate: Float): Float = rate.coerceIn(0.75f, 3f)

/**
 * Which track owns a global second. The mirror of the server's `Timeline.
 * Locate`: tracks own [global_start, global_start + duration), so a boundary
 * second belongs to the track that is starting, and an unmeasured track owns
 * no time at all — it holds its place in the running order and nothing else.
 */
fun trackAt(timeline: AudioTimeline, global: Double): Int {
    val tracks = timeline.tracks
    for (i in tracks.indices) {
        val track = tracks[i]
        if (track.durationSeconds > 0 && global < track.globalStart + track.durationSeconds) {
            return i
        }
    }
    // Past the end, or a timeline where nothing could be measured: the last
    // track that holds any time, else simply the first file.
    for (i in tracks.indices.reversed()) {
        if (tracks[i].durationSeconds > 0) return i
    }
    return if (tracks.isNotEmpty()) 0 else -1
}

/** The tape's playable span, in seconds — missing files are not on it. */
private fun AudioTimeline.playableDuration(): Double =
    tracks.filterNot { it.missing }.sumOf { if (it.measured) it.durationSeconds else 0.0 }

/**
 * The tape as the playback engine wires it: one concatenated window over the
 * tracks that are present, in order, with the server's measured durations as
 * each one's initial length. Everything the engine and the UI need to move
 * between the server's global seconds and the window's milliseconds lives
 * here, in the server's arithmetic — missing tracks are subtracted out, so
 * the two clocks can drift by exactly the bytes the NAS lost, nothing else.
 */
class BookTape(val timeline: AudioTimeline) {

    /** The tracks the tape is physically built from: present, in running order. */
    val present: List<AudioTrack> = timeline.tracks.filterNot { it.missing }

    /** Nothing playable at all. */
    val isEmpty: Boolean get() = present.isEmpty()

    /**
     * Measured durations of the present tracks, in seconds. An unmeasured
     * track gets the scanner's best guess from its size (the real length
     * replaces the guess the moment the engine loads the file); the honest
     * statement about the whole timeline is [AudioTimeline.degraded], which
     * the UI surfaces the way the web does.
     */
    val presentDurations: List<Double> = present.map { track ->
        if (track.measured) track.durationSeconds else estimateDurationSeconds(track)
    }

    /** The window's total length in ms — what the seek bar spans. */
    val windowDurationMs: Long = (presentDurations.sum() * 1000).toLong()

    /** A guess at an unmeasured file's length: ~96 kbps is the m4b mid-band. */
    private fun estimateDurationSeconds(track: AudioTrack): Double =
        if (track.sizeBytes > 0) track.sizeBytes * 8.0 / 96_000.0 else 0.0

    /**
     * Where a global second lands in the playback window. The server's global
     * arithmetic spans the missing files too; the window does not, so their
     * measured lengths are subtracted on the way past them.
     */
    fun windowPositionMs(serverGlobalSeconds: Double): Long {
        var remaining = serverGlobalSeconds.coerceAtLeast(0.0)
        var base = 0.0
        var presentIndex = 0
        for (track in timeline.tracks) {
            if (track.missing) {
                remaining -= track.durationSeconds
                continue
            }
            val span = presentDurations[presentIndex]
            if (remaining < span) {
                return ((base + remaining) * 1000).toLong()
            }
            remaining -= span
            base += span
            presentIndex++
        }
        // Past the end of everything: the end of the window.
        return (base * 1000).toLong()
    }

    /**
     * The global second the tape is at, said the way the web says it:
     * `track.global_start + seconds into the track`. Uses the server's
     * global_start, so the app's clock reads the same number the web's would
     * for the same spot on the tape.
     */
    fun serverGlobalSeconds(trackIndex: Int, offsetSeconds: Double): Double {
        val track = present.getOrNull(trackIndex) ?: return 0.0
        return track.globalStart + offsetSeconds.coerceAtLeast(0.0)
    }

    /**
     * The position write for wherever the tape is: a timestamp inside one
     * track plus the file it was measured in, because a track-relative
     * position survives re-measuring or re-ordering the timeline in a way a
     * global second would not. Measured tracks clamp to their length; the
     * web's exact clamp. Translating it into a character offset is the
     * server's call, not ours — and the server holds `listen` as the audio
     * write's source, so no source rides along.
     */
    fun writeFor(trackIndex: Int, offsetSeconds: Double): PositionWrite? {
        val track = present.getOrNull(trackIndex) ?: return null
        val clamped = if (track.measured) {
            offsetSeconds.coerceIn(0.0, track.durationSeconds)
        } else {
            offsetSeconds.coerceAtLeast(0.0)
        }
        return PositionWrite(audioFileId = track.id, audioSeconds = clamped)
    }
}

/**
 * Walk the playback window's real period durations (the engine's own numbers,
 * placeholder guesses replaced as each file is opened) to find which track
 * owns a window position and how far into it the tape is.
 *
 * Pure so it can be unit-tested against any list of durations: the service
 * extracts the period lengths from its player and hands them over.
 */
fun trackAndOffset(periodDurationsMs: LongArray, positionMs: Long): Pair<Int, Double>? {
    if (periodDurationsMs.isEmpty()) return null
    var remaining = positionMs.coerceAtLeast(0L)
    for (i in periodDurationsMs.indices) {
        val span = periodDurationsMs[i]
        if (remaining < span) return i to remaining / 1000.0
        // Past the end: hold at the last second of the last track, so the end
        // of the tape writes the end of the tape.
        if (i == periodDurationsMs.lastIndex) return i to span / 1000.0
        remaining -= span
    }
    return periodDurationsMs.lastIndex to 0.0
}
