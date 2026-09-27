plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// Versioning comes from CI: BACKHOG_VERSION_NAME is the git tag (android-v1.2.3
// → 1.2.3) or a dev string, BACKHOG_VERSION_CODE the workflow run number, so
// every archived APK installs over the one before it. Local builds get 0.1.0 / 1.
val versionNameEnv: String? = providers.environmentVariable("BACKHOG_VERSION_NAME").orNull
val versionCodeEnv: Int? = providers.environmentVariable("BACKHOG_VERSION_CODE").orNull?.toIntOrNull()

// Release signing, from the environment only — the keystore never lives in the
// repo. CI decodes it from secrets; without them the release build is still
// produced (so R8 is exercised on every PR) but left unsigned.
val keystorePath: String? = providers.environmentVariable("BACKHOG_KEYSTORE_PATH").orNull
val hasReleaseKey = !keystorePath.isNullOrBlank() && file(keystorePath).exists()

android {
    namespace = "com.collinpendleton.backhog"
    compileSdk = 37

    defaultConfig {
        applicationId = "com.collinpendleton.backhog"
        minSdk = 29
        targetSdk = 36
        versionCode = versionCodeEnv ?: 1
        versionName = versionNameEnv ?: "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        if (hasReleaseKey) {
            create("release") {
                storeFile = file(keystorePath!!)
                storePassword = providers.environmentVariable("BACKHOG_KEYSTORE_PASSWORD").orNull
                keyAlias = providers.environmentVariable("BACKHOG_KEY_ALIAS").orNull
                keyPassword = providers.environmentVariable("BACKHOG_KEY_PASSWORD").orNull
            }
        }
    }

    buildTypes {
        debug {
            // Installs beside a release build instead of fighting it for the id.
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            if (hasReleaseKey) signingConfig = signingConfigs.getByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    lint {
        warningsAsErrors = false
        abortOnError = true
        // New library versions land through their own PRs, not as a lint
        // failure on unrelated work.
        disable += setOf("GradleDependency", "NewerVersionAvailable", "AndroidGradlePluginVersion")
        // targetSdk moves when the app has been run against the new release's behaviour changes.
        disable += "OldTargetApi"
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    implementation(libs.androidx.core)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.androidx.datastore.preferences)

    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons)
    implementation(libs.compose.material.icons.extended)
    debugImplementation(libs.compose.ui.tooling)

    implementation(libs.retrofit)
    implementation(libs.retrofit.kotlinx.serialization)
    implementation(libs.okhttp)
    implementation(libs.okhttp.logging)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)

    // Covers, straight from the public cover endpoints.
    implementation(libs.coil.compose)

    // The camera paths: barcode adds and page scanning. ML Kit's bundled
    // variants carry their models in the APK — no Play Services at runtime,
    // and nothing leaves the device but the passage text the matcher gets.
    implementation(libs.androidx.camera.core)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    implementation(libs.mlkit.barcode.scanning)
    implementation(libs.mlkit.text.recognition)

    testImplementation(libs.junit)
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(libs.kotlinx.coroutines.test)
}
