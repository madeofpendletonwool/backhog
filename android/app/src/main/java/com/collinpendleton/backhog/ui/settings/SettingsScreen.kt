package com.collinpendleton.backhog.ui.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.FilterQuality
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.res.imageResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.BuildConfig
import com.collinpendleton.backhog.R
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.ChangePasswordRequest
import com.collinpendleton.backhog.api.User
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.data.Arena
import com.collinpendleton.backhog.data.ThemeSettings
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.Field
import com.collinpendleton.backhog.ui.components.Panel
import com.collinpendleton.backhog.ui.components.PrimaryButton
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.ThemeFamily
import com.collinpendleton.backhog.ui.theme.ThemeId
import com.collinpendleton.backhog.ui.theme.Tones
import kotlinx.coroutines.launch

@Composable
fun SettingsScreen(container: AppContainer, user: User, baseUrl: String) {
    val themes by container.preferences.themes.collectAsStateWithLifecycle(ThemeSettings())
    val arena by container.preferences.arena.collectAsStateWithLifecycle(Arena.Games)
    val scope = rememberCoroutineScope()
    var confirmSwitch by remember { mutableStateOf(false) }
    val p = Backhog.palette

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        HogHeader(container, baseUrl)

        Panel {
            Text(user.username, style = MaterialTheme.typography.titleLarge, color = p.c100)
            Text(user.email, style = MaterialTheme.typography.bodyMedium, color = p.c400)
            ToneChip(user.role.label, if (user.isAdmin) Tones.Gold else Tones.Playing)
            Text(user.role.blurb, style = MaterialTheme.typography.bodySmall, color = p.c500)
            Text(baseUrl, style = MaterialTheme.typography.bodySmall, color = p.c500)
        }

        Section("Appearance") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Same theme in both arenas", color = p.c100)
                    Text(
                        "Off gives Games and Books a look of their own.",
                        style = MaterialTheme.typography.bodySmall,
                        color = p.c500,
                    )
                }
                Switch(
                    checked = themes.linked,
                    onCheckedChange = { linked -> scope.launch { container.preferences.setLinked(linked, arena) } },
                )
            }
            if (themes.linked) {
                ThemePicker(themes.forArena(arena)) { scope.launch { container.preferences.setTheme(it, arena) } }
            } else {
                Arena.entries.forEach { a ->
                    Text(a.label, style = MaterialTheme.typography.titleSmall, color = p.c300)
                    ThemePicker(themes.forArena(a)) { scope.launch { container.preferences.setTheme(it, a) } }
                }
            }
        }

        Section("Password") { ChangePassword(container) }

        Section("Shared books") { SharedBooksSection(container, baseUrl) }

        Section("Account") {
            OutlinedButton(onClick = { scope.launch { container.session.logout() } }, modifier = Modifier.fillMaxWidth()) {
                Text("Sign out")
            }
            TextButton(onClick = { confirmSwitch = true }, modifier = Modifier.fillMaxWidth()) {
                Text("Switch server")
            }
        }

        Text(
            "Backhog for Android ${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE})",
            style = MaterialTheme.typography.bodySmall,
            color = p.c600,
        )
        Spacer(Modifier.size(12.dp))
    }

    if (confirmSwitch) {
        AlertDialog(
            onDismissRequest = { confirmSwitch = false },
            title = { Text("Switch server?") },
            text = { Text("You will be signed out of $baseUrl on this device.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmSwitch = false
                    scope.launch { container.session.switchServer() }
                }) { Text("Switch") }
            },
            dismissButton = { TextButton(onClick = { confirmSwitch = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun Section(title: String, content: @Composable () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text(title.uppercase(), style = MaterialTheme.typography.labelMedium, color = Backhog.palette.c500)
        Panel { content() }
    }
}

/**
 * The settings header: the hog itself. Ten taps on the logo hatch the Hog
 * Watcher egg — the app's identity surface, standing in for the web's
 * sidebar logo. The counter resets on any leave, like one sitting.
 */
@Composable
private fun HogHeader(container: AppContainer, baseUrl: String) {
    val p = Backhog.palette
    val scope = rememberCoroutineScope()
    var taps by remember { mutableStateOf(0) }

    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        androidx.compose.foundation.Image(
            bitmap = ImageBitmap.imageResource(R.drawable.hog_art),
            contentDescription = "Backhog",
            filterQuality = FilterQuality.None,
            modifier = Modifier
                .size(44.dp)
                .clip(CircleShape)
                .clickable {
                    taps += 1
                    if (taps >= 10) {
                        taps = 0
                        scope.launch {
                            com.collinpendleton.backhog.api.apiCall {
                                container.session.api(baseUrl).egg("hog_watcher")
                            }.onSuccess { response ->
                                if (response.unlocked) container.unlocks.unlock(response.achievement)
                            }
                        }
                    }
                },
        )
        Text("Settings", style = MaterialTheme.typography.headlineMedium, color = p.c100)
    }
}

/** Pick a family, then — for the library — which light to read by. Same two tiers as the web picker. */
@Composable
private fun ThemePicker(current: ThemeId, onPick: (ThemeId) -> Unit) {
    val p = Backhog.palette
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        ThemeFamily.entries.forEach { family ->
            val selected = current.family == family
            val shape = MaterialTheme.shapes.medium
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(shape)
                    .background(if (selected) p.fillActive else p.fillHover)
                    .border(1.dp, if (selected) p.hlBright else p.edge, shape)
                    .clickable {
                        if (!selected) onPick(ThemeId.entries.first { it.family == family })
                    }
                    .padding(14.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Swatches(ThemeId.entries.filter { it.family == family })
                    Text(family.label, color = p.c100, style = MaterialTheme.typography.titleSmall)
                }
                Text(family.blurb, color = p.c400, style = MaterialTheme.typography.bodySmall)
            }
        }
        val tier = ThemeId.entries.filter { it.family == current.family }
        if (tier.size > 1) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                tier.forEach { theme ->
                    val selected = theme == current
                    val shape = MaterialTheme.shapes.small
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .clip(shape)
                            .background(if (selected) p.fillActive else p.fillHover)
                            .border(1.dp, if (selected) p.hlBright else p.edge, shape)
                            .clickable { onPick(theme) }
                            .padding(10.dp),
                    ) {
                        Text(theme.label, color = p.c100, style = MaterialTheme.typography.labelLarge)
                        Text(theme.note, color = p.c500, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
}

@Composable
private fun Swatches(themes: List<ThemeId>) {
    Row(horizontalArrangement = Arrangement.spacedBy(3.dp)) {
        themes.forEach { t ->
            Spacer(
                Modifier
                    .size(14.dp)
                    .clip(CircleShape)
                    .background(t.palette.c900)
                    .border(2.dp, t.palette.hlBright, CircleShape),
            )
        }
    }
}

@Composable
private fun ChangePassword(container: AppContainer) {
    val scope = rememberCoroutineScope()
    var current by remember { mutableStateOf("") }
    var next by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var done by remember { mutableStateOf(false) }

    Field(current, { current = it; done = false }, "Current password", password = true)
    Field(next, { next = it; done = false }, "New password", password = true, supportingText = "At least 8 characters")
    Field(confirm, { confirm = it; done = false }, "Confirm new password", password = true, imeAction = ImeAction.Done)
    ErrorText(error)
    if (done) Text("Password changed.", color = Backhog.palette.toneInk(Tones.Played))
    PrimaryButton(
        "Change password",
        busy = busy,
        enabled = current.isNotEmpty() && next.isNotEmpty() && confirm.isNotEmpty(),
        onClick = {
            error = when {
                next.length < 8 -> "Passwords need at least 8 characters."
                next != confirm -> "The new passwords do not match."
                else -> null
            }
            if (error != null) return@PrimaryButton
            val api = container.session.currentApi ?: return@PrimaryButton
            busy = true
            scope.launch {
                apiCall { api.changePassword(ChangePasswordRequest(current, next)) }
                    .onSuccess {
                        current = ""; next = ""; confirm = ""
                        done = true
                    }
                    .onFailure {
                        val apiError = it as ApiError
                        error = apiError.message
                        // The server also answers 401 here from Require when the
                        // session is gone; a real dead session should land on
                        // sign-in, a wrong current password stays inline.
                        if (apiError.isUnauthorized) scope.launch { container.session.restore() }
                    }
                busy = false
            }
        },
    )
}
