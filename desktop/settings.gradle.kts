// core vive al lado, en el mismo repositorio (ADR-020): Gradle cambia la
// coordenada io.github.mex040708afk.bitabit:bitabit-core por ese proyecto.
includeBuild("../core")

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        mavenCentral()
    }
}

rootProject.name = "bitabit-desktop"
