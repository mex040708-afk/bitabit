package io.github.mex040708afk.bitabit.desktop;

import java.util.function.BiConsumer;
import javafx.geometry.Insets;
import javafx.scene.Parent;
import javafx.scene.control.Button;
import javafx.scene.control.Label;
import javafx.scene.control.PasswordField;
import javafx.scene.control.TextField;
import javafx.scene.layout.VBox;

/** La pantalla de inicio de sesión: servidor y contraseña. Solo se toca desde el hilo de JavaFX. */
final class LoginView {

    private final TextField server = new TextField();
    private final PasswordField password = new PasswordField();
    private final Button enter = new Button("Entrar");
    private final Label message = new Label();
    private final VBox root;

    /** @param onLogin recibe el servidor y la contraseña tal como se escribieron */
    LoginView(BiConsumer<String, String> onLogin) {
        server.setPromptText("http://servidor:8080");
        password.setPromptText("Contraseña de la cuenta");
        message.setWrapText(true);
        Runnable submit = () -> {
            String typed = password.getText();
            // Se borra ya: no tiene que quedarse en el campo mientras se espera al servidor.
            password.clear();
            onLogin.accept(server.getText(), typed);
        };
        enter.setOnAction(e -> submit.run());
        enter.setDefaultButton(true);
        root = new VBox(10,
                new Label("Iniciar sesión en Bitabit"),
                new Label("Servidor"), server,
                new Label("Contraseña"), password,
                enter, message);
        root.setPadding(new Insets(16));
        root.setMaxWidth(420);
    }

    Parent root() {
        return root;
    }

    /** Mientras se espera al servidor no se puede volver a pulsar. */
    void busy(boolean busy) {
        enter.setDisable(busy);
        server.setDisable(busy);
        password.setDisable(busy);
        if (busy) {
            message.setText("Entrando…");
        }
    }

    void message(String text) {
        message.setText(text);
    }

    void server(String address) {
        server.setText(address);
    }

    /** Pone el foco donde hay que escribir: el servidor si falta, si no la contraseña. */
    void focus() {
        if (server.getText().isBlank()) {
            server.requestFocus();
        } else {
            password.requestFocus();
        }
    }
}
