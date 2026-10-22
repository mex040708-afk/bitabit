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
    testImplementation(libs.junit)
}

tasks.withType<JavaCompile>().configureEach {
    options.release.set(17)
    options.compilerArgs.add("-Xlint:all")
}

tasks.test {
    useJUnit()
}
