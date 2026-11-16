plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "systems.neolabs.neocloud"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_11
        targetCompatibility = JavaVersion.VERSION_11
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_11.toString()
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "systems.neolabs.neocloud"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    // An update only installs over an installed app when both carry the same
    // signature. A runner's own debug key is new on every CI run, so releases
    // are signed with a key kept in the repository (like NeoAgent's), which
    // makes every APK from CI update the one before it. Not a secret: it only
    // keeps the signature stable.
    signingConfigs {
        create("release") {
            storeFile = file("${rootProject.projectDir}/ci-release.keystore")
            storePassword = "neocloud-ci"
            keyAlias = "neocloud-ci"
            keyPassword = "neocloud-ci"
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.getByName("release")
        }
    }
}

flutter {
    source = "../.."
}
