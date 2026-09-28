package com.collinpendleton.backhog.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// The Stage 5 file layer, transcribed from web/src/lib/types.ts. Everything
// here is member/admin territory — the routes sit behind RequireMediaManager
// server-side and the app mirrors that gate with `user.canManageMedia`.

/** One file the scanner did not inventory, with the reason why. */
@Serializable
data class MediaSkipped(
    val id: Long = 0,
    val root: String = "",
    val path: String = "",
    val ext: String = "",
    /** Six statements, not one shrug: DRM refusals, KFX, sidecars, unknown. */
    val reason: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
    @SerialName("seen_at") val seenAt: String = "",
)

/** One proposed (book, confidence) pair for an unattached candidate. */
@Serializable
data class MediaSuggestion(
    val book: Book,
    val confidence: Double = 0.0,
    /** "library" or "openlibrary". */
    val source: String = "library",
    /** "sidecar" | "tags" | "directory" | "filename" — which facts matched. */
    val signal: String = "filename",
    @SerialName("in_library") val inLibrary: Boolean = false,
    /** The user's entry, when owned — the attach API is entry-keyed. */
    @SerialName("entry_id") val entryId: String? = null,
)

/**
 * One attachable unit from the review queue: an audiobook directory of
 * ordered files, or a single EPUB. Files are track-ordered for audio —
 * attaching them in array order is the explicit track order.
 */
@Serializable
data class MediaCandidate(
    val key: String,
    val kind: String = "epub",
    val root: String = "",
    @SerialName("dir_path") val dirPath: String = "",
    @SerialName("title_guess") val titleGuess: String = "",
    @SerialName("author_guess") val authorGuess: String = "",
    val files: List<MediaFile> = emptyList(),
    @SerialName("total_duration_seconds") val totalDurationSeconds: Double = 0.0,
    /** Null from older API builds when nothing matched; treat as []. */
    val suggestions: List<MediaSuggestion>? = null,
    @SerialName("high_confidence") val highConfidence: Boolean = false,
    @SerialName("alternate_format") val alternateFormat: Boolean = false,
    @SerialName("alternate_of") val alternateOf: String? = null,
)

/** GET /api/media/candidates. */
@Serializable
data class MediaCandidatesResponse(
    val candidates: List<MediaCandidate> = emptyList(),
    val skipped: List<MediaSkipped> = emptyList(),
)

/** One scan's counts, live while it runs and frozen as `last` once done. */
@Serializable
data class MediaScanResult(
    @SerialName("started_at") val startedAt: String = "",
    @SerialName("finished_at") val finishedAt: String? = null,
    val roots: List<String> = emptyList(),
    val found: Int = 0,
    @SerialName("new") val newFiles: Int = 0,
    val changed: Int = 0,
    val restored: Int = 0,
    val missing: Int = 0,
    val unsupported: Int = 0,
    val sidecars: Int = 0,
    val failed: Int = 0,
    val error: String? = null,
)

/** GET /api/media/scan — the live or last-completed scan summary. */
@Serializable
data class MediaScanStatus(
    val running: Boolean = false,
    val found: Int = 0,
    @SerialName("new") val newFiles: Int = 0,
    val unsupported: Int = 0,
    val last: MediaScanResult? = null,
)

/** GET /api/media/files — the raw inventory, for the path browser. */
@Serializable
data class MediaFilesResponse(val files: List<MediaFile> = emptyList())

@Serializable
data class AttachFilesRequest(
    @SerialName("file_ids") val fileIds: List<Long>,
    val kind: String,
)

@Serializable
data class AttachFilesResponse(
    val attached: Int = 0,
    val files: List<MediaFile> = emptyList(),
)

@Serializable
data class DetachFileResponse(val detached: Boolean = false)

@Serializable
data class PromoteFileResponse(val file: MediaFile? = null)

@Serializable
data class StartedResponse(val started: Boolean = false)

@Serializable
data class IgnoreFilesRequest(@SerialName("file_ids") val fileIds: List<Long>)

@Serializable
data class IgnoreFilesResponse(val ignored: Int = 0)

@Serializable
data class UnignoreFileResponse(val ignored: Boolean = false)

// --- alignment ---------------------------------------------------------------

/** The worker pipeline's view of one alignment job. */
@Serializable
data class AlignmentJob(
    val id: String,
    @SerialName("entry_id") val entryId: String = "",
    /** queued | claimed | transcribing | aligning | ready | failed | low_confidence */
    val state: String = "queued",
    /** 0–1 while the pipeline is running. */
    val progress: Double = 0.0,
    @SerialName("stage_detail") val stageDetail: String = "",
    val error: String? = null,
    val attempts: Int = 0,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
)

/** The stored map one finished alignment produced. */
@Serializable
data class AlignmentRecord(
    val id: String,
    /** aligning | ready | low_confidence | failed */
    val state: String = "ready",
    val coverage: Double = 0.0,
    @SerialName("mean_confidence") val meanConfidence: Double = 0.0,
    val model: String = "",
    @SerialName("created_at") val createdAt: String = "",
)

/** GET /api/books/{entryId}/align — where an entry's alignment stands. */
@Serializable
data class AlignmentStatusView(
    val job: AlignmentJob? = null,
    val alignment: AlignmentRecord? = null,
    /** False when no worker is configured, so a queued job will sit forever. */
    @SerialName("worker_enabled") val workerEnabled: Boolean = false,
)

@Serializable
data class AlignmentEnqueueResponse(val job: AlignmentJob? = null)
