package io.github.mex040708afk.bitabit.desktop;

import java.io.IOException;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
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
 * La app de escritorio: inicia sesión en el servidor (ADR-025) y, por ahora, enseña el
 * panel de diagnóstico del prototipo de la fase 1 (ADR-013).
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

    private final Sessions sessions = new Sessions(new SessionStore(SessionStore.defaultDir()),
            Sessions.deviceName(System.getenv()));

    private Scene scene;
    private DiagnosticsView diagnostics;
    private LoginView login;
    private MainView main;

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
        scene = new Scene(new VBox(), 640, 480);
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
        stage.show();
        resume();
    }

    @Override
    public void stop() {
        diagnostics.close();
        network.shutdownNow();
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
        main.server(session.saved().server());
        main.message("");
        scene.setRoot(main.root());
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
        network.execute(() -> {
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
