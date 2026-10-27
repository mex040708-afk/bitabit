package io.github.mex040708afk.bitabit.desktop;

import javafx.geometry.Insets;
import javafx.scene.Node;
import javafx.scene.Parent;
import javafx.scene.control.Button;
import javafx.scene.control.Label;
import javafx.scene.control.Separator;
import javafx.scene.layout.HBox;
import javafx.scene.layout.VBox;

/**
 * La vista principal provisional, hasta que llegue el catálogo: con qué servidor hay
 * sesión, «Cerrar sesión» y, debajo, el panel de diagnóstico del prototipo.
 */
final class MainView {

    private final Label server = new Label();
    private final Button logout = new Button("Cerrar sesión");
    private final Label message = new Label();
    private final VBox root;

    MainView(Node diagnostics, Runnable onLogout) {
        message.setWrapText(true);
        logout.setOnAction(e -> onLogout.run());
        root = new VBox(10, new HBox(10, server, logout), message, new Separator(), diagnostics);
        root.setPadding(new Insets(16));
    }

    Parent root() {
        return root;
    }

    void server(String address) {
        server.setText("Sesión iniciada en " + address);
    }

    void busy(boolean busy) {
        logout.setDisable(busy);
    }

    void message(String text) {
        message.setText(text);
    }
}
