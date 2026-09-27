package com.collinpendleton.backhog.player

import android.content.ComponentName
import android.content.Context
import android.os.Bundle
import androidx.media3.common.C
import androidx.media3.common.PlaybackException
import androidx.media3.common.PlaybackParameters
import androidx.media3.common.Player
import androidx.media3.common.Timeline
import androidx.media3.session.MediaController
import androidx.media3.session.SessionCommand
import androidx.media3.session.SessionToken
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.api.AudioTimeline
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * The app's handle on the playback service: a [MediaController] built once
 * and kept for the life of the process, the state the player UI renders, and
 * the commands it sends.
 *
 * The controller is the same interface the notification and Android Auto
 * hold — one tape, many controllers. Playback is never bound to a screen:
 * navigate anywhere, turn the screen off, kill the app; the sound is the
 * service's, and this class is only how the UI looks at it.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
class PlayerConnection(
    private val context: Context,
    @Suppress("UNUSED_PARAMETER") container: AppContainer,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    private var controller: MediaController? = null
    private var connecting = false

    private val _state = MutableStateFlow(PlayerUiState())
    val state: StateFlow<PlayerUiState> = _state.asStateFlow()

    /** Connects to the service if not already — safe to call from anywhere, any number of times. */
    fun ensure() {
        if (controller != null || connecting) return
        connecting = true
        val token = SessionToken(context, ComponentName(context, PlaybackService::class.java))
        val future = MediaController.Builder(context, token)
            .setListener(ControllerExtrasListener())
            .buildAsync()
        future.addListener(
            {
                val c = try {
                    future.get()
                } catch (t: Throwable) {
                    connecting = false
                    return@addListener
                }
                controller = c
                c.addListener(ControllerListener())
                _state.value = _state.value.copy(connected = true)
                parseExtras(c.sessionExtras)
                pollPosition()
                pollClock()
            },
            Runnable::run,
        )
    }

    /** The clock is its own context: it ticks twice a second while the tape runs. */
    private fun pollClock() {
        scope.launch {
            while (isActive) {
                if (_state.value.isPlaying) pollPosition()
                delay(500)
            }
        }
    }

    private inner class ControllerListener : Player.Listener {
        override fun onIsPlayingChanged(isPlaying: Boolean) {
            _state.value = _state.value.copy(isPlaying = isPlaying)
            pollPosition()
        }

        override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) {
            _state.value = _state.value.copy(playWhenReady = playWhenReady)
        }

        override fun onPlaybackStateChanged(playbackState: Int) {
            pollPosition()
        }

        override fun onPlaybackParametersChanged(playbackParameters: PlaybackParameters) {
            _state.value = _state.value.copy(rate = playbackParameters.speed)
        }

        override fun onPositionDiscontinuity(reason: Int) {
            pollPosition()
        }

        override fun onPlayerError(error: PlaybackException) {
            // The service publishes the readable message in extras; this only
            // nudges the transport to re-read state.
            pollPosition()
        }
    }

    private inner class ControllerExtrasListener : MediaController.Listener {
        override fun onExtrasChanged(controller: MediaController, extras: Bundle) {
            parseExtras(extras)
        }
    }

    private fun parseExtras(extras: Bundle?) {
        if (extras == null) return
        val timelineJson = extras.getString(PlayerContract.EXTRA_TIMELINE)
        val timeline = timelineJson?.let {
            runCatching { ApiClient.json.decodeFromString(AudioTimeline.serializer(), it) }.getOrNull()
        }
        _state.value = _state.value.copy(
            entryId = extras.getString(PlayerContract.EXTRA_ENTRY_ID),
            title = extras.getString(PlayerContract.EXTRA_TITLE) ?: "",
            authors = extras.getString(PlayerContract.EXTRA_AUTHORS) ?: "",
            coverUrl = extras.getString(PlayerContract.EXTRA_COVER) ?: "",
            accentHex = extras.getString(PlayerContract.EXTRA_ACCENT) ?: "",
            timeline = timeline,
            degraded = extras.getBoolean(PlayerContract.EXTRA_DEGRADED, false),
            error = extras.getString(PlayerContract.EXTRA_ERROR),
            sleepKind = extras.getString(PlayerContract.EXTRA_SLEEP_KIND) ?: PlayerContract.SLEEP_OFF,
            sleepMinutes = extras.getInt(PlayerContract.EXTRA_SLEEP_MINUTES, 0),
            sleepEndsAt = extras.getLong(PlayerContract.EXTRA_SLEEP_ENDS_AT, 0L),
            tape = timeline?.let { BookTape(it) },
        )
        pollPosition()
    }

    /** Reads the player's own clock and translates it into the tape's coordinates. */
    private fun pollPosition() {
        val c = controller ?: return
        val durations = periodDurationsMs(c)
        val (index, offset) = trackAndOffset(durations, c.currentPosition) ?: (-1 to 0.0)
        val duration = c.duration.takeIf { it != C.TIME_UNSET } ?: 0L
        _state.value = _state.value.copy(
            isPlaying = c.isPlaying,
            playWhenReady = c.playWhenReady,
            buffering = c.playbackState == Player.STATE_BUFFERING,
            ended = c.playbackState == Player.STATE_ENDED,
            rate = c.playbackParameters.speed,
            positionMs = c.currentPosition.coerceAtLeast(0L),
            durationMs = duration,
            trackIndex = index,
            trackOffsetSeconds = offset,
        )
    }

    /** A nudge for the moments the UI opens onto the player between clock ticks. */
    fun pollPositionForUi() = pollPosition()

    private fun periodDurationsMs(c: Player): LongArray {
        val t = c.currentTimeline
        if (t.isEmpty) return LongArray(0)
        val window = Timeline.Window()
        t.getWindow(c.currentMediaItemIndex.coerceAtLeast(0), window)
        val count = window.lastPeriodIndex - window.firstPeriodIndex + 1
        if (count <= 0) return LongArray(0)
        val period = Timeline.Period()
        return LongArray(count) { i ->
            t.getPeriod(window.firstPeriodIndex + i, period)
            val duration = period.durationMs
            if (duration != C.TIME_UNSET) duration else 0L
        }
    }

    // --- commands -----------------------------------------------------------

    /** Loads a book and (by default) starts it from its stored position. */
    fun open(entryId: String, startAt: Double? = null, autoplay: Boolean = true) {
        ensure()
        val c = controller
        if (c == null) {
            // Still connecting: retry a few times while the controller lands,
            // then give up quietly — the bar simply does not appear yet.
            if (openRetries < 5) {
                openRetries++
                scope.launch {
                    delay(250)
                    open(entryId, startAt, autoplay)
                }
            }
            return
        }
        openRetries = 0
        val args = Bundle().apply {
            putString(PlayerContract.ARG_ENTRY_ID, entryId)
            startAt?.let { putDouble(PlayerContract.ARG_START_AT, it) }
            putBoolean(PlayerContract.ARG_AUTOPLAY, autoplay)
        }
        c.sendCustomCommand(SessionCommand(PlayerContract.CMD_OPEN, Bundle.EMPTY), args)
    }

    private var openRetries = 0

    fun close() {
        controller?.sendCustomCommand(SessionCommand(PlayerContract.CMD_CLOSE, Bundle.EMPTY), Bundle.EMPTY)
    }

    fun reload() {
        controller?.sendCustomCommand(SessionCommand(PlayerContract.CMD_RELOAD, Bundle.EMPTY), Bundle.EMPTY)
    }

    fun setSleep(kind: String, minutes: Int = 0) {
        val args = Bundle().apply {
            putString(PlayerContract.ARG_KIND, kind)
            putInt(PlayerContract.ARG_MINUTES, minutes)
        }
        controller?.sendCustomCommand(SessionCommand(PlayerContract.CMD_SLEEP, Bundle.EMPTY), args)
    }

    fun toggle() {
        val c = controller ?: return
        if (c.isPlaying) c.pause() else c.play()
    }

    fun play() = controller?.play()

    fun pause() = controller?.pause()

    /** Seek in global milliseconds — the bar is the whole book, not one file. */
    fun seekTo(positionMs: Long) {
        controller?.seekTo(positionMs.coerceAtLeast(0L))
    }

    fun skipBack() = controller?.seekBack()

    fun skipForward() = controller?.seekForward()

    /** Chapter/track granularity, the web's nextTrack/previousTrack. */
    fun nextTrack() = controller?.seekToNextMediaItem()

    fun previousTrack() = controller?.seekToPreviousMediaItem()

    fun setRate(rate: Float) {
        controller?.setPlaybackSpeed(clampRate(rate))
    }

    fun release() {
        controller?.release()
        controller = null
        scope.cancel()
    }
}

/**
 * Everything the player UI renders. The clock lives here too (`positionMs`,
 * `trackIndex`, `trackOffsetSeconds`), updated at a half-second cadence while
 * the tape runs — the Compose-side split of the web's clock context.
 */
data class PlayerUiState(
    val connected: Boolean = false,
    val entryId: String? = null,
    val title: String = "",
    val authors: String = "",
    val coverUrl: String = "",
    val accentHex: String = "",
    val timeline: AudioTimeline? = null,
    val tape: BookTape? = null,
    val degraded: Boolean = false,
    val isPlaying: Boolean = false,
    val playWhenReady: Boolean = false,
    val buffering: Boolean = false,
    val ended: Boolean = false,
    val error: String? = null,
    val rate: Float = 1f,
    val positionMs: Long = 0,
    val durationMs: Long = 0,
    /** Which present track holds the tape; -1 before the window is ready. */
    val trackIndex: Int = -1,
    val trackOffsetSeconds: Double = 0.0,
    val sleepKind: String = PlayerContract.SLEEP_OFF,
    val sleepMinutes: Int = 0,
    val sleepEndsAt: Long = 0L,
) {
    val hasBook: Boolean get() = entryId != null

    /** The track now playing — a present one, so index against the tape, not the raw timeline. */
    val currentTrack: com.collinpendleton.backhog.api.AudioTrack?
        get() = tape?.present?.getOrNull(trackIndex)

    /** Where the tape is, in the server's global seconds — the number every surface shows. */
    val globalSeconds: Double
        get() = tape?.serverGlobalSeconds(trackIndex, trackOffsetSeconds) ?: positionMs / 1000.0
}
