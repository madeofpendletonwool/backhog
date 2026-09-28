package com.collinpendleton.backhog.ui.books

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.collinpendleton.backhog.api.PassageResult
import com.collinpendleton.backhog.books.PageText
import com.collinpendleton.backhog.books.rememberCameraGranted
import com.collinpendleton.backhog.books.recognizePageLines
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.Field
import com.collinpendleton.backhog.ui.components.PrimaryButton
import com.collinpendleton.backhog.ui.theme.Backhog
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

/**
 * Scan a page of the paper copy and Backhog works out where you are.
 *
 * The whole feature rests on one UI decision, ported from the web: the
 * matched passage is always shown, in the book's own words, beside the
 * answer. An imperfect matcher with a visible passage is perfectly usable —
 * the reader glances at one line and knows instantly whether the map is
 * right — while a perfect-looking page number with nothing behind it is a
 * number nobody can check.
 *
 * Everything on-device: ML Kit reads the photo, the extraction picks the
 * clean prose, and nothing leaves the phone but the passage text POSTed to
 * the user's own server.
 */
@Composable
fun ScanPageSheet(
    vm: BookDetailViewModel,
    copyId: String,
    anchorCount: Int,
    onDone: () -> Unit,
) {
    val p = Backhog.palette
    val scope = rememberCoroutineScope()
    val cameraGranted = rememberCameraGranted()

    var mode by remember { mutableStateOf(if (cameraGranted) "camera" else "type") }
    var reading by remember { mutableStateOf(false) }
    var matching by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var typed by remember { mutableStateOf("") }
    var scan by remember { mutableStateOf<PageText.PageScan?>(null) }
    var result by remember { mutableStateOf<PassageResult?>(null) }
    // Which path produced the offset: a typed sentence is a reader saying
    // "here", a photo is a machine guess.
    var source by remember { mutableStateOf("manual") }
    var page by remember { mutableStateOf("") }
    var alsoSetProgress by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }

    val context = androidx.compose.ui.platform.LocalContext.current

    fun reset() {
        reading = false
        matching = false
        error = null
        scan = null
        result = null
        page = ""
    }

    fun match(text: String) {
        matching = true
        error = null
        scope.launch {
            vm.matchPassage(text)
                .onSuccess {
                    result = it
                    matching = false
                }
                .onFailure { e ->
                    matching = false
                    error = e.message ?: "That passage could not be placed."
                }
        }
    }

    fun readAndMatch(file: File) {
        source = "ocr"
        reading = true
        error = null
        scope.launch {
            try {
                val lines = withContext(Dispatchers.Default) { recognizePageLines(file) }
                val read = PageText.fromLines(lines)
                scan = read
                if (read.pageNumber != null) page = read.pageNumber.toString()
                if (read.passage.split(" ").size < 10) {
                    reading = false
                    error =
                        "That came back as too few readable words to place. More light, the page flatter, or type a sentence from it instead."
                    return@launch
                }
                reading = false
                match(read.passage)
            } catch (e: Exception) {
                reading = false
                error = e.message ?: "The page could not be read."
            }
        }
    }

    fun readAndMatchFromUri(context: android.content.Context, uri: Uri) {
        source = "ocr"
        reading = true
        error = null
        scope.launch {
            try {
                val lines = withContext(Dispatchers.Default) {
                    val cache = File(context.cacheDir, "page-picked-${System.currentTimeMillis()}.jpg")
                    context.contentResolver.openInputStream(uri)?.use { input ->
                        cache.outputStream().use { output -> input.copyTo(output) }
                    } ?: error("That photo could not be read.")
                    recognizePageLines(cache)
                }
                val read = PageText.fromLines(lines)
                scan = read
                if (read.pageNumber != null) page = read.pageNumber.toString()
                if (read.passage.split(" ").size < 10) {
                    reading = false
                    error =
                        "That came back as too few readable words to place. More light, the page flatter, or type a sentence from it instead."
                    return@launch
                }
                reading = false
                match(read.passage)
            } catch (e: Exception) {
                reading = false
                error = e.message ?: "The page could not be read."
            }
        }
    }

    val photoPicker = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri: Uri? ->
        if (uri != null) {
            readAndMatchFromUri(context, uri)
        }
    }

    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp)
            .padding(bottom = 40.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Text("Scan a page", style = MaterialTheme.typography.titleLarge, color = p.c100)
        Text(
            if (anchorCount == 0) {
                "Point at any page of prose. The first scan is what turns this printing's page numbers on."
            } else {
                "$anchorCount page${if (anchorCount == 1) "" else "s"} mapped — accuracy improves as you scan more."
            },
            style = MaterialTheme.typography.bodyMedium,
            color = p.c400,
        )

        val current = result
        if (current != null) {
            // The match review: passage shown in the book's own words, the
            // page number editable (a misread folio would poison the map),
            // and the "move me here too" switch.
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    "Found it — ${"%d%%".format((current.match.confidence * 100).toInt())} confident",
                    style = MaterialTheme.typography.titleMedium,
                    color = p.c200,
                )
                if (current.ambiguous) {
                    Text(
                        "The passage recurs — check the words below really are where you are.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
                Text(
                    buildString {
                        append(current.context.before)
                        append(current.context.passage)
                        append(current.context.after)
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = p.c300,
                )
                Field(
                    value = page,
                    onValueChange = { page = it.filter { c -> c.isDigit() } },
                    label = "Printed page number",
                    keyboardType = KeyboardType.Number,
                    supportingText = scan?.pageNumber?.let { "The folio looked like $it — correct it if that's wrong." },
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = alsoSetProgress, onCheckedChange = { alsoSetProgress = it })
                    Text("Also move me to this spot", style = MaterialTheme.typography.bodyMedium, color = p.c300)
                }
                ErrorText(error)
                val pageNumber = page.toIntOrNull()
                val pageValid = page.isBlank() || (pageNumber != null && pageNumber > 0)
                val somethingToSave = (pageNumber != null && pageNumber > 0) || alsoSetProgress
                PrimaryButton(
                    text = "Save",
                    onClick = {
                        saving = true
                        vm.saveScan(
                            copyId = copyId,
                            printedPage = pageNumber ?: 0,
                            charOffset = current.match.charOffset,
                            source = source,
                            confidence = current.match.confidence,
                            alsoSetProgress = alsoSetProgress,
                        )
                        onDone()
                    },
                    enabled = pageValid && somethingToSave && !saving,
                    busy = saving,
                )
                TextButton(onClick = { reset() }) { Text("Scan another page") }
            }
        } else if (reading || matching) {
            Column(
                Modifier.fillMaxWidth().padding(vertical = 32.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                CircularProgressIndicator()
                Text(if (reading) "Reading the page…" else "Placing it in the book…", color = p.c400)
            }
        } else {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (cameraGranted) {
                    FilterChip(
                        selected = mode == "camera",
                        onClick = { mode = "camera" },
                        label = { Text("Camera") },
                    )
                }
                FilterChip(selected = mode == "photo", onClick = { mode = "photo" }, label = { Text("Photo") })
                FilterChip(selected = mode == "type", onClick = { mode = "type" }, label = { Text("Type a sentence") })
            }

            ErrorText(error)

            when (mode) {
                "camera" -> {
                    com.collinpendleton.backhog.books.PageCamera(
                        onCaptured = { file -> readAndMatch(file) },
                        onError = { error = it },
                    )
                }
                "photo" -> {
                    OutlinedButton(
                        onClick = {
                            photoPicker.launch(
                                PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly),
                            )
                        },
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text("Pick a photo of the page") }
                }
                else -> {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        OutlinedTextField(
                            value = typed,
                            onValueChange = { typed = it },
                            label = { Text("A sentence from the page") },
                            modifier = Modifier.fillMaxWidth(),
                            minLines = 3,
                        )
                        PrimaryButton(
                            text = "Find it",
                            onClick = {
                                source = "manual"
                                scan = null
                                match(typed)
                            },
                            enabled = typed.trim().split(Regex("\\s+")).size >= 10,
                        )
                        Text(
                            "Ten words or so of running prose — not the chapter title, not the running head.",
                            style = MaterialTheme.typography.bodySmall,
                            color = p.c500,
                        )
                    }
                }
            }
        }
    }
}
