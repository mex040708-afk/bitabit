package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.Contract.Track;
import java.io.IOException;
import java.util.Map;
import java.util.Optional;
import java.util.OptionalDouble;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;
import javafx.application.Application;
import javafx.application.Platform;
import javafx.geometry.Insets;
import javafx.scene.Parent;
import javafx.scene.Scene;
import javafx.scene.control.Button;
import javafx.scene.control.Label;
import javafx.scene.layout.VBox;
import javafx.stage.Stage;

/**
 * La app de escritorio: inicia sesión en el servidor (ADR-025), enseña el catálogo y lo
 * reproduce con cola (ADR-026). El panel de diagnóstico del prototipo de la fase 1
 * (ADR-013) sigue en el menú.
 *
 * <p>Las pruebas automáticas del prototipo siguen funcionando sin sesión: con
 * {@code --enlace-archivo=RUTA} o {@code --accion=medir|escuchar} la app abre solo el
 * panel de diagnóstico, y {@code --exclusivo=no} y {@code --salir} hacen lo mismo que
 * antes (ver {@link DiagnosticsView}).
 */
public final class App extends Application {

    /** La red, nunca en el hilo de JavaFX (ADR-026). Un hilo basta: las llamadas van de una en una. */
    private final ExecutorService network = Executors.newSingleThreadExecutor(task -> {
        Thread t = new Thread(task, "red");
        t.setDaemon(true);
        return t;
    });

    /** Las carátulas van aparte: una lenta no tiene que hacer esperar al resto de las llamadas. */
    private final ExecutorService coverPool = Executors.newFixedThreadPool(3, task -> {
        Thread t = new Thread(task, "caratulas");
        t.setDaemon(true);
        return t;
    });

    /**
     * Pedir el enlace y hablar con mpv, de uno en uno y en orden (ADR-026): los comandos de
     * mpv esperan hasta 5 s y no pueden ir en el hilo de JavaFX ni en el de la red.
     */
    private final ExecutorService playbackPool = Executors.newSingleThreadExecutor(task -> {
        Thread t = new Thread(task, "reproduccion");
        t.setDaemon(true);
        return t;
    });

    private final String mpvExe = System.getenv().getOrDefault("BITABIT_MPV", "mpv");

    private final Sessions sessions = new Sessions(new SessionStore(SessionStore.defaultDir()),
            Sessions.deviceName(System.getenv()));

    private Scene scene;
    private DiagnosticsView diagnostics;
    private LoginView login;
    private MainView main;
    private PlayerBar bar;
    /** La reproducción de la sesión abierta, o null. Solo se toca desde el hilo de JavaFX. */
    private Playback playback;

    /** Solo se toca desde el hilo de JavaFX. */
    private Sessions.Active active;

    public static void main(String[] args) {
        launch(args);
    }

    @Override
    public void start(Stage stage) {
        Map<String, String> named = getParameters().getNamed();
        boolean exitWhenDone = getParameters().getRaw().contains("--salir");
        diagnostics = new DiagnosticsView(exitWhenDone, this::exit);
        scene = new Scene(new VBox(), 1000, 680);
        stage.setTitle("Bitabit");
        stage.setScene(scene);

        if (named.containsKey("enlace-archivo") || named.containsKey("accion")) {
            VBox root = new VBox(diagnostics.root());
            root.setPadding(new Insets(16));
            scene.setRoot(root);
            stage.show();
            diagnostics.autostart(named);
            return;
        }

        login = new LoginView(this::login);
        main = new MainView(diagnostics.root(), this::logout);
        bar = new PlayerBar();
        stage.show();
        resume();
    }

    @Override
    public void stop() {
        closePlayback();
        playbackPool.shutdown();
        try {
            // Que mpv se cierre antes de que acabe la JVM, o se quedaría vivo.
            if (!playbackPool.awaitTermination(6, TimeUnit.SECONDS)) {
                System.getLogger(App.class.getName()).log(System.Logger.Level.WARNING,
                        "mpv no se cerró en 6 s al salir");
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
        diagnostics.close();
        network.shutdownNow();
        coverPool.shutdownNow();
    }

    private void resume() {
        scene.setRoot(notice("Comprobando la sesión guardada…", Optional.empty()));
        inBackground(sessions::resume, result -> {
            switch (result) {
                case Sessions.Resumed r -> showMain(r.session());
                case Sessions.NeedsLogin n -> showLogin(n.server(), n.notice());
                case Sessions.Unreachable u -> scene.setRoot(notice(u.message()
                        + "\nLa sesión sigue guardada.", Optional.of(this::resume)));
            }
        }, e -> scene.setRoot(notice("No se pudo comprobar la sesión guardada: " + e.getMessage(),
                Optional.of(this::resume))));
    }

    private void login(String server, String password) {
        String address;
        try {
            address = ServerAddress.parse(server);
        } catch (IllegalArgumentException e) {
            login.message(e.getMessage());
            return;
        }
        if (password.isEmpty()) {
            login.message("Escribe la contraseña.");
            return;
        }
        login.busy(true);
        inBackground(() -> sessions.login(address, password), session -> {
            login.busy(false);
            showMain(session);
        }, e -> {
            login.busy(false);
            login.message(e instanceof IOException io ? Messages.login(address, io) : e.getMessage());
            login.focus();
        });
    }

    private void logout() {
        Sessions.Active closing = active;
        main.busy(true);
        closePlayback();
        inBackground(() -> sessions.logout(closing), warning -> {
            main.busy(false);
            active = null;
            showLogin(Optional.of(closing.saved().server()), warning);
        }, e -> {
            main.busy(false);
            main.message("No se pudo cerrar la sesión: " + e.getMessage());
        });
    }

    private void showLogin(Optional<String> server, Optional<String> notice) {
        server.ifPresent(login::server);
        login.message(notice.orElse(""));
        scene.setRoot(login.root());
        login.focus();
    }

    private void showMain(Sessions.Active session) {
        active = session;
        Catalog catalog = new Catalog(session.client(), session.saved().server());
        Covers covers = new Covers(catalog, this::inCovers);
        closePlayback();
        PlaybackEvents events = new PlaybackEvents();
        Playback started = new Playback(this::inPlayback, exclusive -> startPlayer(exclusive, events), catalog::link,
                new Playback.View() {
                    @Override
                    public void nowPlaying(Optional<Track> track) {
                        bar.nowPlaying(track);
                    }

                    @Override
                    public void problem(String message) {
                        bar.problem(message);
                    }

                    @Override
                    public void sessionExpired() {
                        bar.sessionExpired();
                        App.this.sessionExpired(session);
                    }
                });
        events.owner = started;
        playback = started;
        bar.bind(started);
        main.open(session.saved().server(), new CatalogView(catalog, this::inBackground, covers,
                () -> sessionExpired(session), started::play), bar, started::pause);
        scene.setRoot(main.root());
    }

    /** El servidor ya no acepta el token: se olvida la sesión y se vuelve al login. */
    private void sessionExpired(Sessions.Active expired) {
        if (active != expired) {
            return;
        }
        active = null;
        closePlayback();
        String server = expired.saved().server();
        String notice = "La sesión ha caducado en " + server + ". Vuelve a iniciar sesión.";
        try {
            sessions.expire(expired);
        } catch (IOException e) {
            notice += " No se pudo borrar la sesión guardada: " + e.getMessage();
        }
        showLogin(Optional.of(server), Optional.of(notice));
    }

    /**
     * Arranca mpv para la reproducción, en el hilo «reproduccion». Sus avisos llegan del hilo
     * de la tubería y {@code events} los pasa al de JavaFX.
     */
    private Playback.Engine startPlayer(boolean exclusive, PlaybackEvents events)
            throws IOException, InterruptedException {
        try {
            return Player.start(mpvExe, exclusive, events);
        } catch (IOException e) {
            throw new IOException("No se pudo arrancar mpv (" + mpvExe + "): " + e.getMessage()
                    + ". Instálalo o pon su ruta en BITABIT_MPV.", e);
        }
    }

    /**
     * Pasa los avisos del Player a la barra y a la reproducción que lo arrancó. Los de una
     * sesión que ya se cerró se descartan: su mpv puede tardar en callarse.
     */
    private final class PlaybackEvents implements Player.Listener {

        /** Se pone una vez, en el hilo de JavaFX, antes de que arranque ningún mpv. */
        private Playback owner;

        /** Solo en el hilo de JavaFX. */
        private boolean current() {
            return owner != null && owner == playback;
        }

        @Override
        public void onPosition(OptionalDouble seconds) {
            Platform.runLater(() -> {
                if (current()) {
                    bar.position(seconds);
                    owner.onPosition(seconds);
                }
            });
        }

        @Override
        public void onDuration(OptionalDouble seconds) {
            Platform.runLater(() -> {
                if (current()) {
                    bar.duration(seconds);
                }
            });
        }

        @Override
        public void onPause(boolean paused) {
            Platform.runLater(() -> {
                if (current()) {
                    bar.paused(paused);
                }
            });
        }

        @Override
        public void onDecodedFormat(Optional<AudioParams> params) {
            // La barra enseña lo que llega a Windows; lo decodificado está en el diagnóstico.
        }

        @Override
        public void onOutputFormat(Optional<AudioParams> params) {
            Platform.runLater(() -> {
                if (current()) {
                    bar.outputFormat(params);
                }
            });
        }

        @Override
        public void onTrackEnded(EndReason reason, Optional<String> error) {
            Platform.runLater(() -> {
                if (current()) {
                    owner.onTrackEnded(reason, error);
                }
            });
        }

        @Override
        public void onFailure(IOException error) {
            Platform.runLater(() -> {
                if (current()) {
                    owner.onEngineFailed(error);
                }
            });
        }
    }

    private void closePlayback() {
        if (playback != null) {
            playback.close();
            playback = null;
        }
    }

    private <T> void inPlayback(Callable<T> task, Consumer<T> done, Consumer<Throwable> failed) {
        inBackground(playbackPool, task, done, failed);
    }

    /** Un texto y, si se puede reintentar, el botón para hacerlo. */
    private static Parent notice(String text, Optional<Runnable> retry) {
        Label label = new Label(text);
        label.setWrapText(true);
        VBox root = new VBox(10, label);
        retry.ifPresent(action -> {
            Button button = new Button("Reintentar");
            button.setOnAction(e -> action.run());
            root.getChildren().add(button);
        });
        root.setPadding(new Insets(16));
        return root;
    }

    /**
     * Hace {@code task} en el hilo de la red y vuelve al de JavaFX con el resultado o con el
     * error. Los errores llegan aquí siempre: un hilo de fondo que muere en silencio deja la
     * ventana esperando para siempre.
     */
    private <T> void inBackground(Callable<T> task, Consumer<T> done, Consumer<Throwable> failed) {
        inBackground(network, task, done, failed);
    }

    private <T> void inCovers(Callable<T> task, Consumer<T> done, Consumer<Throwable> failed) {
        inBackground(coverPool, task, done, failed);
    }

    private static <T> void inBackground(ExecutorService executor, Callable<T> task, Consumer<T> done,
            Consumer<Throwable> failed) {
        executor.execute(() -> {
            try {
                T result = task.call();
                Platform.runLater(() -> done.accept(result));
            } catch (Exception e) {
                Platform.runLater(() -> failed.accept(e));
            } catch (Error e) {
                // Se cuenta en la ventana y se deja seguir: un Error no se puede arreglar aquí.
                Platform.runLater(() -> failed.accept(e));
                throw e;
            }
        });
    }

    private void exit(int code) {
        stop();
        System.exit(code);
    }
}
