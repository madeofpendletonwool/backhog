package com.collinpendleton.backhog.books

import android.Manifest
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ExperimentalGetImage
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageCapture
import androidx.camera.core.ImageCaptureException
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import com.google.mlkit.vision.text.TextRecognition
import com.google.mlkit.vision.text.latin.TextRecognizerOptions
import kotlinx.coroutines.suspendCancellableCoroutine
import java.io.File
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * The camera paths. Both run entirely on the device: the barcode reader and
 * the text recogniser carry their models in the APK (the bundled ML Kit
 * variants), so nothing leaves the phone but the passage text the matcher
 * gets — and that goes to the user's own server.
 */

/** Camera permission as a state, asking when it is not yet granted. */
@Composable
fun rememberCameraGranted(): Boolean {
    val context = LocalContext.current
    var granted by remember {
        mutableStateOf(
            ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) ==
                PackageManager.PERMISSION_GRANTED,
        )
    }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        granted = it
    }
    LaunchedEffect(granted) {
        if (!granted) launcher.launch(Manifest.permission.CAMERA)
    }
    return granted
}

/** The formats a book barcode is actually printed in. */
private fun isbnScannerOptions() = BarcodeScannerOptions.Builder()
    .setBarcodeFormats(
        Barcode.FORMAT_EAN_13,
        Barcode.FORMAT_EAN_8,
        Barcode.FORMAT_UPC_A,
        Barcode.FORMAT_UPC_E,
    )
    .build()

/**
 * The rear camera as a live barcode finder. Detection runs on an analyzer at
 * KEEP_ONLY_LATEST cadence — a barcode does not move fast — and the first
 * value that normalises to an ISBN is handed up once. Anything else the
 * reader reads is reported as a "that isn't a book ISBN" hint.
 */
@OptIn(ExperimentalGetImage::class)
@Composable
fun BarcodeCamera(
    modifier: Modifier = Modifier,
    onIsbn: (String) -> Unit,
    onOther: (String) -> Unit,
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    var settled by remember { mutableStateOf(false) }
    var provider by remember { mutableStateOf<ProcessCameraProvider?>(null) }

    Box(
        modifier
            .fillMaxWidth()
            .height(280.dp)
            .clip(RoundedCornerShape(12.dp)),
    ) {
        AndroidView(
            onRelease = { provider?.unbindAll() },
            factory = { viewContext ->
                val previewView = PreviewView(viewContext)
                val scanner = BarcodeScanning.getClient(isbnScannerOptions())
                val analysis = ImageAnalysis.Builder()
                    .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                    .build()
                analysis.setAnalyzer(ContextCompat.getMainExecutor(viewContext)) { image ->
                    @OptIn(ExperimentalGetImage::class)
                    val media = image.image
                    if (media == null) {
                        image.close()
                        return@setAnalyzer
                    }
                    val input = InputImage.fromMediaImage(media, image.imageInfo.rotationDegrees)
                    scanner.process(input)
                        .addOnSuccessListener { barcodes ->
                            for (barcode in barcodes) {
                                val raw = barcode.rawValue ?: continue
                                val isbn = normalizeIsbn(raw)
                                if (looksLikeIsbn(isbn)) {
                                    if (!settled) {
                                        settled = true
                                        onIsbn(isbn)
                                    }
                                } else {
                                    onOther(raw)
                                }
                            }
                        }
                        .addOnCompleteListener { image.close() }
                }
                val providerFuture = ProcessCameraProvider.getInstance(viewContext)
                providerFuture.addListener({
                    val cameraProvider = providerFuture.get()
                    provider = cameraProvider
                    val preview = Preview.Builder().build().also {
                        it.surfaceProvider = previewView.surfaceProvider
                    }
                    try {
                        cameraProvider.unbindAll()
                        cameraProvider.bindToLifecycle(
                            lifecycleOwner,
                            CameraSelector.DEFAULT_BACK_CAMERA,
                            preview,
                            analysis,
                        )
                    } catch (_: Exception) {
                        // No back camera (or in use): the typed paths still work.
                    }
                }, ContextCompat.getMainExecutor(viewContext))
                previewView
            },
            modifier = Modifier.fillMaxWidth().height(280.dp),
        )
    }
}

/**
 * The page scanner's viewfinder: a live preview with a shutter that writes
 * one JPEG to the cache directory and hands the file up.
 */
@Composable
fun PageCamera(
    modifier: Modifier = Modifier,
    onCaptured: (File) -> Unit,
    onError: (String) -> Unit,
    busy: Boolean = false,
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val capture = remember { ImageCapture.Builder().build() }
    var provider by remember { mutableStateOf<ProcessCameraProvider?>(null) }

    Column(modifier.fillMaxWidth()) {
        Box(
            Modifier
                .fillMaxWidth()
                .height(360.dp)
                .clip(RoundedCornerShape(12.dp)),
        ) {
            AndroidView(
                onRelease = { provider?.unbindAll() },
                factory = { viewContext ->
                    val previewView = PreviewView(viewContext)
                    val providerFuture = ProcessCameraProvider.getInstance(viewContext)
                    providerFuture.addListener({
                        val cameraProvider = providerFuture.get()
                        provider = cameraProvider
                        val preview = Preview.Builder().build().also {
                            it.surfaceProvider = previewView.surfaceProvider
                        }
                        try {
                            cameraProvider.unbindAll()
                            cameraProvider.bindToLifecycle(
                                lifecycleOwner,
                                CameraSelector.DEFAULT_BACK_CAMERA,
                                preview,
                                capture,
                            )
                        } catch (_: Exception) {
                            onError("No camera available.")
                        }
                    }, ContextCompat.getMainExecutor(viewContext))
                    previewView
                },
                modifier = Modifier.fillMaxWidth().height(360.dp),
            )
        }
        Button(
            onClick = {
                val file = File(context.cacheDir, "page-${System.currentTimeMillis()}.jpg")
                capture.takePicture(
                    ImageCapture.OutputFileOptions.Builder(file).build(),
                    ContextCompat.getMainExecutor(context),
                    object : ImageCapture.OnImageSavedCallback {
                        override fun onImageSaved(output: ImageCapture.OutputFileResults) = onCaptured(file)
                        override fun onError(exception: ImageCaptureException) =
                            onError("The photo could not be taken: ${exception.message}")
                    },
                )
            },
            enabled = !busy,
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 16.dp),
        ) {
            if (busy) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            Text(if (busy) "Reading…" else "Photograph the page")
        }
    }
}

// --- ML Kit tasks as coroutines ---------------------------------------------

suspend fun <T> com.google.android.gms.tasks.Task<T>.await(): T =
    suspendCancellableCoroutine { continuation ->
        addOnSuccessListener { result -> continuation.resume(result) }
        addOnFailureListener { e -> continuation.resumeWithException(e) }
        addOnCanceledListener { continuation.cancel() }
    }

/** Reads one photo of a page: the recogniser's lines, top to bottom. */
suspend fun recognizePageLines(file: File): List<String> {
    val bitmap = decodeDownscaled(file, maxWidth = 2000)
        ?: error("There was nothing to read in that image.")
    try {
        val text = TextRecognition.getClient(TextRecognizerOptions.DEFAULT_OPTIONS)
            .process(InputImage.fromBitmap(bitmap, 0))
            .await()
        return text.textBlocks.flatMap { block -> block.lines.map { line -> line.text } }
    } finally {
        bitmap.recycle()
    }
}

/** Recognition does not want a four-thousand-pixel JPEG; neither does memory. */
private fun decodeDownscaled(file: File, maxWidth: Int): Bitmap? {
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeFile(file.path, bounds)
    if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null

    var sample = 1
    while (bounds.outWidth / (sample * 2) >= maxWidth) sample *= 2
    val bitmap = BitmapFactory.decodeFile(file.path, BitmapFactory.Options().apply { inSampleSize = sample })
        ?: return null
    if (bitmap.width <= maxWidth) return bitmap
    val scale = maxWidth.toFloat() / bitmap.width
    val scaled = Bitmap.createScaledBitmap(bitmap, maxWidth, (bitmap.height * scale).toInt(), true)
    if (scaled != bitmap) bitmap.recycle()
    return scaled
}
