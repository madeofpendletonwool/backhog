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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Checklist
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.TrackChanges
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.api.Project
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.theme.Backhog

/** The kind labels — the web's PROJECT_KIND_LABELS. */
private fun kindLabel(kind: String): String = when (kind) {
    "checklist" -> "Checklist"
    "count_goal" -> "Count goal"
    "rule_goal" -> "Rule goal"
    else -> kind
}

/**
 * The projects index — the web's ProjectsPage: temporary objectives, in
 * progress and completed, each with its progress bar.
 */
@Composable
fun ProjectsScreen(
    container: AppContainer,
    baseUrl: String,
    onOpenProject: (String) -> Unit,
) {
    val vm: ProjectsViewModel = viewModel(key = "projects|$baseUrl") {
        ProjectsViewModel(container.session, baseUrl)
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
                Text("Projects", style = MaterialTheme.typography.headlineSmall, color = p.c100)
                Text("Temporary objectives — finish a set, hit a count, clear a slice of the backlog.", style = MaterialTheme.typography.bodySmall, color = p.c400)
            }
            Button(onClick = vm::openCreate, contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 14.dp, vertical = 8.dp)) {
                Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(4.dp))
                Text("New project")
            }
        }

        when {
            ui.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            ui.error != null && ui.projects.isEmpty() -> Column(
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
                Text("No projects yet", style = MaterialTheme.typography.titleMedium, color = p.c100)
                Text(
                    "Lists are what exists. Projects are what you're trying to accomplish — give one a target and go.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = p.c400,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
                Button(onClick = vm::openCreate) { Text("Create a project") }
            }
            else -> LazyColumn(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                if (ui.active.isNotEmpty()) {
                    item { SectionHeader("In progress", "Still working toward the target.") }
                    items(ui.active.size) { index -> ProjectCard(ui.active[index], onOpenProject) }
                }
                if (ui.completed.isNotEmpty()) {
                    item { SectionHeader("Completed", "Targets met — or closed by hand. Enjoy the W.") }
                    items(ui.completed.size) { index -> ProjectCard(ui.completed[index], onOpenProject) }
                }
                item { Spacer(Modifier.height(20.dp)) }
            }
        }
    }

    if (create.open) {
        CreateProjectSheet(vm, onDismiss = vm::closeCreate, onCreated = onOpenProject)
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
private fun ProjectCard(project: Project, onOpenProject: (String) -> Unit) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    Column(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, p.edgeStrong, shape)
            .clickable { onOpenProject(project.id) }
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(
                when (project.kind) {
                    "checklist" -> Icons.Filled.Checklist
                    "count_goal" -> Icons.Filled.TrackChanges
                    else -> Icons.Filled.AutoAwesome
                },
                contentDescription = null,
                tint = p.c500,
                modifier = Modifier.size(14.dp),
            )
            Spacer(Modifier.width(6.dp))
            Text(
                project.name,
                style = MaterialTheme.typography.titleMedium,
                color = p.c100,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                kindLabel(project.kind),
                style = MaterialTheme.typography.labelSmall,
                color = p.c400,
                modifier = Modifier
                    .clip(MaterialTheme.shapes.extraSmall)
                    .background(p.fillHover)
                    .padding(horizontal = 8.dp, vertical = 3.dp),
            )
        }
        if (project.description.isNotBlank()) {
            Text(project.description, style = MaterialTheme.typography.labelMedium, color = p.c500, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        LinearProgressIndicator(
            progress = { (project.progress.percent.toFloat() / 100f).coerceIn(0f, 1f) },
            modifier = Modifier.fillMaxWidth().height(6.dp).clip(CircleShape),
            color = if (project.completedAt != null) com.collinpendleton.backhog.ui.theme.Tones.Played else p.hlMid,
            trackColor = p.c800,
        )
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Text(
                "${project.progress.completedCount}/${project.progress.targetCount} finished",
                style = MaterialTheme.typography.labelSmall,
                color = p.c300,
            )
            Text(
                if (project.completedAt != null) "Done ${Format.date(project.completedAt)}"
                else "${Format.hours(project.progress.estHoursRemaining)} to go",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }
    }
}

/** The web's CreateProjectDialog: kind, name, target, and rules for a rule goal. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CreateProjectSheet(vm: ProjectsViewModel, onDismiss: () -> Unit, onCreated: (String) -> Unit) {
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
            Text("New project", style = MaterialTheme.typography.titleLarge, color = p.c100)

            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                KindOption(
                    title = "Checklist",
                    description = "A curated member list; every member done = complete.",
                    active = create.kind == "checklist",
                    modifier = Modifier.weight(1f),
                ) { vm.setCreateKind("checklist") }
                KindOption(
                    title = "Count goal",
                    description = "Finish N games; the whole library counts.",
                    active = create.kind == "count_goal",
                    modifier = Modifier.weight(1f),
                ) { vm.setCreateKind("count_goal") }
            }
            KindOption(
                title = "Rule goal",
                description = "A smart-list rule set defines what counts.",
                active = create.kind == "rule_goal",
                modifier = Modifier.fillMaxWidth(),
            ) { vm.setCreateKind("rule_goal") }

            OutlinedTextField(
                value = create.name,
                onValueChange = vm::setCreateName,
                label = { Text("Name") },
                placeholder = { Text("Clear the soulslikes") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = create.description,
                onValueChange = vm::setCreateDescription,
                label = { Text("Description (optional)") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            if (create.wantsTarget) {
                OutlinedTextField(
                    value = create.targetCount,
                    onValueChange = vm::setCreateTarget,
                    label = { Text("Target count") },
                    supportingText = { Text("Leave empty for \"every match\"") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            if (create.wantsRules) {
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
                androidx.compose.material3.TextButton(onClick = onDismiss) { Text("Cancel") }
                Spacer(Modifier.width(8.dp))
                Button(
                    onClick = { vm.submitCreate(onCreated) },
                    enabled = create.name.isNotBlank() && !create.busy,
                ) {
                    if (create.busy) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    } else {
                        Text("Create project")
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
            fontWeight = FontWeight.Medium,
            color = if (active) p.hlBright else p.c200,
        )
        Text(description, style = MaterialTheme.typography.labelSmall, color = p.c500)
    }
}
