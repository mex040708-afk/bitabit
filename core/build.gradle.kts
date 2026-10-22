// Módulo core: librería Java 17 que usan Android y el escritorio. Sin nada de
// interfaz, y con el nivel de lenguaje que Android acepta (Mandamientos 4).
plugins {
    `java-library`
}

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

tasks.test {
    useJUnit()
}
