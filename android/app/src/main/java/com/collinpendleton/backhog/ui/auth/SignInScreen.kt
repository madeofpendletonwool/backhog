package com.collinpendleton.backhog.ui.auth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.collinpendleton.backhog.session.SessionManager
import com.collinpendleton.backhog.session.SessionState
import com.collinpendleton.backhog.ui.components.AuthShell
import com.collinpendleton.backhog.ui.components.ErrorText
import com.collinpendleton.backhog.ui.components.Field
import com.collinpendleton.backhog.ui.components.PrimaryButton
import com.collinpendleton.backhog.ui.components.ToneChip
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import kotlinx.coroutines.launch

@Composable
fun SignInScreen(session: SessionManager, state: SessionState.SignedOut) {
    // Keyed by invite too: the activity keeps ViewModels, and a second visit to
    // the same server with a freshly pasted invite must not reuse the old one.
    val vm: SignInViewModel = viewModel(key = "${state.baseUrl}|${state.invite}") {
        SignInViewModel(session, state.baseUrl, state.invite)
    }
    // Registration can be opened or closed between visits (sign out, session expiry): re-read it.
    LaunchedEffect(state) { vm.loadConfig() }
    val ui by vm.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val p = Backhog.palette

    var email by rememberSaveable { mutableStateOf("") }
    var username by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    var inviteInput by rememberSaveable { mutableStateOf("") }
    var prefilled by rememberSaveable { mutableStateOf(false) }

    // The invite's email pre-fills the form once, and stays editable.
    LaunchedEffect(ui.config?.invite?.email) {
        val invited = ui.config?.invite?.email
        if (!prefilled && !invited.isNullOrBlank() && email.isBlank()) {
            email = invited
            prefilled = true
        }
    }

    val registering = ui.registering
    val invite = ui.config?.invite
    val title = when {
        !registering -> "Sign in"
        ui.config?.setup == true -> "Create the first account"
        invite != null -> "${invite.invitedBy.ifBlank { "Someone" }} invited you"
        else -> "Create an account"
    }

    AuthShell(title = title, subtitle = state.baseUrl) {
        if (state.expired) {
            Text("Your session ended. Sign in again.", color = p.c300, style = MaterialTheme.typography.bodyMedium)
        }
        if (ui.configError != null) {
            ErrorText("Could not read the server's sign-in settings: ${ui.configError}")
            TextButton(onClick = vm::loadConfig) { Text("Retry") }
        }

        if (registering) {
            when {
                ui.config?.setup == true -> Text(
                    "No accounts exist yet. The first one becomes the administrator.",
                    color = p.c300,
                    style = MaterialTheme.typography.bodyMedium,
                )
                invite != null -> Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    ToneChip("Joining as ${invite.role.label.lowercase()}", Tones.Playing)
                    Text(invite.role.blurb, color = p.c400, style = MaterialTheme.typography.bodySmall)
                }
            }
            if (ui.config != null && !ui.canRegister) {
                Text(
                    "This server is invite-only. Paste the invite link you were sent.",
                    color = p.c300,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            if (ui.staleInvite) {
                ErrorText("That invite is unknown, already used, revoked or expired. Ask for a new link.")
            }
            Field(email, { email = it }, "Email", keyboardType = KeyboardType.Email)
            Field(username, { username = it }, "Username")
            Field(password, { password = it }, "Password", password = true, imeAction = ImeAction.Done, supportingText = "At least 8 characters")
            ErrorText(ui.error)
            PrimaryButton(
                "Create account",
                onClick = { vm.register(email, username, password) },
                busy = ui.busy,
                enabled = email.isNotBlank() && username.isNotBlank() && password.isNotBlank() && ui.canRegister,
            )
            if (invite == null && ui.config?.setup != true) {
                Field(inviteInput, { inviteInput = it }, "Invite link or code (optional)", keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done)
                if (inviteInput.isNotBlank()) {
                    TextButton(onClick = { vm.useInvite(inviteInput) }) { Text("Use invite") }
                }
            }
            if (ui.config?.setup != true) {
                TextButton(onClick = { vm.setRegistering(false) }) { Text("Already have an account? Sign in") }
            }
        } else {
            Field(email, { email = it }, "Email", keyboardType = KeyboardType.Email)
            Field(password, { password = it }, "Password", password = true, imeAction = ImeAction.Done)
            ErrorText(ui.error)
            PrimaryButton(
                "Sign in",
                onClick = { vm.login(email, password) },
                busy = ui.busy,
                enabled = email.isNotBlank() && password.isNotBlank(),
            )
            if (ui.canRegister) {
                TextButton(onClick = { vm.setRegistering(true) }) { Text("Create an account") }
            } else if (ui.config != null) {
                // Invite-only server: registration is closed, but a link still gets you in.
                TextButton(onClick = { vm.setRegistering(true) }) { Text("Have an invite?") }
            }
        }

        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = { scope.launch { session.switchServer() } }) { Text("Use a different server") }
        }
    }
}

@Composable
fun UnreachableScreen(session: SessionManager, state: SessionState.Unreachable) {
    val scope = rememberCoroutineScope()
    AuthShell(title = "Can't reach your server", subtitle = state.baseUrl) {
        ErrorText(state.message)
        PrimaryButton("Try again", onClick = { scope.launch { session.restore() } })
        TextButton(onClick = { scope.launch { session.switchServer() } }) { Text("Use a different server") }
    }
}
