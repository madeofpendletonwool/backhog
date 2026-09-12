package com.collinpendleton.backhog.ui.settings

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Book
import androidx.compose.material.icons.outlined.SportsEsports
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.collinpendleton.backhog.api.ROLE_COPY
import com.collinpendleton.backhog.api.User
import com.collinpendleton.backhog.auth.SessionManager
import com.collinpendleton.backhog.data.Settings
import com.collinpendleton.backhog.data.SettingsStore
import com.collinpendleton.backhog.theme.BackhogTheme
import com.collinpendleton.backhog.theme.LocalBackhogPalette
import com.collinpendleton.backhog.theme.ThemeFamily
import com.collinpendleton.backhog.theme.ThemeSlots
import com.collinpendleton.backhog.theme.TIER_LABEL
import com.collinpendleton.backhog.theme.themesInFamily
import com.collinpendleton.backhog.ui.components.BackhogField
import com.collinpendleton.backhog.ui.components.BackhogPanel
import com.collinpendleton.backhog.ui.components.Banner
import com.collinpendleton.backhog.ui.components.BannerKind
import com.collinpendleton.backhog.ui.components.BodyText
import com.collinpendleton.backhog.ui.components.DangerButton
import com.collinpendleton.backhog.ui.components.KeyValueRow
import com.collinpendleton.backhog.ui.components.PrimaryButton
import com.collinpendleton.backhog.ui.components.SectionTitle
import com.collinpendleton.backhog.ui.components.SoftButton
import com.collinpendleton.backhog.ui.components.formatDate
import androidx.compose.runtime.collectAsState
import kotlinx.coroutines.launch

/**
 * Account, themes, password, server. Mirrors the web SettingsPage (minus the
 * admin surfaces, which stay on the web by scope decision) plus the app's own
 * switch-server affordance.
 */
@Composable
fun SettingsScreen(session: SessionManager, settings: SettingsStore, user: User, arena: String) {
    val scope = rememberCoroutineScope()
    val settingsState by settings.settings.collectAsState(initial = Settings("", true, ThemeSlots()))

    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
    ) {
        Spacer(Modifier.height(20.dp))
        Text(
            "Settings",
            color = LocalBackhogPalette.current.c100,
            fontSize = 22.sp,
            fontWeight = FontWeight.Bold,
            fontFamily = LocalBackhogPalette.current.displayFont,
            modifier = Modifier.padding(horizontal = 8.dp),
        )
        Spacer(Modifier.height(16.dp))

        // --- Account ------------------------------------------------------
        BackhogPanel(Modifier.fillMaxWidth()) {
            SectionTitle("Account")
            Spacer(Modifier.height(10.dp))
            KeyValueRow("Username", user.username)
            KeyValueRow("Email", user.email)
            KeyValueRow("Member since", formatDate(user.createdAt))
            KeyValueRow("Role", ROLE_COPY.getValue(user.role).label)
            Spacer(Modifier.height(6.dp))
            BodyText(ROLE_COPY.getValue(user.role).blurb)
        }
        Spacer(Modifier.height(14.dp))

        // --- Theme --------------------------------------------------------
        BackhogPanel(Modifier.fillMaxWidth()) {
            SectionTitle("Theme")
            Spacer(Modifier.height(4.dp))
            BodyText(
                "Ways to dress the app. Cover art, status colours and your accents stay " +
                    "the same in all of them — only the chrome changes.",
            )
            Spacer(Modifier.height(14.dp))

            LinkedToggle(
                linked = settingsState.themeLinked,
                onChange = { linked ->
                    scope.launch {
                        settings.setLinked(linked)
                        if (linked) settings.collapseSlots(arena)
                    }
                },
            )
            Spacer(Modifier.height(14.dp))

            if (settingsState.themeLinked) {
                ArenaThemePicker(
                    arena = arena,
                    slots = settingsState.slots,
                    onFamily = { family ->
                        scope.launch { settings.setFamily(family, arena) }
                    },
                    onTheme = { theme ->
                        scope.launch { settings.setTheme(theme, arena) }
                    },
                )
            } else {
                Column {
                    listOf("games", "books").forEach { which ->
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            modifier = Modifier.padding(vertical = 6.dp),
                        ) {
                            Icon(
                                if (which == "books") Icons.Outlined.Book else Icons.Outlined.SportsEsports,
                                contentDescription = which,
                                tint = LocalBackhogPalette.current.c300,
                                modifier = Modifier.padding(end = 6.dp).height(16.dp),
                            )
                            Text(
                                if (which == "books") "Books" else "Games",
                                color = LocalBackhogPalette.current.c200,
                                fontSize = 13.sp,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                        ArenaThemePicker(
                            arena = which,
                            slots = settingsState.slots,
                            onFamily = { family ->
                                scope.launch { settings.setFamily(family, which) }
                            },
                            onTheme = { theme ->
                                scope.launch { settings.setTheme(theme, which) }
                            },
                        )
                        if (which == "games") Spacer(Modifier.height(10.dp))
                    }
                }
            }
        }
        Spacer(Modifier.height(14.dp))

        // --- Change password ----------------------------------------------
        ChangePasswordPanel(session)
        Spacer(Modifier.height(14.dp))

        // --- Server -------------------------------------------------------
        BackhogPanel(Modifier.fillMaxWidth()) {
            SectionTitle("Server")
            Spacer(Modifier.height(10.dp))
            KeyValueRow("Address", session.serverUrl)
            Spacer(Modifier.height(10.dp))
            BodyText(
                "Switching servers signs you out here — your library lives on the server, " +
                    "and a different address is a different library.",
            )
            Spacer(Modifier.height(12.dp))
            SoftButton(text = "Switch server", onClick = { session.beginSwitchServer() })
            Spacer(Modifier.height(10.dp))
            DangerButton(text = "Sign out", onClick = { session.logout() })
        }
        Spacer(Modifier.height(32.dp))
    }
}

@Composable
private fun LinkedToggle(linked: Boolean, onChange: (Boolean) -> Unit) {
    val p = LocalBackhogPalette.current
    Row(
        Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Column(Modifier.weight(1f)) {
            Text("Use the same theme in both arenas", color = p.c200, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.height(2.dp))
            Text(
                "Turn this off to keep one theme for games and another for books.",
                color = p.c500,
                fontSize = 12.sp,
                lineHeight = 16.sp,
            )
        }
        Spacer(Modifier.width(12.dp))
        Switch(
            checked = linked,
            onCheckedChange = onChange,
            colors =
                SwitchDefaults.colors(
                    checkedTrackColor = p.hlMid,
                    checkedThumbColor = p.primaryInk,
                    uncheckedTrackColor = p.c700,
                    uncheckedThumbColor = p.c300,
                ),
        )
    }
}

/**
 * One arena's theme choice: pick a family, then a ground when the family has
 * more than one — the web's ArenaTheme, same semantics (remembered theme per
 * family, "same in both arenas" link handled by the caller).
 */
@Composable
private fun ArenaThemePicker(
    arena: String,
    slots: ThemeSlots,
    onFamily: (ThemeFamily) -> Unit,
    onTheme: (BackhogTheme) -> Unit,
) {
    val p = LocalBackhogPalette.current
    val theme = if (arena == "books") slots.books else slots.games
    val family = theme.family

    Column {
        ThemeFamily.entries.forEach { f ->
            val selected = family == f
            Surface(
                shape = RoundedCornerShape(p.radiusPanel),
                color = if (selected) p.fillActive else p.c850,
                border = BorderStroke(1.dp, if (selected) p.edgeStrong else p.edge),
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .padding(vertical = 4.dp)
                        .clickable { onFamily(f) },
            ) {
                Row(Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                    ThemeSwatch(family = f)
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Text(f.label, color = p.c100, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, fontFamily = p.displayFont)
                        Spacer(Modifier.height(2.dp))
                        Text(f.blurb, color = p.c500, fontSize = 12.sp, lineHeight = 16.sp)
                    }
                    if (selected) {
                        Icon(
                            Icons.Outlined.Check,
                            contentDescription = "Selected",
                            tint = p.hlBright,
                        )
                    }
                }
            }
        }

        // A family with more than one theme names its second row itself.
        TIER_LABEL[family]?.let { (title, blurb) ->
            Spacer(Modifier.height(12.dp))
            Text(title, color = p.c200, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.height(2.dp))
            Text(blurb, color = p.c500, fontSize = 12.sp, lineHeight = 16.sp)
            Spacer(Modifier.height(8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                themesInFamily(family).forEach { t ->
                    val selected = t == theme
                    Surface(
                        shape = RoundedCornerShape(p.radiusField),
                        color = if (selected) p.fillActive else p.c850,
                        border = BorderStroke(1.dp, if (selected) p.edgeStrong else p.edge),
                        modifier =
                            Modifier
                                .weight(1f)
                                .clickable { onTheme(t) },
                    ) {
                        Column(
                            Modifier.padding(10.dp).fillMaxWidth(),
                            horizontalAlignment = Alignment.CenterHorizontally,
                        ) {
                            Text(t.label, color = p.c100, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, fontFamily = p.displayFont)
                            Text(t.note, color = p.c500, fontSize = 11.sp)
                        }
                    }
                }
            }
        }
    }
}

/**
 * A miniature of what a family looks like — hand-drawn from literal colours,
 * not the live tokens, so the Midnight card looks like Midnight even when
 * the page around it is wearing Paper. (The web draws its swatches the same
 * way, for the same reason.)
 */
@Composable
private fun ThemeSwatch(family: ThemeFamily) {
    val spec =
        when (family) {
            ThemeFamily.FLAT ->
                Triple(Color(0xFF0C0C10), Color(0xFF7C3AED), Color(0xFFFFFFFF))
            ThemeFamily.LIBRARY -> Triple(Color(0xFF0B0907), Color(0xFFF0A868), Color(0xFF1A1005))
        }
    val (ground, button, ink) = spec
    Surface(
        shape = RoundedCornerShape(8.dp),
        color = ground,
        border = BorderStroke(1.dp, Color(0x14FFFFFF)),
        modifier = Modifier.width(84.dp).height(36.dp),
    ) {
        Row(
            Modifier.padding(horizontal = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Box(
                Modifier
                    .width(28.dp)
                    .height(18.dp)
                    .background(
                        color = if (family == ThemeFamily.FLAT) Color(0xFF17171F) else Color(0xFF130F0B),
                        shape = RoundedCornerShape(4.dp),
                    ),
            )
            Surface(shape = RoundedCornerShape(4.dp), color = button) {
                Text(
                    "Aa",
                    color = ink,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.padding(horizontal = 7.dp, vertical = 2.dp),
                )
            }
        }
    }
}

@Composable
private fun ChangePasswordPanel(session: SessionManager) {
    var current by remember { mutableStateOf("") }
    var next by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<Pair<BannerKind, String>?>(null) }
    var busy by remember { mutableStateOf(false) }

    BackhogPanel(Modifier.fillMaxWidth()) {
        SectionTitle("Change password")
        Spacer(Modifier.height(4.dp))
        BodyText("Changing your password signs you out everywhere except this device.")
        Spacer(Modifier.height(14.dp))
        BackhogField(value = current, onValueChange = { current = it }, label = "Current password", password = true)
        Spacer(Modifier.height(12.dp))
        BackhogField(value = next, onValueChange = { next = it }, label = "New password", placeholder = "At least 8 characters", password = true)
        Spacer(Modifier.height(14.dp))
        status?.let { (kind, message) -> Banner(kind, message) ; Spacer(Modifier.height(12.dp)) }
        PrimaryButton(
            text = "Update password",
            loading = busy,
            onClick = {
                if (next.length < 8) {
                    status = BannerKind.ERROR to "New password must be at least 8 characters."
                    return@PrimaryButton
                }
                busy = true
                status = null
                session.changePassword(current, next) { result ->
                    busy = false
                    result
                        .onSuccess {
                            current = ""
                            next = ""
                            status = BannerKind.OK to "Password updated. Any other devices have been signed out."
                        }
                        .onFailure { e -> status = BannerKind.ERROR to (e.message ?: "Could not update the password.") }
                }
            },
        )
    }
}
