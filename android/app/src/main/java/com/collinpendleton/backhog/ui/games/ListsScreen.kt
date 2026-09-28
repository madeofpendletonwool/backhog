package com.collinpendleton.backhog.ui.games

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.ListSummary
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.theme.Backhog

/**
 * The lists index — the web's ListsPage: smart lists that keep themselves
 * current, and manual ones curated by hand.
 */
@Composable
fun ListsScreen(
    container: AppContainer,
    baseUrl: String,
    onOpenList: (String) -> Unit,
) {
    val vm: ListsViewModel = viewModel(key = "lists|$baseUrl") {
        ListsViewModel(container.session, baseUrl)
    }
    val ui by vm.state.collectAsStateWithLifecycle()
    val create by vm.create.collectAsStateWithLifecycle()
    val p = Backhog.palette

    Column(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
        Row(
            Modifier.fillMaxWidth().padding(top = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text("Lists", style = MaterialTheme.typography.headlineSmall, color = p.c100)
                Text("Hand-picked collections, and smart lists that keep themselves current.", style = MaterialTheme.typography.bodySmall, color = p.c400)
            }
            Button(onClick = vm::openCreate, contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 14.dp, vertical = 8.dp)) {
                Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(4.dp))
                Text("New list")
            }
        }

        when {
            ui.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            ui.error != null && ui.lists.isEmpty() -> Column(
                Modifier.fillMaxSize(),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterVertically),
            ) {
                Text(ui.error ?: "", color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
                Button(onClick = vm::load) {
                    Icon(Icons.Filled.Refresh, contentDescription = null, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("Try again")
                }
            }
            ui.forGames.isEmpty() -> Column(
                Modifier.fillMaxSize().padding(vertical = 48.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("No lists yet", style = MaterialTheme.typography.titleMedium, color = p.c100)
                Text(
                    "Group games however you like — a manual list you curate, or a smart list defined by rules.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = p.c400,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
                Button(onClick = vm::openCreate) { Text("Create a list") }
            }
            else -> LazyColumn(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                if (ui.smart.isNotEmpty()) {
                    item { SectionHeader("Smart lists", "Defined by rules, always up to date.") }
                    items(ui.smart.size) { index -> ListCard(ui.smart[index], onOpenList) }
                }
                if (ui.manual.isNotEmpty()) {
                    item { SectionHeader("Your lists", "Curated by hand.") }
                    items(ui.manual.size) { index -> ListCard(ui.manual[index], onOpenList) }
                }
                item { Spacer(Modifier.height(20.dp)) }
            }
        }
    }

    if (create.open) {
        CreateListSheet(vm, onDismiss = vm::closeCreate, onCreated = onOpenList)
    }
}

@Composable
private fun SectionHeader(title: String, caption: String) {
    val p = Backhog.palette
    Column(Modifier.padding(top = 8.dp)) {
        Text(title, style = MaterialTheme.typography.titleSmall, color = p.c200)
        Text(caption, style = MaterialTheme.typography.labelSmall, color = p.c500)
    }
}

@Composable
private fun ListCard(list: ListSummary, onOpenList: (String) -> Unit) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    Row(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, shape)
            .clickable { onOpenList(list.id) }
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (list.kind == "smart") {
                    Icon(Icons.Filled.AutoAwesome, contentDescription = null, tint = p.hlBright, modifier = Modifier.size(14.dp))
                }
                Text(
                    list.name,
                    style = MaterialTheme.typography.titleMedium,
                    color = p.c100,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (list.description.isNotBlank()) {
                Text(list.description, style = MaterialTheme.typography.labelMedium, color = p.c500, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        Text(
            "${list.count}",
            style = MaterialTheme.typography.labelLarge,
            color = p.c300,
            modifier = Modifier
                .clip(MaterialTheme.shapes.small)
                .background(p.fillHover)
                .padding(horizontal = 10.dp, vertical = 4.dp),
        )
    }
}

/** The web's CreateListDialog: pick a kind, name it, and for smart lists build the rules. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CreateListSheet(vm: ListsViewModel, onDismiss: () -> Unit, onCreated: (String) -> Unit) {
    val create by vm.create.collectAsStateWithLifecycle()
    val p = Backhog.palette

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = p.c950,
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 20.dp)
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text("New list", style = MaterialTheme.typography.titleLarge, color = p.c100)

            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                KindOption(
                    title = "Manual",
                    description = "You choose what goes in.",
                    active = create.kind == "manual",
                    modifier = Modifier.weight(1f),
                ) { vm.setCreateKind("manual") }
                KindOption(
                    title = "Smart",
                    description = "Rules decide, always current.",
                    active = create.kind == "smart",
                    modifier = Modifier.weight(1f),
                ) { vm.setCreateKind("smart") }
            }

            OutlinedTextField(
                value = create.name,
                onValueChange = vm::setCreateName,
                label = { Text("Name") },
                placeholder = { Text(if (create.kind == "smart") "Short and sweet" else "Summer 2026") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = create.description,
                onValueChange = vm::setCreateDescription,
                label = { Text("Description (optional)") },
                placeholder = { Text("What belongs in here?") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )

            if (create.kind == "smart") {
                Column(
                    Modifier
                        .fillMaxWidth()
                        .clip(MaterialTheme.shapes.medium)
                        .background(p.c900)
                        .border(1.dp, p.edgeStrong, MaterialTheme.shapes.medium)
                        .padding(10.dp),
                ) {
                    SmartListBuilder(
                        value = create.rules,
                        fields = create.fields,
                        arenaMedia = "game",
                        onChange = vm::setCreateRules,
                    )
                }
            }

            ErrorText(create.error)

            Row(Modifier.fillMaxWidth().padding(bottom = 24.dp), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onDismiss) { Text("Cancel") }
                Spacer(Modifier.width(8.dp))
                Button(
                    onClick = { vm.submitCreate(onCreated) },
                    enabled = create.name.isNotBlank() && !create.busy,
                ) {
                    if (create.busy) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    } else {
                        Text("Create list")
                    }
                }
            }
        }
    }
}

@Composable
private fun KindOption(title: String, description: String, active: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val p = Backhog.palette
    Column(
        modifier
            .clip(MaterialTheme.shapes.medium)
            .background(if (active) p.hlMid.copy(alpha = 0.12f) else p.c900)
            .border(
                1.dp,
                if (active) p.hlMid.copy(alpha = 0.5f) else p.edgeStrong,
                MaterialTheme.shapes.medium,
            )
            .clickable(onClick = onClick)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(
            title,
            style = MaterialTheme.typography.titleSmall,
            color = if (active) p.hlBright else p.c200,
        )
        Text(description, style = MaterialTheme.typography.labelSmall, color = p.c500)
    }
}
