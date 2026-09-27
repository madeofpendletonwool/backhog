package com.collinpendleton.backhog.ui.books

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MenuAnchorType
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.Book
import com.collinpendleton.backhog.api.Status
import com.collinpendleton.backhog.books.byline
import com.collinpendleton.backhog.books.editionIsbn
import com.collinpendleton.backhog.books.editionLabel
import com.collinpendleton.backhog.books.publishYear
import com.collinpendleton.backhog.books.rememberCameraGranted
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.Field
import com.collinpendleton.backhog.ui.components.PrimaryButton
import com.collinpendleton.backhog.ui.theme.Backhog

/** The statuses a book can be added in. */
private val ADD_STATUSES = listOf(Status.Backlog, Status.Playing, Status.Played, Status.Wishlist)

/**
 * Three ways to put a book on the shelf, in the order they are fastest:
 * point the camera at the barcode, type the ISBN off the back, or search by
 * title and author. The middle one is the floor — every phone can type — so
 * the scanner is an accelerator, never a requirement.
 */
@Composable
fun AddBookSheet(container: AppContainer, baseUrl: String, onAdded: () -> Unit) {
    val vm: AddBookViewModel = viewModel(factory = AddBookViewModel.Factory(container, baseUrl))
    val state by vm.state.collectAsState()

    val picked = state.picked
    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp)
            .padding(bottom = 40.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        if (picked != null) {
            ConfirmStep(state, vm, baseUrl, onAdded)
        } else {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                AddMode.entries.forEach { mode ->
                    FilterChip(
                        selected = state.mode == mode,
                        onClick = { vm.setMode(mode) },
                        label = {
                            Text(
                                when (mode) {
                                    AddMode.Scan -> "Scan"
                                    AddMode.Isbn -> "ISBN"
                                    AddMode.Search -> "Search"
                                },
                            )
                        },
                    )
                }
            }

            when (state.mode) {
                AddMode.Scan -> ScanStep(state, vm)
                AddMode.Isbn -> IsbnStep(state, vm)
                AddMode.Search -> SearchStep(state, baseUrl, vm)
            }
        }
    }
}

/* ------------------------------------------------------------------- scan */

@Composable
private fun ScanStep(state: AddBookState, vm: AddBookViewModel) {
    val p = Backhog.palette
    val granted = rememberCameraGranted()

    if (!granted) {
        Column(
            Modifier.fillMaxWidth().padding(vertical = 20.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text("Camera access was blocked", style = MaterialTheme.typography.titleSmall, color = p.c200)
            Text(
                "Allow it in settings, or type the ISBN instead — that works everywhere.",
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
            )
            OutlinedButton(onClick = { vm.setMode(AddMode.Isbn) }) { Text("Type the ISBN") }
        }
        return
    }

    if (state.resolving) {
        Box(Modifier.fillMaxWidth().padding(vertical = 40.dp), contentAlignment = Alignment.Center) {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(18.dp))
                Text("Looking up ${state.isbn}…", color = p.c400)
            }
        }
        return
    }

    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        com.collinpendleton.backhog.books.BarcodeCamera(
            onIsbn = vm::scannedCode,
            onOther = { code -> vm.scannedCode(code) },
        )
        Text(
            if (state.notABook.isNotEmpty()) {
                "Read \"${state.notABook}\" — that isn't a book ISBN. Try the barcode above the price."
            } else {
                "Hold the barcode on the back of the book steady in the frame."
            },
            style = MaterialTheme.typography.bodySmall,
            color = p.c500,
        )
        if (state.isbnError != null) {
            ErrorText(state.isbnError)
            OutlinedButton(onClick = { vm.setMode(AddMode.Search) }) { Text("Search by title instead") }
        }
        TextButton(onClick = { vm.setMode(AddMode.Isbn) }) { Text("Type the ISBN instead") }
    }
}

/* ------------------------------------------------------------------- isbn */

@Composable
private fun IsbnStep(state: AddBookState, vm: AddBookViewModel) {
    if (state.resolving) {
        val p = Backhog.palette
        Box(Modifier.fillMaxWidth().padding(vertical = 40.dp), contentAlignment = Alignment.Center) {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(18.dp))
                Text("Looking up ${state.isbn}…", color = p.c400)
            }
        }
        return
    }

    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Field(
            value = state.isbn,
            onValueChange = vm::setIsbn,
            label = "ISBN",
            keyboardType = KeyboardType.Number,
            supportingText = "Ten or thirteen digits, from the back cover or the copyright page. Dashes and spaces are fine.",
        )
        PrimaryButton(
            text = "Look it up",
            onClick = { vm.resolveIsbn() },
            enabled = state.isbnValid,
            busy = state.resolving,
        )
        if (state.isbnError != null) {
            ErrorText(state.isbnError)
            OutlinedButton(onClick = { vm.setMode(AddMode.Search) }) { Text("Search by title instead") }
        }
    }
}

/* ----------------------------------------------------------------- search */

@Composable
private fun SearchStep(state: AddBookState, baseUrl: String, vm: AddBookViewModel) {
    val p = Backhog.palette
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        OutlinedTextField(
            value = state.search,
            onValueChange = vm::setSearch,
            placeholder = { Text("Search by title or author…") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        when {
            state.searchError != null -> ErrorText(state.searchError)
            state.search.trim().length < 2 -> Text(
                "Type at least two characters. Works come from Open Library; you pick the printing on the next step.",
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
            )
            state.searching && state.results.isEmpty() -> Box(
                Modifier.fillMaxWidth().padding(vertical = 24.dp),
                contentAlignment = Alignment.Center,
            ) { CircularProgressIndicator(Modifier.size(20.dp)) }
            state.results.isEmpty() -> Text(
                "Nothing found for \"${state.search}\".",
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
            )
            else -> state.results.forEach { book ->
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clickable { vm.pick(book) }
                        .padding(vertical = 6.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    BookCover(book, baseUrl, Modifier.width(36.dp).aspectRatio(2f / 3f))
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        Text(
                            book.title,
                            style = MaterialTheme.typography.bodyMedium,
                            color = p.c100,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (byline(book).isNotEmpty() || publishYear(book).isNotEmpty()) {
                            Text(
                                listOf(byline(book), publishYear(book)).filter { it.isNotEmpty() }
                                    .joinToString(" · "),
                                style = MaterialTheme.typography.labelSmall,
                                color = p.c400,
                                maxLines = 1,
                            )
                        }
                    }
                    Text("Add", style = MaterialTheme.typography.labelMedium, color = p.hlBright)
                }
            }
        }
    }
}

/* ---------------------------------------------------------------- confirm */

@Composable
private fun ConfirmStep(state: AddBookState, vm: AddBookViewModel, baseUrl: String, onAdded: () -> Unit) {
    val p = Backhog.palette
    val book = state.picked ?: return

    Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
        TextButton(onClick = vm::back, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) {
            Text("Back")
        }

        Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            BookCover(book, baseUrl, Modifier.width(84.dp).aspectRatio(2f / 3f))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    book.title,
                    style = MaterialTheme.typography.titleLarge,
                    color = p.c100,
                )
                Text(
                    listOf(byline(book), publishYear(book)).filter { it.isNotEmpty() }
                        .joinToString(" · ").ifEmpty { "Unknown author" },
                    style = MaterialTheme.typography.bodyMedium,
                    color = p.c400,
                )
            }
        }

        if (book.description.isNotEmpty()) {
            Text(
                book.description,
                style = MaterialTheme.typography.bodySmall,
                color = p.c500,
                maxLines = 4,
                overflow = TextOverflow.Ellipsis,
            )
        }

        StatusPicker(state.status, vm::setStatus)

        EditionPicker(state, vm)

        Text(
            "Page counts belong to a printing, not to the work — pick the one you actually own and the numbers will match your copy.",
            style = MaterialTheme.typography.bodySmall,
            color = p.c500,
        )

        ErrorText(state.addError)

        PrimaryButton(
            text = "Add to shelf",
            onClick = { vm.add(onAdded) },
            busy = state.adding,
        )
    }
}

@Composable
private fun StatusPicker(current: Status, pick: (Status) -> Unit) {
    val p = Backhog.palette
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionLabel("Shelf")
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ADD_STATUSES.forEach { status ->
                FilterChip(
                    selected = current == status,
                    onClick = { pick(status) },
                    label = { Text(status.bookLabel) },
                )
            }
        }
    }
}

@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
private fun EditionPicker(state: AddBookState, vm: AddBookViewModel) {
    val p = Backhog.palette
    var open by remember { mutableStateOf(false) }
    val editions = state.editions
    val selected = editions.firstOrNull { it.id == state.editionId }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionLabel("Printing")
        ExposedDropdownMenuBox(expanded = open, onExpandedChange = { open = it }) {
            OutlinedTextField(
                value = when {
                    editions.isEmpty() -> "No printings on file"
                    selected != null -> editionLabel(selected).ifEmpty {
                        editionIsbn(selected).ifEmpty { selected.id }
                    }
                    else -> "Not sure which one"
                },
                onValueChange = {},
                readOnly = true,
                enabled = editions.isNotEmpty(),
                label = { Text("Printing") },
                trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = open) },
                modifier = Modifier
                    .fillMaxWidth()
                    .menuAnchor(MenuAnchorType.PrimaryNotEditable),
            )
            ExposedDropdownMenu(expanded = open && editions.isNotEmpty(), onDismissRequest = { open = false }) {
                DropdownMenuItem(
                    text = { Text("Not sure which one") },
                    onClick = {
                        vm.setEdition("")
                        open = false
                    },
                )
                editions.forEach { edition ->
                    DropdownMenuItem(
                        text = { Text(editionLabel(edition).ifEmpty { editionIsbn(edition).ifEmpty { edition.id } }) },
                        onClick = {
                            vm.setEdition(edition.id)
                            open = false
                        },
                    )
                }
            }
        }
    }
}
