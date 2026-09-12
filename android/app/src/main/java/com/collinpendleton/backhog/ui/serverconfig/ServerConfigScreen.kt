package com.collinpendleton.backhog.ui.serverconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.api.HealthStatus
import com.collinpendleton.backhog.api.Status
import com.collinpendleton.backhog.auth.SessionManager
import com.collinpendleton.backhog.data.Settings
import com.collinpendleton.backhog.data.SettingsStore
import com.collinpendleton.backhog.theme.LocalBackhogPalette
import com.collinpendleton.backhog.theme.ThemeSlots
import com.collinpendleton.backhog.theme.statusInk
import com.collinpendleton.backhog.ui.components.BackhogField
import com.collinpendleton.backhog.ui.components.BackhogPanel
import com.collinpendleton.backhog.ui.components.Banner
import com.collinpendleton.backhog.ui.components.BannerKind
import com.collinpendleton.backhog.ui.components.BodyText
import com.collinpendleton.backhog.ui.components.PrimaryButton
import com.collinpendleton.backhog.ui.components.SoftButton
import androidx.compose.runtime.collectAsState
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * First-launch server config (and, later, switch-server): point the app at a
 * Backhog, prove it answers with /api/healthz, save. A pasted invite link
 * (`https://hog.example.com/register?invite=…`) fills both the address and
 * the token the register screen will spend.
 */
@Composable
fun ServerConfigScreen(session: SessionManager, api: ApiClient, settings: SettingsStore) {
    val p = LocalBackhogPalette.current
    val settingsState by settings.settings.collectAsState(initial = Settings("", true, ThemeSlots()))
    val existingUrl = settingsState.baseUrl

    var input by remember(existingUrl) { mutableStateOf(existingUrl.removePrefix("https://").trimEnd('/')) }
    var checking by remember { mutableStateOf(false) }
    // The last green check: the URL it validated, and what it learned.
    var verified by remember { mutableStateOf<Pair<String, HealthStatus>?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    // Bumped to run a check; LaunchedEffect does the blocking work off-main.
    var checkTicket by remember { mutableIntStateOf(0) }

    val normalized = ApiClient.normalizeServerInput(input)

    LaunchedEffect(checkTicket) {
        if (checkTicket == 0) return@LaunchedEffect
        val (base, _) = normalized
        if (base.isBlank()) {
            failure = "Type your server's address first."
            verified = null
            return@LaunchedEffect
        }
        checking = true
        failure = null
        val result = withContext(Dispatchers.IO) { api.healthCheck(base) }
        checking = false
        result
            .onSuccess { health -> verified = base to health }
            .onFailure { e ->
                verified = null
                failure = e.message ?: "Could not reach that address."
            }
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .imePadding()
            .padding(horizontal = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.height(64.dp))
        Text("🐗", fontSize = 44.sp)
        Spacer(Modifier.height(8.dp))
        Text(
            "Backhog",
            color = p.c100,
            fontSize = 26.sp,
            fontWeight = FontWeight.Bold,
            fontFamily = p.displayFont,
        )
        Spacer(Modifier.height(4.dp))
        BodyText("Point the app at your server.")
        Spacer(Modifier.height(28.dp))

        BackhogPanel(Modifier.fillMaxWidth()) {
            Text(
                if (existingUrl.isBlank()) "Where does your Backhog live?" else "Switch server",
                color = p.c100,
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                fontFamily = p.displayFont,
            )
            Spacer(Modifier.height(4.dp))
            BodyText(
                "Production servers sit behind HTTPS (Let's Encrypt) — the app talks to nothing in the clear.",
            )
            Spacer(Modifier.height(16.dp))

            BackhogField(
                value = input,
                onValueChange = {
                    input = it
                    verified = null
                },
                label = "Server address",
                placeholder = "hog.example.com",
            )
            Spacer(Modifier.height(14.dp))

            if (failure != null) {
                Banner(BannerKind.ERROR, failure!!)
                Spacer(Modifier.height(12.dp))
            }

            verified?.let { (base, health) ->
                Surface(
                    shape = RoundedCornerShape(p.radiusField),
                    color = p.c850,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Column(Modifier.padding(12.dp)) {
                        Text(
                            "Connected — ${health.status}",
                            color = statusInk(Status.PLAYED, p),
                            fontSize = 14.sp,
                            fontWeight = FontWeight.Medium,
                        )
                        Spacer(Modifier.height(6.dp))
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Tag(text = if (health.metadata) "Game metadata on" else "Game metadata off")
                            Tag(text = if (health.steam) "Steam import on" else "Steam import off")
                        }
                        if (normalized.second != null && normalized.first == base) {
                            Spacer(Modifier.height(8.dp))
                            Text(
                                "Sign-up link detected — continue after connecting to create your account.",
                                color = p.c300,
                                fontSize = 12.sp,
                                lineHeight = 16.sp,
                            )
                        }
                    }
                }
                Spacer(Modifier.height(12.dp))
            }

            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                SoftButton(
                    text = if (checking) "Checking…" else "Check connection",
                    onClick = { checkTicket++ },
                    enabled = !checking,
                )
                PrimaryButton(
                    text = "Save and continue",
                    onClick = { session.adoptServer(normalized.first, normalized.second) },
                    enabled = verified != null && verified?.first == normalized.first && !checking,
                )
            }

            if (existingUrl.isNotBlank()) {
                Spacer(Modifier.height(10.dp))
                SoftButton(text = "Cancel", onClick = { session.cancelSwitchServer() })
            }
        }
        Spacer(Modifier.height(32.dp))
    }
}

@Composable
private fun Tag(text: String) {
    val p = LocalBackhogPalette.current
    Surface(
        shape = RoundedCornerShape(999.dp),
        color = p.fillActive,
        contentColor = p.c300,
    ) {
        Text(text, Modifier.padding(horizontal = 10.dp, vertical = 4.dp), fontSize = 11.sp)
    }
}
