package com.collinpendleton.backhog.player

import android.app.PendingIntent
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.ForwardingPlayer
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.PlaybackException
import androidx.media3.common.PlaybackParameters
import androidx.media3.common.Player
import androidx.media3.common.Timeline
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.ConcatenatingMediaSource2
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.session.LibraryResult
import androidx.media3.session.MediaLibraryService
import androidx.media3.session.MediaLibraryService.LibraryParams
import androidx.media3.session.MediaLibraryService.MediaLibrarySession
import androidx.media3.session.MediaSession
import androidx.media3.session.SessionCommand
import androidx.media3.session.SessionError
import androidx.media3.session.SessionResult
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.BackhogApp
import com.collinpendleton.backhog.MainActivity
import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.api.AudioTimeline
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.api.toApiError
import com.collinpendleton.backhog.books.byline
import com.google.common.collect.ImmutableList
import com.google.common.util.concurrent.ListenableFuture
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlin.math.abs

/**
 * Backhog's audiobook engine: one foreground service, one session, one tape.
 *
 * The reading/listening handoff only works if Backhog owns playback, so the
 * player is a [MediaLibraryService] rather than a handed-off intent. The
 * service is the only thing that touches sound; the app's player UI, the
 * system's notification and lock screen, and Android Auto are all controllers
 * of this one session — which is the entire Android Auto integration for an
 * audio app: no car app templates, no store listing, no review.
 *
 * The engine itself is ExoPlayer over a [ConcatenatingMediaSource2]: the
 * book's tracks become one single seekable window, so "how far into this book
 * am I" is answerable from the car's seekbar exactly as from the app's. All
 * global-time ↔ (track, offset) translation lives in [BookTape], the port of
 * the web's `useAudioPlayer` arithmetic, and every position write is a
 * track-relative second plus the file that measured it — the server
 * translates, on both the PUT and the may-die POST form.
 */
@androidx.annotation.OptIn(UnstableApi::class)
class PlaybackService : MediaLibraryService() {

    private lateinit var container: AppContainer
    private var baseUrl: String = ""

    /** The service's main scope: dies with the service. */
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    /** Outlives onDestroy just long enough to land the last write. */
    private val farewellScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private var player: ExoPlayer? = null
    private var session: MediaLibrarySession? = null

    /** The book on the tape right now. Null when the player is empty. */
    private var entry: Entry? = null
    private var timeline: AudioTimeline? = null
    private var tape: BookTape? = null
    private var error: String? = null

    // --- the write discipline, ported from the web's flush() ----------------

    /** The global second the server last holds from us; resuming is not a write. */
    private var lastWriteGlobal = Double.NEGATIVE_INFINITY
    private var lastPeriod = -1
    private var ticker: Job? = null

    // --- the sleep timer ------------------------------------------------------

    private var sleepKind = PlayerContract.SLEEP_OFF
    private var sleepMinutes = 0
    private var sleepEndsAt = 0L
    private var sleepAnchorPeriod = -1
    private var sleepJob: Job? = null

    /** Browse results, cached per folder so a car paging through hits the server once. */
    private val browseCache = mutableMapOf<String, Pair<Long, List<MediaItem>>>()

    // --- lifecycle ------------------------------------------------------------

    override fun onCreate() {
        super.onCreate()
        container = (application as BackhogApp).container
        baseUrl = runBlocking { container.preferences.currentBaseUrl() } ?: ""
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaLibrarySession? {
        if (session == null) {
            val base = buildPlayer()
            session = MediaLibrarySession.Builder(this, TapePlayer(base), LibraryCallback())
                .setSessionActivity(sessionActivity())
                .build()
            restoreAfterRestart()
        }
        return session
    }

    private fun sessionActivity(): PendingIntent = PendingIntent.getActivity(
        this,
        0,
        Intent(this, MainActivity::class.java),
        PendingIntent.FLAG_IMMUTABLE,
    )

    private var restoring = false

    /**
     * A service restarted after process death opens with an empty session and
     * a dead player. The last book is remembered, so it is re-opened paused
     * at the server's position — the server's position, because our own last
     * checkpoint is what the server has. The notification appears only when
     * playback actually starts, and there is exactly one session, so there is
     * no double play.
     */
    private fun restoreAfterRestart() {
        if (restoring || tape != null || player?.currentMediaItem != null) return
        restoring = true
        scope.launch {
            val last = container.preferences.lastAudioEntry.first()
            try {
                if (last != null && tape == null && player?.currentMediaItem == null) {
                    open(last, autoplay = false)
                }
            } finally {
                restoring = false
            }
        }
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        super.onTaskRemoved(rootIntent)
        // Media3 keeps a playing session alive and pauses an idle one; either
        // way the tape's position belongs to the server before we go.
        checkpoint(beacon = false)
    }

    override fun onDestroy() {
        checkpoint(beacon = true)
        player?.release()
        player = null
        session?.release()
        session = null
        scope.cancel()
        super.onDestroy()
    }

    // --- the engine -----------------------------------------------------------

    private fun dataSourceFactory(): DefaultDataSource.Factory = DefaultDataSource.Factory(
        this,
        OkHttpDataSource.Factory(container.mediaClient),
    )

    private fun mediaSourceFactory(): DefaultMediaSourceFactory =
        DefaultMediaSourceFactory(dataSourceFactory())

    private fun buildPlayer(): ExoPlayer =
        ExoPlayer.Builder(this, mediaSourceFactory())
            // Audiobooks are speech: focus loss to navigation or a call ducks
            // rather than being fought, and a headphone unplug pauses.
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(C.USAGE_MEDIA)
                    .setContentType(C.AUDIO_CONTENT_TYPE_SPEECH)
                    .build(),
                /* handleAudioFocus = */ true,
            )
            .setHandleAudioBecomingNoisy(true)
            .setWakeMode(C.WAKE_MODE_LOCAL)
            // The car's seek buttons default to the audiobook pair when it
            // asks for none, and ours are always the same: 30 back, 30
            // forward — back far enough to re-hear a sentence.
            .setSeekBackIncrementMs(30_000)
            .setSeekForwardIncrementMs(30_000)
            .build()
            .also { it.addListener(EngineListener()) }

    /**
     * The window as ExoPlayer really holds it: the present tracks' period
     * lengths, server-measured placeholders replaced by container-truth as
     * each file opens.
     */
    private fun periodDurationsMs(): LongArray {
        val p = player ?: return LongArray(0)
        val t = p.currentTimeline
        if (t.isEmpty) return LongArray(0)
        val window = Timeline.Window()
        t.getWindow(p.currentMediaItemIndex.coerceAtLeast(0), window)
        val count = window.lastPeriodIndex - window.firstPeriodIndex + 1
        if (count <= 0) return LongArray(0)
        val period = Timeline.Period()
        return LongArray(count) { i ->
            t.getPeriod(window.firstPeriodIndex + i, period)
            val duration = period.durationMs
            if (duration != C.TIME_UNSET) duration else 0L
        }
    }

    /** The tape's own number for where it is: the server's global arithmetic. */
    private fun currentGlobalSeconds(): Double {
        val t = tape ?: return 0.0
        val p = player ?: return 0.0
        val (index, offset) = trackAndOffset(periodDurationsMs(), p.currentPosition) ?: return 0.0
        return t.serverGlobalSeconds(index, offset)
    }

    private fun currentPeriodIndex(): Int =
        trackAndOffset(periodDurationsMs(), player?.currentPosition ?: 0L)?.first ?: -1

    private fun trackUrl(entryId: String, trackId: Long): String =
        "${baseUrl.trimEnd('/')}/api/books/$entryId/audio/$trackId"

    // --- opening, closing, retrying -------------------------------------------

    /** Opens a book on the tape: fetches what it needs, wires the window, seeks. */
    private suspend fun open(entryId: String, startAt: Double? = null, autoplay: Boolean = true) {
        val api = container.session.api(baseUrl) ?: run {
            error = "No server is configured."
            publishExtras()
            return
        }
        error = null
        publishExtras()

        val opened = apiCall {
            val e = api.entry(entryId)
            val audio = api.bookAudio(entryId)
            // The position is a nicety — a book with no stored position starts
            // at zero, which is not worth failing the whole open for.
            val resume = startAt ?: apiCall { api.bookPosition(entryId) }
                .getOrNull()
                ?.audio
                ?.seconds
                ?: 0.0
            Triple(e, audio, resume)
        }
        opened.onSuccess { (e, audio, resume) -> load(e, audio, resume, autoplay) }
            .onFailure { t ->
                error = t.toApiError().message ?: "That audiobook could not be loaded."
                publishExtras()
            }
    }

    private fun load(e: Entry, audio: AudioTimeline, resume: Double, autoplay: Boolean) {
        val p = player ?: return
        val newTape = BookTape(audio)
        if (newTape.isEmpty) {
            entry = e
            timeline = audio
            tape = newTape
            error = "This book has an audiobook attached, but none of its files are readable."
            publishExtras()
            return
        }

        // Opening a different book checkpoints the old one first, like the
        // web's open(); the writes are keyed to the file they were measured
        // in, so the server files each under the right book.
        checkpoint(beacon = false)

        entry = e
        timeline = audio
        tape = newTape
        error = null
        lastPeriod = -1
        lastWriteGlobal = resume // resuming is not a position change

        val source = ConcatenatingMediaSource2.Builder()
            .setMediaSourceFactory(mediaSourceFactory())
            .setMediaItem(bookItem(e))
            .apply {
                newTape.present.forEachIndexed { i, track ->
                    // The server measured these lengths; the engine takes them
                    // as each period's opening truth and corrects to the
                    // container's own number as files open.
                    val placeholder = (newTape.presentDurations[i] * 1000).toLong().coerceAtLeast(1L)
                    add(
                        MediaItem.Builder()
                            .setMediaId("track:${track.id}")
                            .setUri(Uri.parse(trackUrl(e.id, track.id)))
                            .setMediaMetadata(trackItemMetadata(track))
                            .build(),
                        placeholder,
                    )
                }
            }
            .build()

        scope.launch { p.playbackParameters = PlaybackParameters(container.preferences.audioRate.first()) }
        p.setMediaSource(source, newTape.windowPositionMs(resume))
        p.prepare()
        if (autoplay) p.play()

        scope.launch { container.preferences.setLastAudioEntry(e.id) }
        publishExtras()
    }

    /** Closes the player: checkpoint, empty the tape, forget the book. */
    private fun close() {
        checkpoint(beacon = false)
        player?.stop()
        player?.clearMediaItems()
        entry = null
        timeline = null
        tape = null
        error = null
        lastWriteGlobal = Double.NEGATIVE_INFINITY
        lastPeriod = -1
        setSleep(PlayerContract.SLEEP_OFF, 0)
        scope.launch { container.preferences.setLastAudioEntry(null) }
        publishExtras()
    }

    /**
     * Try the tape again after it failed: the timeline is refetched first,
     * because a file the scanner marked missing stays missing in our copy
     * until the NAS is walked again.
     */
    private fun reload() {
        val id = entry?.id ?: return
        val resume = currentGlobalSeconds()
        scope.launch { open(id, startAt = resume, autoplay = true) }
    }

    // --- the engine's own events ------------------------------------------------

    private inner class EngineListener : Player.Listener {
        override fun onIsPlayingChanged(isPlaying: Boolean) {
            if (isPlaying) {
                startTicker()
            } else {
                ticker?.cancel()
                ticker = null
                // The pause is a write of its own; a client that dies here has
                // already reported the second it stopped at.
                checkpoint(beacon = false)
            }
        }

        override fun onPlaybackStateChanged(playbackState: Int) {
            if (playbackState == Player.STATE_ENDED) {
                // The end of the book: hold at the last second and checkpoint
                // it, so reopening says "finished" rather than rewinding.
                checkpoint(force = true, beacon = false)
            }
        }

        override fun onPositionDiscontinuity(reason: Int) {
            // A seek from any surface (app, notification, car scrubber) that
            // moved the tape at least a second is worth the server knowing.
            checkpoint(beacon = false)
            checkChapterSleep()
        }

        override fun onPlaybackParametersChanged(playbackParameters: PlaybackParameters) {
            // Speed is a preference, not a per-book setting — whoever set it
            // from the car set it for the next book too.
            scope.launch {
                container.preferences.setAudioRate(clampRate(playbackParameters.speed))
            }
        }

        override fun onPlayerError(playbackError: PlaybackException) {
            error = if (tape != null) OFFLINE_MESSAGE else "This track could not be played."
            publishExtras()
        }
    }

    /** While the tape runs: checkpoint on the web's 15-second throttle. */
    private fun startTicker() {
        if (ticker?.isActive == true) return
        ticker = scope.launch {
            while (isActive) {
                delay(WRITE_EVERY_MS)
                checkpoint(beacon = false)
                checkChapterSleep()
            }
        }
    }

    // --- the write ----------------------------------------------------------------

    /**
     * Checkpoint the position: PUT on the 15-second throttle, pause, seek and
     * track change; POST — the beacon form, the one for a client that may die
     * before its next request — when the service goes away. A write only
     * goes when the tape has actually moved a second since the last one, so
     * a paused player never drags a page somebody just photographed back to
     * wherever the audio stopped. `force` is the end of the tape: a position
     * in its own right even when nothing moved.
     */
    private fun checkpoint(force: Boolean = false, beacon: Boolean) {
        val p = player ?: return
        val t = tape ?: return
        val id = entry?.id ?: return
        val (index, offset) = trackAndOffset(periodDurationsMs(), p.currentPosition) ?: return
        val global = t.serverGlobalSeconds(index, offset)

        // A track change is a boundary the server should hear about even at
        // the same elapsed second (the web forces there too).
        val crossedBoundary = lastPeriod != -1 && lastPeriod != index
        lastPeriod = index

        if (!force && !crossedBoundary && abs(global - lastWriteGlobal) < 1.0) return
        val write = t.writeFor(index, offset) ?: return
        lastWriteGlobal = global

        farewellScope.launch {
            apiCall {
                val api = container.session.api(baseUrl) ?: return@apiCall
                if (beacon) api.postBookPosition(id, write) else api.putBookPosition(id, write)
            }
            // A dropped checkpoint is not worth interrupting playback for;
            // the next one is fifteen seconds away.
        }
    }

    // --- the sleep timer -------------------------------------------------------------

    private fun setSleep(kind: String, minutes: Int) {
        sleepJob?.cancel()
        sleepJob = null
        sleepKind = kind
        sleepMinutes = minutes
        sleepAnchorPeriod = -1
        when (kind) {
            PlayerContract.SLEEP_MINUTES -> {
                sleepEndsAt = System.currentTimeMillis() + minutes * 60_000L
                sleepJob = scope.launch {
                    delay(minutes * 60_000L)
                    player?.pause()
                    setSleep(PlayerContract.SLEEP_OFF, 0)
                }
            }
            PlayerContract.SLEEP_CHAPTER -> {
                // "End of chapter" anchors to the track now playing: the next
                // boundary stops the tape, ready for next time.
                sleepAnchorPeriod = currentPeriodIndex()
            }
            else -> {
                sleepEndsAt = 0L
            }
        }
        publishExtras()
    }

    /** Stops at the boundary when the chapter sleep timer crosses one. */
    private fun checkChapterSleep() {
        if (sleepKind != PlayerContract.SLEEP_CHAPTER) return
        val anchor = sleepAnchorPeriod
        if (anchor == -1) {
            sleepAnchorPeriod = currentPeriodIndex()
            return
        }
        if (currentPeriodIndex() != anchor) {
            player?.pause()
            setSleep(PlayerContract.SLEEP_OFF, 0)
        }
    }

    // --- the surfaces' shared picture --------------------------------------------------

    private fun bookItem(e: Entry): MediaItem {
        val brief = e.book
        return MediaItem.Builder()
            .setMediaId("book:${e.id}")
            .setMediaMetadata(
                MediaMetadata.Builder()
                    .setTitle(brief?.title ?: "Untitled")
                    .setArtist(byline(brief?.authors).ifEmpty { "Unknown author" })
                    .setAlbumTitle("Backhog")
                    .setMediaType(MediaMetadata.MEDIA_TYPE_AUDIO_BOOK)
                    .setIsBrowsable(false)
                    .setIsPlayable(true)
                    .apply {
                        val cover = brief?.coverUrl
                        if (!cover.isNullOrEmpty()) {
                            setArtworkUri(Uri.parse("${baseUrl.trimEnd('/')}/api/covers/book/${brief.id}"))
                        }
                    }
                    .build(),
            )
            .build()
    }

    private fun trackItemMetadata(track: com.collinpendleton.backhog.api.AudioTrack): MediaMetadata =
        MediaMetadata.Builder()
            .setTitle(track.title)
            .setMediaType(MediaMetadata.MEDIA_TYPE_AUDIO_BOOK_CHAPTER)
            .setIsBrowsable(false)
            .setIsPlayable(true)
            .build()

    /**
     * One shelf book as the car sees it: title, author, cover, and how far
     * through it its listener is.
     */
    private fun browseItem(entry: Entry, percent: Double?): MediaItem {
        val extras = Bundle().apply {
            percent?.let { p ->
                putInt(
                    EXTRA_COMPLETION_STATUS,
                    when {
                        p >= 100.0 -> COMPLETION_FULLY_PLAYED
                        p > 0.0 -> COMPLETION_PARTIALLY_PLAYED
                        else -> COMPLETION_NOT_PLAYED
                    },
                )
                putDouble(EXTRA_COMPLETION_PERCENTAGE, (p / 100.0).coerceIn(0.0, 1.0))
            }
        }
        return bookItem(entry).buildUpon()
            .setMediaMetadata(bookItem(entry).mediaMetadata.buildUpon().setExtras(extras).build())
            .build()
    }

    /**
     * Publishes the session-level picture the app UI renders from — the
     * entry, the timeline, the sleep timer, the error — as session extras.
     * The clock, the speed and the transport are the player's own state; the
     * controllers read those directly.
     */
    private fun publishExtras() {
        val s = session ?: return
        val bundle = Bundle().apply {
            putString(PlayerContract.EXTRA_ENTRY_ID, entry?.id)
            putString(PlayerContract.EXTRA_TITLE, entry?.book?.title ?: "")
            putString(PlayerContract.EXTRA_AUTHORS, byline(entry?.book?.authors))
            val cover = entry?.book?.coverUrl
            if (!cover.isNullOrEmpty()) {
                putString(
                    PlayerContract.EXTRA_COVER,
                    "${baseUrl.trimEnd('/')}/api/covers/book/${entry?.book?.id}",
                )
            }
            putString(PlayerContract.EXTRA_ACCENT, entry?.book?.accentHex ?: "")
            timeline?.let {
                putString(PlayerContract.EXTRA_TIMELINE, ApiClient.json.encodeToString(AudioTimeline.serializer(), it))
            }
            putBoolean(PlayerContract.EXTRA_DEGRADED, timeline?.degraded == true)
            putString(PlayerContract.EXTRA_ERROR, error)
            putString(PlayerContract.EXTRA_SLEEP_KIND, sleepKind)
            putInt(PlayerContract.EXTRA_SLEEP_MINUTES, sleepMinutes)
            putLong(PlayerContract.EXTRA_SLEEP_ENDS_AT, sleepEndsAt)
        }
        s.setSessionExtras(bundle)
    }

    // --- the tape's chapter/track jumps --------------------------------------------

    /**
     * The player the session publishes: a single window whose next/previous
     * are the tape's track boundaries, because a car's next-button should
     * mean "next chapter", not nothing (one window) or "next book" (many).
     */
    private inner class TapePlayer(private val delegate: ExoPlayer) : ForwardingPlayer(delegate) {

        override fun seekToNextMediaItem() {
            jumpPeriod(+1)
        }

        override fun seekToPreviousMediaItem() {
            jumpPeriod(-1)
        }

        override fun isCommandAvailable(command: Int): Boolean = when (command) {
            Player.COMMAND_SEEK_TO_NEXT_MEDIA_ITEM, Player.COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM ->
                periodCount() > 1
            else -> super.isCommandAvailable(command)
        }

        override fun getAvailableCommands(): Player.Commands {
            val commands = super.getAvailableCommands()
            if (periodCount() <= 1) return commands
            return commands.buildUpon()
                .add(Player.COMMAND_SEEK_TO_NEXT_MEDIA_ITEM)
                .add(Player.COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM)
                .build()
        }

        private fun periodCount(): Int = windowPeriodCount(delegate)

        private fun windowPeriodCount(p: Player): Int {
            val t = p.currentTimeline
            if (t.isEmpty) return 0
            val window = Timeline.Window()
            t.getWindow(p.currentMediaItemIndex.coerceAtLeast(0), window)
            return window.lastPeriodIndex - window.firstPeriodIndex + 1
        }
    }

    /** Seek to a period boundary, keeping the web's track-jump semantics. */
    private fun jumpPeriod(delta: Int) {
        val p = player ?: return
        val durations = periodDurationsMs()
        if (durations.isEmpty()) return
        val current = trackAndOffset(durations, p.currentPosition)?.first ?: return
        val target = if (delta > 0) {
            current + 1
        } else {
            // Restart the current track unless you are barely into it.
            val into = p.currentPosition - durations.take(current).sum()
            if (into > 3_000 || current == 0) current else current - 1
        }
        if (target < 0 || target >= durations.size) return
        val wasPlaying = p.playWhenReady
        p.seekTo(durations.take(target).sum())
        p.playWhenReady = wasPlaying
    }

    // --- controllers -------------------------------------------------------------------

    private inner class LibraryCallback : MediaLibrarySession.Callback {

        override fun onConnect(
            session: MediaSession,
            controller: MediaSession.ControllerInfo,
        ): MediaSession.ConnectionResult {
            val sessionCommands = MediaSession.ConnectionResult.DEFAULT_SESSION_AND_LIBRARY_COMMANDS
                .buildUpon()
                .apply {
                    PlayerContract.COMMANDS.forEach { add(SessionCommand(it, Bundle.EMPTY)) }
                }
                .build()
            return MediaSession.ConnectionResult.AcceptedResultBuilder(session)
                .setAvailableSessionCommands(sessionCommands)
                .build()
        }

        override fun onCustomCommand(
            session: MediaSession,
            controller: MediaSession.ControllerInfo,
            customCommand: SessionCommand,
            args: Bundle,
        ): ListenableFuture<SessionResult> {
            when (customCommand.customAction) {
                PlayerContract.CMD_OPEN -> {
                    val entryId = args.getString(PlayerContract.ARG_ENTRY_ID)
                        ?: return SettableFuture.immediate(SessionResult(SessionError.ERROR_BAD_VALUE))
                    val startAt = if (args.containsKey(PlayerContract.ARG_START_AT)) {
                        args.getDouble(PlayerContract.ARG_START_AT)
                    } else {
                        null
                    }
                    val autoplay = args.getBoolean(PlayerContract.ARG_AUTOPLAY, true)
                    scope.launch { open(entryId, startAt, autoplay) }
                }
                PlayerContract.CMD_CLOSE -> close()
                PlayerContract.CMD_RELOAD -> reload()
                PlayerContract.CMD_SLEEP -> setSleep(
                    args.getString(PlayerContract.ARG_KIND) ?: PlayerContract.SLEEP_OFF,
                    args.getInt(PlayerContract.ARG_MINUTES, 0),
                )
                else -> return SettableFuture.immediate(SessionResult(SessionError.ERROR_NOT_SUPPORTED))
            }
            return SettableFuture.immediate(SessionResult(SessionResult.RESULT_SUCCESS))
        }

        // --- the car's shelf: Android Auto's browse tree -------------------

        override fun onGetLibraryRoot(
            session: MediaLibrarySession,
            browser: MediaSession.ControllerInfo,
            params: LibraryParams?,
        ): ListenableFuture<LibraryResult<MediaItem>> =
            SettableFuture.immediate(LibraryResult.ofItem(folderItem(ROOT_ID, "Backhog"), null))

        override fun onGetChildren(
            session: MediaLibrarySession,
            browser: MediaSession.ControllerInfo,
            parentId: String,
            page: Int,
            pageSize: Int,
            params: LibraryParams?,
        ): ListenableFuture<LibraryResult<ImmutableList<MediaItem>>> {
            val future = SettableFuture<LibraryResult<ImmutableList<MediaItem>>>()
            when (parentId) {
                ROOT_ID -> future.set(
                    LibraryResult.ofItemList(
                        listOf(
                            folderItem(CONTINUE_ID, "Continue listening"),
                            folderItem(RECENT_ID, "Recently played"),
                            folderItem(SHELF_ID, "Your shelf"),
                        ),
                        null,
                    ),
                )
                CONTINUE_ID -> cachedOrFetch(parentId, future) { continueListening() }
                RECENT_ID -> cachedOrFetch(parentId, future) { recentlyPlayed() }
                SHELF_ID -> cachedOrFetch(parentId, future) { theShelf() }
                else -> future.set(LibraryResult.ofItemList(emptyList(), null))
            }
            return future
        }

        override fun onGetItem(
            session: MediaLibrarySession,
            browser: MediaSession.ControllerInfo,
            mediaId: String,
        ): ListenableFuture<LibraryResult<MediaItem>> {
            val future = SettableFuture<LibraryResult<MediaItem>>()
            when (mediaId) {
                ROOT_ID -> future.set(LibraryResult.ofItem(folderItem(mediaId, "Backhog"), null))
                CONTINUE_ID -> future.set(LibraryResult.ofItem(folderItem(mediaId, "Continue listening"), null))
                RECENT_ID -> future.set(LibraryResult.ofItem(folderItem(mediaId, "Recently played"), null))
                SHELF_ID -> future.set(LibraryResult.ofItem(folderItem(mediaId, "Your shelf"), null))
                else -> {
                    val entryId = mediaId.removePrefix("entry:")
                    if (entryId == mediaId) {
                        future.set(LibraryResult.ofError(SessionError.ERROR_BAD_VALUE))
                    } else {
                        // Completed off the application thread: a legacy car
                        // browser blocks the main thread waiting on this
                        // future, so it must not be completed by a main-thread
                        // post — the deadlock the media3 docs warn about.
                        scope.launch(Dispatchers.IO) {
                            val api = container.session.api(baseUrl)
                            val found = api?.let { apiCall { api.entry(entryId) }.getOrNull() }
                            if (found != null) {
                                future.set(LibraryResult.ofItem(browseItem(found, null), null))
                            } else {
                                future.set(LibraryResult.ofError(SessionError.ERROR_IO))
                            }
                        }
                    }
                }
            }
            return future
        }
    }

    private fun folderItem(id: String, title: String): MediaItem =
        MediaItem.Builder()
            .setMediaId(id)
            .setMediaMetadata(
                MediaMetadata.Builder()
                    .setTitle(title)
                    .setMediaType(MediaMetadata.MEDIA_TYPE_FOLDER_MIXED)
                    .setIsBrowsable(true)
                    .setIsPlayable(false)
                    .build(),
            )
            .build()

    private fun cachedOrFetch(
        id: String,
        future: SettableFuture<LibraryResult<ImmutableList<MediaItem>>>,
        fetch: suspend () -> List<MediaItem>,
    ) {
        // See onGetItem: completed on IO, never by a main-thread post.
        scope.launch(Dispatchers.IO) {
            val cached = browseCache[id]
            if (cached != null && System.currentTimeMillis() - cached.first < BROWSE_CACHE_MS) {
                future.set(LibraryResult.ofItemList(cached.second, null))
                return@launch
            }
            val items = try {
                fetch()
            } catch (t: Throwable) {
                if (t is kotlinx.coroutines.CancellationException) throw t
                future.set(LibraryResult.ofError(SessionError.ERROR_IO))
                return@launch
            }
            browseCache[id] = System.currentTimeMillis() to items
            future.set(LibraryResult.ofItemList(items, null))
        }
    }

    // --- browse content ------------------------------------------------------------

    /** In-progress books, most recently moved first — the web's continuation row. */
    private suspend fun continueListening(): List<MediaItem> {
        val api = container.session.api(baseUrl) ?: return emptyList()
        return apiCall { api.readingNow() }.getOrNull()?.books.orEmpty()
            .take(10)
            .map { browseItem(it.entry, it.percent) }
    }

    /** The books whose progress last came from the tape. */
    private suspend fun recentlyPlayed(): List<MediaItem> {
        val api = container.session.api(baseUrl) ?: return emptyList()
        return apiCall { api.readingNow() }.getOrNull()?.books.orEmpty()
            .filter { it.audio }
            .take(10)
            .map { browseItem(it.entry, it.percent) }
    }

    /** The shelf itself, first page. */
    private suspend fun theShelf(): List<MediaItem> {
        val api = container.session.api(baseUrl) ?: return emptyList()
        return apiCall { api.library(media = "book", sort = "updated", limit = 50) }
            .getOrNull()
            ?.entries
            .orEmpty()
            .map { browseItem(it, it.progressPercent) }
    }

    companion object {
        const val ROOT_ID = "root"
        const val CONTINUE_ID = "continue"
        const val RECENT_ID = "recent"
        const val SHELF_ID = "shelf"
        const val BROWSE_CACHE_MS = 120_000L

        // The media-browser completion extras a car reads for progress, by
        // value: media3's MediaConstants carries them but is restricted to
        // its own library. These strings are the stable, documented contract.
        private const val EXTRA_COMPLETION_STATUS = "android.media.extra.PLAYBACK_STATUS"
        private const val EXTRA_COMPLETION_PERCENTAGE = "androidx.media.MediaItem.Extras.COMPLETION_PERCENTAGE"
        private const val COMPLETION_NOT_PLAYED = 0
        private const val COMPLETION_PARTIALLY_PLAYED = 1
        private const val COMPLETION_FULLY_PLAYED = 2
    }
}
