package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.Contract.Track;
import java.util.Optional;
import java.util.OptionalDouble;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.Parent;
import javafx.scene.control.Button;
import javafx.scene.control.CheckBox;
import javafx.scene.control.Label;
import javafx.scene.control.Slider;
import javafx.scene.layout.HBox;
import javafx.scene.layout.Priority;
import javafx.scene.layout.VBox;

/**
 * La barra de reproducción, abajo de la ventana (ADR-026): anterior, pausa y siguiente, la
 * pista que suena, la posición, el formato que llega a Windows y el modo exclusivo. Solo se
 * toca desde el hilo de JavaFX.
 */
final class PlayerBar implements Playback.View {

    private final Button previous = new Button("⏮");
    private final Button playPause = new Button("▶");
    private final Button next = new Button("⏭");
    private final Label title = new Label("Nada en la cola");
    private final Label artist = new Label();
    private final Slider position = new Slider(0, 1, 0);
    private final Label time = new Label();
    private final Label output = new Label();
    private final Label problem = new Label();
    private final CheckBox exclusive = new CheckBox("Modo exclusivo");
    private final VBox root;
    private Playback playback;
    private OptionalDouble duration = OptionalDouble.empty();

    PlayerBar() {
        for (Button button : new Button[] {previous, playPause, next}) {
            button.setMinWidth(40);
        }
        title.setStyle("-fx-font-weight: bold;");
        artist.setStyle("-fx-text-fill: #8a8a92;");
        output.setStyle("-fx-text-fill: #8a8a92; -fx-font-size: 11px;");
        problem.setStyle("-fx-text-fill: #c0392b;");
        problem.setWrapText(true);
        problem.managedProperty().bind(problem.visibleProperty());
        problem.setVisible(false);
        exclusive.setSelected(true);

        VBox text = new VBox(2, title, artist);
        text.setMinWidth(200);
        text.setPrefWidth(260);
        HBox.setHgrow(position, Priority.ALWAYS);
        position.setMaxWidth(Double.MAX_VALUE);
        time.setMinWidth(90);
        HBox row = new HBox(10, previous, playPause, next, text, position, time, exclusive);
        row.setAlignment(Pos.CENTER_LEFT);
        root = new VBox(4, row, output, problem);
        root.setPadding(new Insets(8, 12, 8, 12));
        root.setStyle("-fx-border-color: -fx-box-border; -fx-border-width: 1 0 0 0;");
        clear();
    }

    Parent root() {
        return root;
    }

    /** Une la barra con la reproducción de la sesión. */
    void bind(Playback playback) {
        this.playback = playback;
        previous.setOnAction(e -> playback.previous());
        playPause.setOnAction(e -> playback.togglePause());
        next.setOnAction(e -> playback.next());
        exclusive.setSelected(playback.exclusive());
        exclusive.setOnAction(e -> playback.setExclusive(exclusive.isSelected()));
        // Salta al soltar: mientras se arrastra, la posición de mpv no mueve el control.
        position.setOnMouseReleased(e -> playback.seek(position.getValue()));
        clear();
    }

    @Override
    public void nowPlaying(Optional<Track> track) {
        boolean any = track.isPresent();
        title.setText(track.map(t -> Display.name(t.getTitle())).orElse("Nada en la cola"));
        artist.setText(track.map(t -> Display.name(t.getArtist())).orElse(""));
        for (Button button : new Button[] {previous, playPause, next}) {
            button.setDisable(!any);
        }
        position.setDisable(!any);
        // Cada pista nueva empieza sonando (Playback quita la pausa al cargarla).
        paused(!any);
        duration = track.map(t -> OptionalDouble.of(t.getDurationMs() / 1000.0)).orElse(OptionalDouble.empty());
        position(OptionalDouble.of(0));
    }

    @Override
    public void problem(String message) {
        problem.setText(message);
        problem.setVisible(!message.isEmpty());
    }

    @Override
    public void sessionExpired() {
        clear();
    }

    /** Lo que avisa el Player, ya en el hilo de JavaFX. */
    void position(OptionalDouble seconds) {
        if (position.isPressed() || position.isValueChanging()) {
            return;
        }
        double at = seconds.orElse(0);
        position.setMax(Math.max(duration.orElse(0), at));
        position.setValue(at);
        time.setText(Display.duration((long) (at * 1000)) + " / "
                + (duration.isPresent() ? Display.duration((long) (duration.getAsDouble() * 1000)) : "–"));
    }

    void duration(OptionalDouble seconds) {
        // mpv sabe la duración de verdad; sin ella, vale la del catálogo.
        if (seconds.isPresent()) {
            duration = seconds;
        }
    }

    void paused(boolean paused) {
        playPause.setText(paused ? "▶" : "⏸");
    }

    void outputFormat(Optional<AudioParams> params) {
        output.setText(params.map(p -> "A Windows: " + p.describe()
                + (playback != null && playback.exclusive() ? " (exclusivo)" : " (compartido)")).orElse(""));
    }

    private void clear() {
        nowPlaying(Optional.empty());
        paused(true);
        outputFormat(Optional.empty());
        problem("");
    }
}
