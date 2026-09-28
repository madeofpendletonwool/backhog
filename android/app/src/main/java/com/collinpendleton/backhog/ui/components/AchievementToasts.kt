package com.collinpendleton.backhog.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.EmojiEvents
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.Format
import com.collinpendleton.backhog.achievements.UnlockBus
import com.collinpendleton.backhog.api.AchievementStatus
import com.collinpendleton.backhog.ui.games.CoverImage
import com.collinpendleton.backhog.ui.theme.Backhog
import com.collinpendleton.backhog.ui.theme.Tones
import kotlinx.coroutines.delay

/** How long a toast stays up — the web's 6.5s. */
private const val TOAST_MS = 6_500L

/**
 * Mounts the toast stack at the bottom-centre of the screen, above every
 * screen but below nothing — the position the web's fixed stack sits in.
 * Tapping a toast dismisses it.
 */
@Composable
fun AchievementToastsHost(container: AppContainer, baseUrl: String) {
    Box(
        Modifier
            .fillMaxWidth()
            .padding(bottom = 8.dp),
        contentAlignment = Alignment.BottomCenter,
    ) {
        AchievementToasts(container.unlocks, baseUrl)
    }
}

/**
 * The achievement toast stack, mounted once in the Shell — the port of the
 * web's AchievementToasts. Mutations push unlocks onto the bus; this
 * celebrates: icon, title, description, and the cover of the game that tipped
 * it over.
 */
@Composable
fun AchievementToasts(bus: UnlockBus, baseUrl: String) {
    val toasts by bus.toasts.collectAsStateWithLifecycle()

    // Expire oldest-first: while any toast is showing, one leaves every interval.
    if (toasts.isNotEmpty()) {
        LaunchedEffect(toasts.size) {
            delay(TOAST_MS)
            bus.dismissOldest()
        }
    }

    Column(
        modifier = Modifier.padding(horizontal = 16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        toasts.takeLast(3).forEach { toast ->
            UnlockToastCard(toast, baseUrl)
        }
    }
}

@Composable
private fun UnlockToastCard(toast: AchievementStatus, baseUrl: String) {
    val p = Backhog.palette
    val shape = MaterialTheme.shapes.large
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(p.c900)
            .border(1.dp, Tones.forTier(toast.tier).copy(alpha = 0.4f), shape)
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier
                .size(44.dp)
                .clip(MaterialTheme.shapes.medium)
                .background(Tones.forTier(toast.tier).copy(alpha = 0.14f)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(achievementIcon(toast.icon), contentDescription = null, tint = Tones.forTier(toast.tier))
        }
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Icon(
                    if (toast.egg) Icons.Filled.Shuffle else Icons.Filled.EmojiEvents,
                    contentDescription = null,
                    tint = p.hlBright,
                    modifier = Modifier.size(12.dp),
                )
                Text(
                    "Achievement unlocked",
                    style = MaterialTheme.typography.labelSmall,
                    fontWeight = FontWeight.Bold,
                    color = p.hlBright,
                )
            }
            Text(
                toast.title,
                style = MaterialTheme.typography.titleSmall,
                color = p.cMax,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                toast.description,
                style = MaterialTheme.typography.bodySmall,
                color = p.c400,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        toast.entry?.let { entry ->
            CoverImage(entry, baseUrl, Modifier.size(width = 36.dp, height = 48.dp).clip(MaterialTheme.shapes.small))
        }
    }
}

/** One gallery card: tier-toned when unlocked, gray and locked otherwise. */
@Composable
fun AchievementCard(achievement: AchievementStatus, baseUrl: String, onOpenEntry: (String) -> Unit) {
    val p = Backhog.palette
    val unlocked = achievement.unlockedAt != null
    val tone = Tones.forTier(achievement.tier)

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.large)
            .background(p.c900)
            .border(
                1.dp,
                if (unlocked) tone.copy(alpha = 0.35f) else p.edgeStrong,
                MaterialTheme.shapes.large,
            )
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                Modifier
                    .size(40.dp)
                    .clip(MaterialTheme.shapes.medium)
                    .background(if (unlocked) tone.copy(alpha = 0.14f) else p.c850),
                contentAlignment = Alignment.Center,
            ) {
                if (unlocked) {
                    Icon(achievementIcon(achievement.icon), contentDescription = null, tint = tone)
                } else {
                    Icon(Icons.Filled.Lock, contentDescription = "Locked", tint = p.c600)
                }
            }
            Spacer(Modifier.weight(1f))
            Text(
                if (unlocked) Format.date(achievement.unlockedAt) else "",
                style = MaterialTheme.typography.labelSmall,
                color = p.c500,
            )
        }

        Text(
            achievement.title,
            style = MaterialTheme.typography.titleSmall,
            color = if (unlocked) p.cMax else p.c300,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            achievement.description,
            style = MaterialTheme.typography.bodySmall,
            color = p.c400,
        )

        val entry = achievement.entry
        if (unlocked && entry != null) {
            Row(
                Modifier
                    .clip(MaterialTheme.shapes.small)
                    .clickable { onOpenEntry(entry.id) }
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                CoverImage(entry, baseUrl, Modifier.size(width = 28.dp, height = 38.dp).clip(MaterialTheme.shapes.extraSmall))
                Text(
                    entry.title,
                    style = MaterialTheme.typography.labelMedium,
                    color = p.c300,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}
