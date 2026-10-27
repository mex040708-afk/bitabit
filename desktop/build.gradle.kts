plugins {
    application
}

version = "0.1.0"

tasks.withType<JavaCompile>().configureEach {
    // JavaFX 21 funciona con el JDK 21 de Android Studio (ADR-013).
    options.release = 21
    options.encoding = "UTF-8"
    // core es un módulo automático a propósito (ADR-020): su aviso no dice nada nuevo.
    options.compilerArgs.add("-Xlint:all,-requires-automatic")
}

dependencies {
    // Los POM de OpenJFX eligen la plataforma con perfiles de Maven que Gradle
    // no activa: se piden los jar de Windows uno a uno y sin dependencias.
    for (javafx in listOf(libs.javafx.base, libs.javafx.graphics, libs.javafx.controls)) {
        implementation(variantOf(javafx) { classifier("win") }) {
            isTransitive = false
        }
    }
    implementation(libs.bitabit.core)
    implementation(libs.gson)
    testImplementation(libs.junit)
}

application {
    mainModule = "io.github.mex040708afk.bitabit.desktop"
    mainClass = "io.github.mex040708afk.bitabit.desktop.App"
    // OkHttp y Okio, que trae core, son módulos automáticos y no declaran que leen
    // kotlin.stdlib, que sí es un módulo con nombre: sin esto no entra en el grafo y la
    // primera llamada a la red falla con NoClassDefFoundError de kotlin/….
    applicationDefaultJvmArgs = listOf("--add-modules=kotlin.stdlib")
}

tasks.named<JavaExec>("run") {
    // Dar jvmArgs aquí deja sin efecto los de application{}: se vuelven a poner.
    jvmArgs(application.applicationDefaultJvmArgs)
    // Gradle lee la salida de la app como UTF-8, y Java en Windows la
    // escribiría en cp1252: se rompían las tildes.
    jvmArgs("-Dstdout.encoding=UTF-8", "-Dstderr.encoding=UTF-8")
}
