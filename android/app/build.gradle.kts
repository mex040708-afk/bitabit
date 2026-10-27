plugins {
    alias(libs.plugins.android.application)
}

android {
    namespace = "io.github.mex040708afk.bitabit"
    compileSdk {
        version = release(36) {
            minorApiLevel = 1
        }
    }

    defaultConfig {
        applicationId = "io.github.mex040708afk.bitabit"
        // El decodificador de FLAC del sistema existe desde Android 8.1.
        minSdk = 27
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
    }

    buildTypes {
        release {
            optimization {
                enable = false
                keepRules {
                    files.add(file("proguard-rules.pro"))
                }
            }
        }
    }
    compileOptions {
        // El nivel del proyecto para todo el Java que corre en Android (Mandamientos, 4).
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation(libs.bitabit.core)
    implementation(libs.appcompat)
    implementation(libs.fragment)
    implementation(libs.media3.exoplayer)
    testImplementation(libs.junit)
}
