package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;
import javafx.geometry.Insets;
import javafx.scene.Cursor;
import javafx.scene.control.Label;
import javafx.scene.control.ListCell;
import javafx.scene.control.ListView;
import javafx.scene.control.Tooltip;
import javafx.scene.layout.HBox;
import javafx.scene.layout.VBox;

/**
 * Una rejilla de álbumes con carátula. JavaFX no trae rejilla virtualizada: es un
 * {@link ListView} cuyas filas son tiras de álbumes, con tantas columnas como quepan, así
 * que solo hay nodos para lo que se ve aunque la biblioteca tenga miles de álbumes.
 */
final class AlbumGrid extends ListView<List<AlbumSummary>> {

    private static final double COVER = 150;
    private static final double TILE_WIDTH = COVER + 20;
    private static final double ROW_HEIGHT = COVER + 66;

    private final Covers covers;
    private final Consumer<AlbumSummary> onOpen;
    private final boolean showArtist;
    private List<AlbumSummary> albums = List.of();
    private int columns;

    /**
     * @param showArtist si cada álbum enseña su artista (en la lista de un artista sobra)
     * @param onOpen se llama al pulsar un álbum
     */
    AlbumGrid(Covers covers, boolean showArtist, Consumer<AlbumSummary> onOpen) {
        this.covers = covers;
        this.showArtist = showArtist;
        this.onOpen = onOpen;
        setFixedCellSize(ROW_HEIGHT);
        setCellFactory(list -> new RowCell());
        widthProperty().addListener((obs, old, width) -> regroup(width.doubleValue()));
    }

    void setAlbums(List<AlbumSummary> albums) {
        this.albums = albums;
        columns = 0;
        regroup(getWidth());
    }

    private void regroup(double width) {
        // 24 de margen y barra de desplazamiento.
        int fit = Math.max(1, (int) ((width - 24) / TILE_WIDTH));
        if (fit == columns) {
            return;
        }
        columns = fit;
        List<List<AlbumSummary>> rows = new ArrayList<>();
        for (int from = 0; from < albums.size(); from += fit) {
            rows.add(albums.subList(from, Math.min(albums.size(), from + fit)));
        }
        getItems().setAll(rows);
    }

    private final class RowCell extends ListCell<List<AlbumSummary>> {

        private final HBox box = new HBox(10);
        private final List<Tile> tiles = new ArrayList<>();

        RowCell() {
            box.setPadding(new Insets(4, 0, 4, 4));
            // Las filas son tiras de álbumes: que se seleccione una entera no significa nada.
            setStyle("-fx-background-color: transparent;");
        }

        @Override
        protected void updateItem(List<AlbumSummary> row, boolean empty) {
            super.updateItem(row, empty);
            if (empty || row == null) {
                setGraphic(null);
                return;
            }
            while (tiles.size() < row.size()) {
                tiles.add(new Tile());
            }
            box.getChildren().clear();
            for (int i = 0; i < row.size(); i++) {
                tiles.get(i).bind(row.get(i));
                box.getChildren().add(tiles.get(i));
            }
            setGraphic(box);
        }
    }

    private final class Tile extends VBox {

        private final CoverBox cover = new CoverBox(covers, COVER);
        private final Label title = new Label();
        private final Label subtitle = new Label();
        private final Tooltip tip = new Tooltip();
        private AlbumSummary album;

        Tile() {
            super(3);
            setPrefWidth(TILE_WIDTH - 10);
            setMaxWidth(TILE_WIDTH - 10);
            title.setMaxWidth(COVER);
            subtitle.setMaxWidth(COVER);
            subtitle.setStyle("-fx-text-fill: #8a8a92; -fx-font-size: 11px;");
            getChildren().addAll(cover, title, subtitle);
            setCursor(Cursor.HAND);
            Tooltip.install(this, tip);
            setOnMouseClicked(e -> {
                if (album != null) {
                    onOpen.accept(album);
                }
            });
        }

        void bind(AlbumSummary album) {
            this.album = album;
            cover.show(album);
            boolean unnamed = Display.isBlank(album.getTitle());
            title.setText(Display.name(album.getTitle()));
            title.setStyle(unnamed ? "-fx-font-style: italic;" : "");
            String year = album.getYear().map(y -> " · " + y).orElse("");
            String artist = showArtist && album.getArtist() != null ? Display.name(album.getArtist().getName()) : "";
            String caption = showArtist
                    ? artist + year
                    : Display.count(album.getTrackCount(), "pista", "pistas") + year;
            subtitle.setText(caption);
            tip.setText(title.getText() + "\n" + caption);
        }
    }
}
