pluginManagement {
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "bitabit-android"

// core se compila desde el código del monorepo (ADR-020): la app siempre usa el core
// de la misma revisión, sin publicarlo en ningún repositorio.
includeBuild("../core") {
    dependencySubstitution {
        substitute(module("io.github.mex040708afk.bitabit:core")).using(project(":"))
    }
}
include(":app")
