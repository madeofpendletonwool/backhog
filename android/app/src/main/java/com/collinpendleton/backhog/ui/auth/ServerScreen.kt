package com.collinpendleton.backhog.ui.auth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material3.Text
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.collinpendleton.backhog.BuildConfig
import com.collinpendleton.backhog.api.ApiError
import com.collinpendleton.backhog.api.Health
import com.collinpendleton.backhog.api.apiCall
import com.collinpendleton.backhog.data.ServerInput
import com.collinpendleton.backhog.data.ServerUrl
import com.collinpendleton.backhog.session.SessionManager
import com.collinpendleton.backhog.ui.components.AuthShell
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.Field
import com.collinpendleton.backhog.ui.components.PrimaryButton
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import kotlinx.coroutines.launch

/**
 * First launch: where is your Backhog? The address is health-checked before it
 * is saved, so a typo fails here and not as a baffling sign-in error. A pasted
 * invite link works too — it carries both the server and the invite.
 */
@Composable
fun ServerScreen(session: SessionManager) {
    val scope = rememberCoroutineScope()
    var input by rememberSaveable { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var checked by remember { mutableStateOf<Pair<ServerInput, Health>?>(null) }

    fun check() {
        val parsed = ServerUrl.parse(input)
        when {
            parsed == null -> error = "That does not look like a server address."
            // Debug builds may reach a dev server on the host over HTTP (see src/debug).
            parsed.baseUrl.startsWith("http://") && !BuildConfig.DEBUG ->
                error = "Backhog for Android connects over HTTPS only. Use your server's https:// address."
            else -> {
                busy = true
                error = null
                scope.launch {
                    apiCall { session.api(parsed.baseUrl).health() }
                        .onSuccess { health ->
                            if (health.status == "ok") checked = parsed to health
                            else error = "The server answered but is not healthy: ${health.status}"
                        }
                        .onFailure { e ->
                            val err = e as ApiError
                            error = when (err.status) {
                                0 -> err.message
                                404 -> "Nothing answered at ${parsed.baseUrl}/api/healthz. Is this a Backhog server?"
                                else -> "The server said: ${err.message}"
                            }
                        }
                    busy = false
                }
            }
        }
    }

    AuthShell(
        title = "Connect to your server",
        subtitle = "The address you open Backhog at in a browser. An invite link works too.",
    ) {
        Field(
            value = input,
            onValueChange = {
                input = it
                checked = null
                error = null
            },
            label = "Server address",
            keyboardType = KeyboardType.Uri,
            imeAction = ImeAction.Go,
            supportingText = "e.g. backhog.example.com",
        )
        ErrorText(error)

        val ok = checked
        if (ok == null) {
            PrimaryButton("Check server", onClick = ::check, busy = busy, enabled = input.isNotBlank())
        } else {
            val (server, health) = ok
            Text(server.baseUrl, style = MaterialTheme.typography.bodyMedium, color = Backhog.palette.c300)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                ToneChip("Server online", Tones.Played)
                ToneChip(if (health.metadata) "Game search on" else "Game search off", if (health.metadata) Tones.Played else Tones.Backlog)
                ToneChip(if (health.steam) "Steam import on" else "Steam import off", if (health.steam) Tones.Played else Tones.Backlog)
                if (server.invite != null) ToneChip("Invite link", Tones.Playing)
            }
            PrimaryButton(
                "Continue",
                onClick = { scope.launch { session.configureServer(server.baseUrl, server.invite) } },
            )
        }
    }
}
