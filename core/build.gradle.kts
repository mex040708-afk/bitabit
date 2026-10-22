// Módulo core: librería Java 17 que usan Android y el escritorio. Sin nada de
// interfaz, y con el nivel de lenguaje que Android acepta (Mandamientos 4).
plugins {
    `java-library`
}

// Las apps incluyen este build con includeBuild("../core") (ADR-020): Gradle cambia la
// dependencia io.github.mex040708afk.bitabit:bitabit-core por este proyecto.
group = "io.github.mex040708afk.bitabit"

repositories {
    mavenCentral()
}

dependencies {
    implementation(libs.gson)
    implementation(libs.okhttp)
    testImplementation(libs.junit)
    testImplementation(libs.mockwebserver)
}

tasks.withType<JavaCompile>().configureEach {
    options.release.set(17)
    // Los comentarios y los textos llevan tildes: el resultado no debe depender de la
    // codificación por defecto de cada equipo.
    options.encoding = "UTF-8"
    options.compilerArgs.add("-Xlint:all")
}

tasks.jar {
    manifest {
        // El escritorio es una aplicación modular: sin este nombre, el módulo automático se
        // llamaría como el jar.
        attributes("Automatic-Module-Name" to "io.github.mex040708afk.bitabit.core")
    }
}

tasks.test {
    useJUnit()
}
