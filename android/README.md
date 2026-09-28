# Backhog for Android

A native client for Backhog: Kotlin, Jetpack Compose, single activity. It is
sideloaded, never shipped through Google Play. CI builds the APK and archives it.

The server owns every piece of data. The app keeps only the server address, the
session cookie, and appearance choices.

## Build

JDK 17+ and the Android SDK (compileSdk 37) are required.

```sh
cd android
./gradlew assembleDebug          # app/build/outputs/apk/debug/app-debug.apk
./gradlew assembleRelease        # R8-minified; signed only when keystore env is set
./gradlew lintDebug testDebugUnitTest
```

The debug build installs as `com.collinpendleton.backhog.debug`, next to a
release build. It may also reach a dev server on the host over plain HTTP at
`http://10.0.2.2:<port>` from the emulator (`src/debug`). Release builds are
HTTPS-only.

## Install

The release APK installs on any device with Android 10+ (minSdk 29):

```sh
adb install -r backhog-<version>.apk
```

Or copy the APK to the device and open it from a file manager; Android prompts
to allow installs from that app ("unknown sources", since the app is not from
Play). Sideloaded apps update in place as long as each build is signed with the
same key and its `versionCode` is higher — CI derives `versionCode` from the
workflow run number, so any newer archived APK installs over the previous one
and the session, themes, and remembered filters survive the upgrade.

## Server config

First launch asks for the server base URL and signs in. Servers are expected to
be HTTPS (Let's Encrypt); there is no cleartext escape hatch in release builds.
The URL is validated against `GET /api/healthz` before saving. Registration
(open or invite) follows whatever the server's auth config allows, exactly like
the web login page. The address can be changed later in Settings → switch
server, which clears the session.

## CI and releases

`.github/workflows/android.yml` runs on every PR into `main`, on pushes to
`main`, and on `android-v*` tags. Each run does lint, unit tests, the debug
build and the R8 release build. It archives:

- `backhog-android-debug-<version>`: the debug APK, always.
- `backhog-android-release-<version>`: the signed release APK, once signing
  secrets are configured.

Pushing a tag like `android-v1.0.0` publishes the signed APK to a GitHub
Release (the tag job refuses to publish an unsigned build). `versionName` comes
from the tag (`android-v1.2.3` → `1.2.3`, or `<last tag>-pr<N>.<sha>` for dev
builds). `versionCode` is the workflow run number, so every archived APK
installs over the previous one. The run summary lists each APK's size and
SHA-256.

The release APK is a universal build (~70 MB): R8 brings the code down to a
~7 MB dex, and the bulk is ML Kit's bundled on-device OCR/barcode models and
their native libraries, shipped for all four ABIs so one APK installs on any
device with no Play Services dependency.

### Release signing

Generate the key once and keep a copy somewhere safe. If the key is lost,
installed copies can no longer be updated in place.

```sh
keytool -genkeypair -v -keystore backhog-release.jks -alias backhog \
  -keyalg RSA -keysize 4096 -validity 36500
base64 -i backhog-release.jks | gh secret set ANDROID_KEYSTORE_BASE64
gh secret set ANDROID_KEYSTORE_PASSWORD
gh secret set ANDROID_KEY_ALIAS --body backhog
gh secret set ANDROID_KEY_PASSWORD      # omit if it matches the store password
```

To sign locally, set `BACKHOG_KEYSTORE_PATH`, `BACKHOG_KEYSTORE_PASSWORD`,
`BACKHOG_KEY_ALIAS` and `BACKHOG_KEY_PASSWORD`.

## Android Auto — and why not CarPlay

In-car playback is Android Auto. "CarPlay" in the original planning means the
car requirement, but CarPlay itself is Apple's, iOS-only, and cannot be served
by an Android APK; the Android equivalent is Android Auto media playback, which
works for sideloaded apps with no store involvement. Backhog integrates the
way audio apps do — a Media3 `MediaLibraryService` (`player/PlaybackService`)
the head unit binds to; there are no car app templates and no Play review. To
try it without a car, run the Desktop Head Unit (`adh` from Android SDK
extras) against a device or emulator with the app playing.

## Layout

```
app/src/main/java/com/collinpendleton/backhog/
  api/        Retrofit surface, models (from web/src/lib/types.ts), cookie jar, errors
  data/       DataStore preferences (themes, arena, remembered filters, playback speed),
              server-URL parsing
  session/    SessionManager: server → sign-in → signed-in state machine
  achievements/  Unlock bus: server-reported unlocks → toasts
  books/      Page scanning (CameraX + ML Kit OCR, prose extraction), reader pagination
              engine, position formatting
  player/     PlaybackService (Media3 MediaLibraryService: notification, Auto, one tape),
              AudioTimeline (global-time ↔ track/offset arithmetic), PlayerConnection
  ui/theme/   Midnight + Library (Paper / Hearth), ported from web/src/themes
  ui/auth/    server config, sign in, register (open, setup, invite)
  ui/shell/   bottom nav, one back stack per arena
  ui/games/   library (grid/table, facets, remembered filters), add-game search,
              game detail (dossier, status/rating/notes/platform/sessions), play queue
              (drag, jumps, nudges, mark finished), lists + smart-list builder, series
              (orders, backfill), projects, dashboard + debt, tonight picks + random
              roll, Steam import, achievements gallery + season
  ui/books/   book library + reading dashboard, add book (search / ISBN / barcode scan),
              book detail (dossier, position views, copies, shares), page scanning
              sheet, search-in-book, EPUB reader
  ui/media/   member-only file layer: scanner, file browser, candidate review, attach/
              detach, primary promotion, audio editions, alignment
  ui/player/  full-screen player + mini-player
  ui/settings/  account, change password, sessions, shared-with-me shelf, themes
```

## Release QA

`QA-CHECKLIST.md` is the release gate: the feature-parity walkthrough (both
arenas, reader, player + Desktop Head Unit, scanning, lending, eggs, reader
role, themes, degraded modes, install-over-update) to run on a real device
against production before tagging.

## Deliberately not in v1

- **Arcade theme family** — the web's pixel-art theme; deferred to a later pass.
- **Admin panel** — users, invites, and server settings stay on the web client.
- **Konami code egg** — not ported; night owl, hog watcher, and queue shuffler ship.
