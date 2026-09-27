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
./gradlew lintDebug testDebugUnitTest
```

The debug build installs as `com.collinpendleton.backhog.debug`, next to a
release build. It may also reach a dev server on the host over plain HTTP at
`http://10.0.2.2:<port>` from the emulator (`src/debug`). Release builds are
HTTPS-only.

## CI and releases

`.github/workflows/android.yml` runs on every PR into `main`, on pushes to
`main`, and on `android-v*` tags. Each run does lint, unit tests, the debug
build and the R8 release build. It archives:

- `backhog-android-debug-<version>`: the debug APK, always.
- `backhog-android-release-<version>`: the signed release APK, once signing
  secrets are configured.

Pushing a tag like `android-v1.0.0` publishes the signed APK to a GitHub
Release. `versionName` comes from the tag (or `<last tag>-pr<N>.<sha>` for dev
builds). `versionCode` is the workflow run number, so every archived APK
installs over the previous one.

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

## Layout

```
app/src/main/java/com/collinpendleton/backhog/
  api/        Retrofit surface, models (from web/src/lib/types.ts), cookie jar, errors
  data/       DataStore preferences (themes, arena, remembered library filters), server-URL parsing
  session/    SessionManager: server → sign-in → signed-in state machine
  ui/theme/   Midnight + Library (Paper / Hearth), ported from web/src/themes
  ui/auth/    server config, sign in, register (open, setup, invite)
  ui/shell/   bottom nav, one back stack per arena
  ui/games/   library (grid/table, facets, remembered filters), add-game search sheet,
              game detail (dossier + status/rating/notes/platform/sessions), play queue
              (drag reorder, quick moves, mark finished)
  ui/settings/
```
