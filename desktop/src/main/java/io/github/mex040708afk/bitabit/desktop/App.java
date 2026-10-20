package io.github.mex040708afk.bitabit.desktop;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import javafx.application.Application;
import javafx.application.Platform;
import javafx.geometry.Insets;
import javafx.scene.Scene;
import javafx.scene.control.Button;
import javafx.scene.control.CheckBox;
import javafx.scene.control.Label;
import javafx.scene.control.TextField;
import javafx.scene.layout.HBox;
import javafx.scene.layout.VBox;
import javafx.stage.Stage;

/**
 * Prototipo de escritorio de la fase 1 (ADR-013): pega un enlace de «bitabit
 * link» y lo escucha o mide su MD5.
 *
 * <p>Para las pruebas automáticas se puede arrancar con
 * {@code --enlace-archivo=RUTA} (el enlace, en la primera línea de un archivo,
 * para que no salga en la lista de procesos), {@code --accion=medir|escuchar},
 * {@code --exclusivo=no} y {@code --salir}, que cierra al terminar con el código
 * 0 si todo cuadra y 1 si no. El resultado se escribe también en la salida
 * estándar, sin la firma del enlace.
 */
public final class App extends Application {

    private final String mpv = System.getenv().getOrDefault("BITABIT_MPV", "mpv");
    private final HttpClient http = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(5))
            .followRedirects(HttpClient.Redirect.NEVER)
            .build();
    private final ExecutorService worker = Executors.newSingleThreadExecutor(task -> {
        Thread t = new Thread(task, "trabajo");
        t.setDaemon(true);
        return t;
    });

    private final TextField link = new TextField();
    private final CheckBox exclusive = new CheckBox("Modo exclusivo (WASAPI)");
    private final Button listen = new Button("Escuchar");
    private final Button measure = new Button("Medir");
    private final Button stop = new Button("Parar");
    private final Label status = new Label("Pega un enlace de «bitabit link».");
    private final Label formats = new Label();
    private final TextField md5 = new TextField();
    private final Label verdict = new Label();

    /** Solo se toca desde el hilo de JavaFX. */
    private Listen listening;
    private boolean exitWhenDone;

    public static void main(String[] args) {
        launch(args);
    }

    @Override
    public void start(Stage stage) {
        link.setPromptText("http://localhost:8080/media/1.flac?q=orig&exp=…");
        exclusive.setSelected(true);
        md5.setEditable(false);
        md5.setPromptText("MD5 de lo que suena");
        for (Label label : List.of(status, formats, verdict)) {
            label.setWrapText(true);
        }
        stop.setDisable(true);
        listen.setOnAction(e -> startListening());
        measure.setOnAction(e -> startMeasuring());
        stop.setOnAction(e -> stopListening());

        VBox root = new VBox(10,
                new Label("Enlace"), link,
                new HBox(10, exclusive, listen, measure, stop),
                status, formats, md5, verdict);
        root.setPadding(new Insets(16));
        stage.setTitle("Bitabit");
        stage.setScene(new Scene(root, 640, 320));
        stage.show();

        autostart();
    }

    @Override
    public void stop() {
        stopListening();
        worker.shutdownNow();
        http.close();
    }

    private void autostart() {
        Map<String, String> named = getParameters().getNamed();
        exitWhenDone = getParameters().getRaw().contains("--salir");
        exclusive.setSelected(!"no".equals(named.get("exclusivo")));
        String file = named.get("enlace-archivo");
        if (file == null) {
            return;
        }
        try {
            List<String> lines = Files.readAllLines(Path.of(file));
            link.setText(lines.isEmpty() ? "" : lines.get(0));
        } catch (IOException e) {
            fail("No se pudo leer el enlace: " + e.getMessage());
            return;
        }
        switch (named.getOrDefault("accion", "")) {
            case "medir" -> startMeasuring();
            case "escuchar" -> startListening();
            default -> {
                // Sin acción: el enlace queda pegado y se elige a mano.
            }
        }
    }

    private Optional<URI> pastedLink() {
        Optional<URI> uri = Links.parse(link.getText());
        if (uri.isEmpty()) {
            fail("El enlace no vale: tiene que ser http(s)://…/media/{id}.{ext}?… con su firma.");
        }
        return uri;
    }

    private void startMeasuring() {
        Optional<URI> uri = pastedLink();
        if (uri.isEmpty()) {
            return;
        }
        if (!Links.extension(uri.get()).equals("flac")) {
            fail("Solo se puede medir un FLAC: el AAC no tiene un MD5 con el que comparar.");
            return;
        }
        stopListening();
        busy(true);
        clearResult();
        worker.execute(() -> {
            try {
                Measure.Result r = Measure.run(http, mpv, uri.get(), text -> Platform.runLater(() -> status.setText(text)));
                Platform.runLater(() -> showMeasurement(uri.get(), r));
            } catch (IOException e) {
                Platform.runLater(() -> fail("No se pudo medir: " + e.getMessage()));
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            } finally {
                Platform.runLater(() -> busy(false));
            }
        });
    }

    private void showMeasurement(URI uri, Measure.Result r) {
        formats.setText("Original: " + r.original().describe() + ".\nmpv entrega: " + r.output().describe() + ".");
        md5.setText(r.md5());
        String result;
        if (!r.original().hasMd5()) {
            result = "El original no tiene MD5 en STREAMINFO: no hay con qué comparar.";
        } else if (r.matches()) {
            result = "Coincide con el MD5 del original.";
        } else {
            result = "NO coincide. Original: " + r.original().md5()
                    + (r.allSamples() ? "" : ". Faltan o sobran muestras: " + r.samples() + " contadas")
                    + (r.sameFormat() ? "" : ". mpv cambió la frecuencia o los canales");
        }
        verdict.setText(result);
        status.setText("Medido.");
        report("medir " + Links.safe(uri) + ": " + r.original().describe() + "; mpv entrega " + r.output().describe()
                + "; MD5 " + r.md5() + "; original " + r.original().md5() + "; " + result, r.matches());
    }

    private void startListening() {
        Optional<URI> uri = pastedLink();
        if (uri.isEmpty()) {
            return;
        }
        stopListening();
        busy(true);
        clearResult();
        boolean exclusiveMode = exclusive.isSelected();
        status.setText("Arrancando mpv…");
        worker.execute(() -> {
            try {
                Listen started = Listen.start(mpv, uri.get(), exclusiveMode, new Listen.View() {
                    @Override
                    public void formats(Optional<AudioParams> decoded, Optional<AudioParams> output) {
                        Platform.runLater(() -> showFormats(decoded, output));
                    }

                    @Override
                    public void finished(String reason, Optional<IOException> error) {
                        Platform.runLater(() -> listeningFinished(uri.get(), exclusiveMode, reason, error));
                    }
                });
                Platform.runLater(() -> {
                    listening = started;
                    stop.setDisable(false);
                    status.setText("Sonando " + Links.safe(uri.get()) + (exclusiveMode ? " en modo exclusivo." : " en modo compartido."));
                });
            } catch (IOException e) {
                Platform.runLater(() -> {
                    fail("No se pudo escuchar: " + e.getMessage());
                    busy(false);
                });
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        });
    }

    private void showFormats(Optional<AudioParams> decoded, Optional<AudioParams> output) {
        formats.setText("Decodificador: " + decoded.map(AudioParams::describe).orElse("—")
                + ".\nA Windows: " + output.map(AudioParams::describe).orElse("—") + ".");
        if (decoded.isPresent() && output.isPresent()) {
            verdict.setText(decoded.equals(output)
                    ? "Sin conversión: Windows recibe lo mismo que da el decodificador."
                    : "Hay conversión antes de Windows.");
        }
    }

    private void listeningFinished(URI uri, boolean exclusiveMode, String reason, Optional<IOException> error) {
        String formatsText = formats.getText().replace("\n", " ");
        boolean ok = reason.equals("eof") && error.isEmpty() && verdict.getText().startsWith("Sin conversión");
        status.setText(error.map(e -> "Error: " + e.getMessage()).orElse("Terminó (" + reason + ")."));
        report("escuchar " + Links.safe(uri) + (exclusiveMode ? " (exclusivo)" : " (compartido)") + ": "
                + formatsText + " " + verdict.getText() + " Terminó: " + reason
                + error.map(e -> " (" + e.getMessage() + ")").orElse(""), ok);
        stopListening();
    }

    private void stopListening() {
        if (listening != null) {
            try {
                listening.close();
            } catch (IOException e) {
                fail("No se pudo cerrar mpv: " + e.getMessage());
            }
            listening = null;
        }
        stop.setDisable(true);
        busy(false);
    }

    private void busy(boolean busy) {
        listen.setDisable(busy);
        measure.setDisable(busy);
    }

    private void clearResult() {
        formats.setText("");
        md5.setText("");
        verdict.setText("");
    }

    private void fail(String message) {
        status.setText(message);
        System.err.println(message);
        if (exitWhenDone) {
            exit(1);
        }
    }

    private void report(String line, boolean ok) {
        System.out.println(line);
        if (exitWhenDone) {
            exit(ok ? 0 : 1);
        }
    }

    private void exit(int code) {
        stop();
        System.exit(code);
    }
}
