package io.github.mex040708afk.bitabit.desktop;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonNull;
import com.google.gson.JsonObject;
import com.google.gson.JsonParseException;
import com.google.gson.JsonParser;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.net.URI;
import java.nio.ByteBuffer;
import java.nio.channels.AsynchronousFileChannel;
import java.nio.charset.StandardCharsets;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Un proceso de mpv controlado por su JSON IPC, en una tubería con nombre
 * propia de cada arranque (ADR-004 y ADR-013).
 */
final class Mpv implements AutoCloseable {

    /** Lo que llega de mpv. Se llama desde el hilo que lee la tubería. */
    interface Listener {

        /**
         * Un evento de mpv. No puede llamar a {@link #command}: la respuesta
         * la tendría que leer el mismo hilo que está esperando.
         */
        void onEvent(JsonObject event);

        /** La tubería se cerró sin pedirlo, con el error si lo hubo. */
        void onClosed(Optional<IOException> error);
    }

    /** Siempre las mismas, para que nada de fuera toque el audio (ADR-013). */
    private static final List<String> BASE_OPTIONS = List.of(
            "--no-config", // sin el mpv.conf ni los scripts del usuario
            "--no-terminal",
            "--idle=yes",
            "--no-video",
            "--ytdl=no", // si mpv no abre el enlace, no se lo pasa a yt-dlp con su firma
            "--network-timeout=10",
            "--gapless-audio=yes",
            "--volume=100",
            "--replaygain=no");

    private static final Duration CONNECT_TIMEOUT = Duration.ofSeconds(5);
    private static final Duration COMMAND_TIMEOUT = Duration.ofSeconds(5);
    private static final Duration QUIT_TIMEOUT = Duration.ofSeconds(3);
    private static final AtomicInteger STARTED = new AtomicInteger();

    private final Process process;
    private final AsynchronousFileChannel pipe;
    private final Listener listener;
    private final AtomicInteger requestIds = new AtomicInteger();
    private final Map<Integer, CompletableFuture<JsonElement>> pending = new ConcurrentHashMap<>();
    private final Object writeLock = new Object();
    private volatile boolean closing;

    private Mpv(Process process, AsynchronousFileChannel pipe, Listener listener) {
        this.process = process;
        this.pipe = pipe;
        this.listener = listener;
        Thread reader = new Thread(this::readLoop, "mpv-ipc");
        reader.setDaemon(true);
        reader.start();
    }

    /**
     * Arranca mpv y se conecta a su tubería.
     *
     * @param exe la ruta de mpv.exe, o «mpv» para buscarlo en el PATH
     * @param options opciones que se suman a las de siempre
     */
    static Mpv start(String exe, List<String> options, Listener listener) throws IOException, InterruptedException {
        String pipeName = "\\\\.\\pipe\\bitabit-mpv-" + ProcessHandle.current().pid() + "-" + STARTED.incrementAndGet();
        List<String> command = new ArrayList<>();
        command.add(exe);
        command.addAll(BASE_OPTIONS);
        command.addAll(options);
        command.add("--input-ipc-server=" + pipeName);
        Process process = new ProcessBuilder(command)
                .redirectOutput(ProcessBuilder.Redirect.DISCARD)
                .redirectError(ProcessBuilder.Redirect.DISCARD)
                .start();
        try {
            return new Mpv(process, connect(Path.of(pipeName), process), listener);
        } catch (IOException | InterruptedException | RuntimeException e) {
            process.destroyForcibly();
            throw e;
        }
    }

    private static AsynchronousFileChannel connect(Path pipe, Process process) throws IOException, InterruptedException {
        long deadline = System.nanoTime() + CONNECT_TIMEOUT.toNanos();
        while (true) {
            try {
                // E/S asíncrona: con un canal síncrono, la lectura pendiente
                // bloquearía las escrituras (ADR-013).
                return AsynchronousFileChannel.open(pipe, StandardOpenOption.READ, StandardOpenOption.WRITE);
            } catch (NoSuchFileException e) {
                if (!process.isAlive()) {
                    throw new IOException("mpv terminó con el código " + process.exitValue() + " sin abrir su tubería", e);
                }
                if (System.nanoTime() > deadline) {
                    throw new IOException("mpv no abrió su tubería en " + CONNECT_TIMEOUT.toSeconds() + " s", e);
                }
                Thread.sleep(50);
            }
        }
    }

    /** Manda un comando y espera la respuesta de mpv: lo que devuelve en {@code data}. */
    JsonElement command(Object... args) throws IOException, InterruptedException {
        int id = requestIds.incrementAndGet();
        JsonArray command = new JsonArray();
        for (Object arg : args) {
            if (arg instanceof Number n) {
                command.add(n);
            } else {
                command.add(arg.toString());
            }
        }
        JsonObject message = new JsonObject();
        message.add("command", command);
        message.addProperty("request_id", id);
        CompletableFuture<JsonElement> reply = new CompletableFuture<>();
        pending.put(id, reply);
        try {
            write(message + "\n");
            return reply.get(COMMAND_TIMEOUT.toMillis(), TimeUnit.MILLISECONDS);
        } catch (ExecutionException e) {
            throw new IOException(e.getCause().getMessage(), e.getCause());
        } catch (TimeoutException e) {
            throw new IOException("mpv no respondió a «" + args[0] + "» en " + COMMAND_TIMEOUT.toSeconds() + " s", e);
        } finally {
            pending.remove(id);
        }
    }

    /** Pide los cambios de una propiedad, que llegan como eventos {@code property-change}. */
    void observe(int id, String property) throws IOException, InterruptedException {
        command("observe_property", id, property);
    }

    /** Carga el enlace por la tubería, para que no salga en la línea de órdenes. */
    void load(URI link) throws IOException, InterruptedException {
        command("loadfile", link.toString());
    }

    /** Cierra mpv: le pide que salga y, si no lo hace a tiempo, lo mata. */
    @Override
    public void close() throws IOException {
        closing = true;
        try {
            if (process.isAlive()) {
                try {
                    write("{\"command\":[\"quit\"]}\n");
                } catch (IOException e) {
                    process.destroyForcibly();
                }
                if (!process.waitFor(QUIT_TIMEOUT.toMillis(), TimeUnit.MILLISECONDS)) {
                    process.destroyForcibly();
                    process.waitFor(QUIT_TIMEOUT.toMillis(), TimeUnit.MILLISECONDS);
                }
            }
        } catch (InterruptedException e) {
            process.destroyForcibly();
            Thread.currentThread().interrupt();
        } finally {
            pipe.close();
        }
    }

    private void write(String line) throws IOException, InterruptedException {
        ByteBuffer buffer = ByteBuffer.wrap(line.getBytes(StandardCharsets.UTF_8));
        synchronized (writeLock) {
            while (buffer.hasRemaining()) {
                try {
                    pipe.write(buffer, 0).get(COMMAND_TIMEOUT.toMillis(), TimeUnit.MILLISECONDS);
                } catch (ExecutionException e) {
                    throw new IOException("no se pudo escribir en la tubería de mpv", e.getCause());
                } catch (TimeoutException e) {
                    throw new IOException("mpv no lee su tubería", e);
                }
            }
        }
    }

    private void readLoop() {
        Optional<IOException> error = Optional.empty();
        ByteBuffer buffer = ByteBuffer.allocate(64 * 1024);
        ByteArrayOutputStream line = new ByteArrayOutputStream();
        try {
            while (pipe.read(buffer.clear(), 0).get() >= 0) {
                buffer.flip();
                while (buffer.hasRemaining()) {
                    byte b = buffer.get();
                    if (b == '\n') {
                        handle(line.toString(StandardCharsets.UTF_8));
                        line.reset();
                    } else {
                        line.write(b);
                    }
                }
            }
        } catch (ExecutionException e) {
            error = Optional.of(new IOException("se cortó la tubería de mpv", e.getCause()));
        } catch (IOException e) {
            error = Optional.of(e);
        } catch (InterruptedException e) {
            error = Optional.of(new IOException("se interrumpió la lectura de mpv", e));
        } catch (RuntimeException e) {
            error = Optional.of(new IOException("falló al tratar un mensaje de mpv", e));
        }
        IOException closed = error.orElseGet(() -> new IOException("mpv cerró su tubería"));
        pending.values().forEach(reply -> reply.completeExceptionally(closed));
        if (!closing) {
            listener.onClosed(error);
        }
    }

    private void handle(String line) throws IOException {
        JsonObject message;
        try {
            message = JsonParser.parseString(line).getAsJsonObject();
        } catch (JsonParseException | IllegalStateException e) {
            // Sin la línea: podría llevar el enlace con su firma.
            throw new IOException("mpv mandó algo que no es un objeto JSON", e);
        }
        if (message.has("event")) {
            listener.onEvent(message);
            return;
        }
        if (!message.has("request_id")) {
            return;
        }
        CompletableFuture<JsonElement> reply = pending.get(message.get("request_id").getAsInt());
        if (reply == null) {
            return;
        }
        String result = message.has("error") ? message.get("error").getAsString() : "sin respuesta";
        if (result.equals("success")) {
            reply.complete(message.has("data") ? message.get("data") : JsonNull.INSTANCE);
        } else {
            reply.completeExceptionally(new IOException("mpv: " + result));
        }
    }
}
