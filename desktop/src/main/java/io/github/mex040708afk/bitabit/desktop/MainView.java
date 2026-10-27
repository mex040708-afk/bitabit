package io.github.mex040708afk.bitabit.desktop;

import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.Node;
import javafx.scene.Parent;
import javafx.scene.control.Button;
import javafx.scene.control.Label;
import javafx.scene.control.ToggleButton;
import javafx.scene.control.ToggleGroup;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.Priority;
import javafx.scene.layout.Region;
import javafx.scene.layout.VBox;

/**
 * La ventana principal (ADR-026): navegación lateral (artistas, álbumes y el panel de
 * diagnóstico del prototipo), el contenido al lado, abajo del menú la sesión con
 * «Cerrar sesión» y, debajo de todo, la barra de reproducción.
 */
final class MainView {

    private final Label server = new Label();
    private final Button logout = new Button("Cerrar sesión");
    private final Label message = new Label();
    private final Node diagnostics;
    private final BorderPane root = new BorderPane();
    private final ToggleButton artists = new ToggleButton("Artistas");
    private final ToggleButton albums = new ToggleButton("Álbumes");
    private final ToggleButton diagnosis = new ToggleButton("Diagnóstico");
    private final ToggleGroup group = new ToggleGroup();
    private CatalogView catalog;
    /** Se llama al entrar en el diagnóstico, que arranca su propio mpv. */
    private Runnable onDiagnosis = () -> {
    };

    MainView(Node diagnostics, Runnable onLogout) {
        this.diagnostics = diagnostics;
        message.setWrapText(true);
        message.setMaxWidth(160);
        server.setWrapText(true);
        server.setMaxWidth(160);
        logout.setOnAction(e -> onLogout.run());

        for (ToggleButton button : new ToggleButton[] {artists, albums, diagnosis}) {
            button.setToggleGroup(group);
            button.setMaxWidth(Double.MAX_VALUE);
            button.setAlignment(Pos.CENTER_LEFT);
        }
        // Una sección siempre seleccionada: pulsar la activa no la apaga.
        group.selectedToggleProperty().addListener((obs, old, now) -> {
            if (now == null) {
                old.setSelected(true);
            }
        });
        artists.setOnAction(e -> {
            if (artists.isSelected() && catalog != null) {
                catalog.showArtists();
                root.setCenter(catalog.root());
            }
        });
        albums.setOnAction(e -> {
            if (albums.isSelected() && catalog != null) {
                catalog.showAlbums();
                root.setCenter(catalog.root());
            }
        });
        diagnosis.setOnAction(e -> {
            if (diagnosis.isSelected()) {
                onDiagnosis.run();
                root.setCenter(this.diagnostics);
            }
        });

        Region spacer = new Region();
        VBox.setVgrow(spacer, Priority.ALWAYS);
        VBox menu = new VBox(6, artists, albums, diagnosis, spacer, server, message, logout);
        menu.setPadding(new Insets(12));
        menu.setPrefWidth(180);
        menu.setStyle("-fx-background-color: -fx-box-border;");
        root.setLeft(menu);
    }

    Parent root() {
        return root;
    }

    /**
     * Empieza una sesión: enseña los artistas y la barra de reproducción.
     *
     * @param onDiagnosis se llama al entrar en el diagnóstico
     */
    void open(String address, CatalogView catalogView, PlayerBar bar, Runnable onDiagnosis) {
        this.onDiagnosis = onDiagnosis;
        root.setBottom(bar.root());
        server.setText("Sesión en " + address);
        message("");
        catalog = catalogView;
        artists.setSelected(true);
        catalog.showArtists();
        root.setCenter(catalog.root());
    }

    void busy(boolean busy) {
        logout.setDisable(busy);
    }

    void message(String text) {
        message.setText(text);
    }
}
