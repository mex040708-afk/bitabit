package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import javafx.geometry.Pos;
import javafx.scene.control.Label;
import javafx.scene.image.ImageView;
import javafx.scene.layout.StackPane;

/**
 * Un cuadrado con la carátula de un álbum. Mientras carga, o si el álbum no tiene o falla,
 * enseña un marcador. Solo se toca desde el hilo de JavaFX.
 */
final class CoverBox extends StackPane {

    private final Covers covers;
    private final ImageView image = new ImageView();
    private final Label placeholder = new Label("♪");
    /** El álbum que se espera: una carátula que llega tarde para otro álbum se descarta. */
    private long wanted;

    CoverBox(Covers covers, double size) {
        this.covers = covers;
        setMinSize(size, size);
        setPrefSize(size, size);
        setMaxSize(size, size);
        setAlignment(Pos.CENTER);
        setStyle("-fx-background-color: #3a3a3f;");
        placeholder.setStyle("-fx-text-fill: #8a8a92; -fx-font-size: " + (int) (size / 3) + "px;");
        image.setFitWidth(size);
        image.setFitHeight(size);
        image.setPreserveRatio(true);
        image.setVisible(false);
        getChildren().addAll(placeholder, image);
    }

    void show(AlbumSummary album) {
        wanted = album.getId();
        image.setImage(null);
        image.setVisible(false);
        if (!album.hasCover()) {
            return;
        }
        long id = album.getId();
        covers.request(id, loaded -> {
            if (id == wanted && loaded.isPresent()) {
                image.setImage(loaded.get());
                image.setVisible(true);
            }
        });
    }
}
