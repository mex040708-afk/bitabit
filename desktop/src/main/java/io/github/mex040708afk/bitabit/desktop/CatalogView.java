package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.BitabitClient.AlbumSort;
import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import io.github.mex040708afk.bitabit.core.Contract.Artist;
import io.github.mex040708afk.bitabit.core.Contract.ArtistSummary;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import java.io.IOException;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.function.BiConsumer;
import java.util.function.Function;
import javafx.beans.property.ReadOnlyStringWrapper;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.Node;
import javafx.scene.Parent;
import javafx.scene.control.Button;
import javafx.scene.control.ChoiceBox;
import javafx.scene.control.Label;
import javafx.scene.control.ListCell;
import javafx.scene.control.ListView;
import javafx.scene.control.TableCell;
import javafx.scene.control.TableColumn;
import javafx.scene.control.TableView;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.HBox;
import javafx.scene.layout.Priority;
import javafx.scene.layout.VBox;

/**
 * El catálogo: artistas, álbumes de un artista, todos los álbumes y las pistas de un álbum
 * (ADR-026). Cada pantalla se pide al servidor en segundo plano y queda guardada en una pila,
 * así «Atrás» vuelve sin recargar y con el desplazamiento donde estaba. Solo se toca desde
 * el hilo de JavaFX.
 */
final class CatalogView {

    private record Page(String title, Node content) {
    }

    private final Catalog catalog;
    private final Background background;
    private final Covers covers;
    private final Runnable onSessionExpired;
    /** Pone esas pistas en la cola y empieza por la del índice. */
    private final BiConsumer<List<Track>, Integer> onPlay;

    private final Deque<Page> history = new ArrayDeque<>();
    private final Button back = new Button("← Atrás");
    private final Label title = new Label();
    private final BorderPane root = new BorderPane();
    /** Lo que se está enseñando ahora: la página de arriba de la pila, o el aviso de carga o de error. */
    private Node showing;
    /** Sube con cada navegación: una respuesta de una pantalla que ya no se quiere se descarta. */
    private int navigation;

    CatalogView(Catalog catalog, Background background, Covers covers, Runnable onSessionExpired,
            BiConsumer<List<Track>, Integer> onPlay) {
        this.catalog = catalog;
        this.background = background;
        this.covers = covers;
        this.onSessionExpired = onSessionExpired;
        this.onPlay = onPlay;
        back.setOnAction(e -> goBack());
        title.setStyle("-fx-font-size: 18px; -fx-font-weight: bold;");
        title.setMaxWidth(Double.MAX_VALUE);
        HBox.setHgrow(title, Priority.ALWAYS);
        HBox bar = new HBox(10, back, title);
        bar.setAlignment(Pos.CENTER_LEFT);
        bar.setPadding(new Insets(8, 12, 8, 12));
        root.setTop(bar);
        back.setVisible(false);
        back.managedProperty().bind(back.visibleProperty());
    }

    Parent root() {
        return root;
    }

    void showArtists() {
        open("Artistas", true, catalog::artists, this::artistList);
    }

    void showAlbums() {
        showAlbums(AlbumSort.NAME);
    }

    private void showAlbums(AlbumSort sort) {
        open("Álbumes", true, () -> catalog.albums(sort), albums -> {
            AlbumGrid grid = new AlbumGrid(covers, true, this::showAlbum);
            grid.setAlbums(albums);
            ChoiceBox<AlbumSort> order = new ChoiceBox<>();
            order.getItems().addAll(AlbumSort.NAME, AlbumSort.ADDED);
            order.setValue(sort);
            order.setConverter(new javafx.util.StringConverter<>() {
                @Override
                public String toString(AlbumSort s) {
                    return s == AlbumSort.NAME ? "Por nombre" : "Últimos añadidos";
                }

                @Override
                public AlbumSort fromString(String text) {
                    throw new UnsupportedOperationException("el orden solo se elige de la lista");
                }
            });
            order.valueProperty().addListener((obs, old, chosen) -> showAlbums(chosen));
            VBox page = new VBox(6, new HBox(10, new Label("Orden"), order,
                    new Label(Display.count(albums.size(), "álbum", "álbumes"))), grid);
            ((HBox) page.getChildren().get(0)).setAlignment(Pos.CENTER_LEFT);
            page.setPadding(new Insets(0, 0, 0, 12));
            VBox.setVgrow(grid, Priority.ALWAYS);
            return emptyOr(albums.isEmpty(), "No hay álbumes en la biblioteca.", page);
        });
    }

    private void showArtist(ArtistSummary summary) {
        open(Display.name(summary.getName()), false, () -> catalog.artist(summary.getId()), (Artist artist) -> {
            AlbumGrid grid = new AlbumGrid(covers, false, this::showAlbum);
            grid.setAlbums(artist.getAlbums());
            return emptyOr(artist.getAlbums().isEmpty(), "Este artista no tiene álbumes.", grid);
        });
    }

    private void showAlbum(AlbumSummary summary) {
        open(Display.name(summary.getTitle()), false, () -> catalog.album(summary.getId()), this::albumDetail);
    }

    private Node artistList(List<ArtistSummary> artists) {
        ListView<ArtistSummary> list = new ListView<>();
        list.getItems().setAll(artists);
        list.setCellFactory(l -> new ListCell<>() {
            @Override
            protected void updateItem(ArtistSummary artist, boolean empty) {
                super.updateItem(artist, empty);
                if (empty || artist == null) {
                    setText(null);
                    setStyle("");
                    return;
                }
                setText(Display.name(artist.getName()) + "  ·  "
                        + Display.count(artist.getAlbumCount(), "álbum", "álbumes"));
                setStyle(Display.isBlank(artist.getName()) ? "-fx-font-style: italic;" : "");
            }
        });
        list.setOnMouseClicked(e -> openSelected(list));
        list.setOnKeyPressed(e -> {
            if (e.getCode() == javafx.scene.input.KeyCode.ENTER) {
                openSelected(list);
            }
        });
        return emptyOr(artists.isEmpty(), "No hay artistas en la biblioteca.", list);
    }

    private void openSelected(ListView<ArtistSummary> list) {
        ArtistSummary selected = list.getSelectionModel().getSelectedItem();
        if (selected != null) {
            showArtist(selected);
        }
    }

    private Node albumDetail(Album album) {
        CoverBox cover = new CoverBox(covers, 160);
        cover.show(album);
        Label name = new Label(Display.name(album.getTitle()));
        name.setStyle("-fx-font-size: 20px; -fx-font-weight: bold;"
                + (Display.isBlank(album.getTitle()) ? " -fx-font-style: italic;" : ""));
        name.setWrapText(true);
        String artist = album.getArtist() == null ? "" : Display.name(album.getArtist().getName());
        String year = album.getYear().map(y -> " · " + y).orElse("");
        Label info = new Label(artist + year + "\n" + Display.count(album.getTrackCount(), "pista", "pistas")
                + " · " + Display.duration(album.getDurationMs()));
        Button play = new Button("▶ Reproducir");
        play.setDisable(album.getTracks().isEmpty());
        play.setOnAction(e -> onPlay.accept(album.getTracks(), 0));
        VBox text = new VBox(4, name, info, play);
        text.setAlignment(Pos.CENTER_LEFT);
        HBox header = new HBox(14, cover, text);
        header.setPadding(new Insets(0, 12, 8, 12));

        TableView<Display.TrackRow> table = new TableView<>();
        table.setColumnResizePolicy(TableView.CONSTRAINED_RESIZE_POLICY_FLEX_LAST_COLUMN);
        table.setPlaceholder(new Label("Este álbum no tiene pistas."));
        table.getColumns().add(column("Nº", 60, Display.TrackRow::number, Pos.CENTER_RIGHT));
        TableColumn<Display.TrackRow, String> titles = column("Título", 320, Display.TrackRow::title, Pos.CENTER_LEFT);
        titles.setCellFactory(c -> new TableCell<>() {
            @Override
            protected void updateItem(String text, boolean empty) {
                super.updateItem(text, empty);
                setText(empty ? null : text);
                setStyle(!empty && Display.UNNAMED.equals(text) ? "-fx-font-style: italic;" : "");
            }
        });
        table.getColumns().add(titles);
        table.getColumns().add(column("Duración", 90, Display.TrackRow::duration, Pos.CENTER_RIGHT));
        table.getColumns().add(column("Formato", 130, Display.TrackRow::format, Pos.CENTER_LEFT));
        table.getItems().setAll(Display.rows(album));
        // Las filas van en el mismo orden que las pistas del álbum.
        Runnable playSelected = () -> {
            int index = table.getSelectionModel().getSelectedIndex();
            if (index >= 0) {
                onPlay.accept(album.getTracks(), index);
            }
        };
        table.setOnMouseClicked(e -> {
            if (e.getClickCount() == 2) {
                playSelected.run();
            }
        });
        table.setOnKeyPressed(e -> {
            if (e.getCode() == javafx.scene.input.KeyCode.ENTER) {
                playSelected.run();
            }
        });
        VBox.setVgrow(table, Priority.ALWAYS);
        return new VBox(header, table);
    }

    private static TableColumn<Display.TrackRow, String> column(String name, double width,
            Function<Display.TrackRow, String> value, Pos alignment) {
        TableColumn<Display.TrackRow, String> column = new TableColumn<>(name);
        column.setPrefWidth(width);
        column.setSortable(false);
        column.setReorderable(false);
        column.setCellValueFactory(cell -> new ReadOnlyStringWrapper(value.apply(cell.getValue())));
        column.setStyle("-fx-alignment: " + (alignment == Pos.CENTER_RIGHT ? "CENTER-RIGHT" : "CENTER-LEFT") + ";");
        return column;
    }

    /** Un texto centrado si no hay nada que enseñar, o el contenido. */
    private static Node emptyOr(boolean empty, String text, Node content) {
        if (!empty) {
            return content;
        }
        Label label = new Label(text);
        VBox box = new VBox(label);
        box.setAlignment(Pos.CENTER);
        return box;
    }

    /**
     * Pide una pantalla al servidor y, cuando llega, la enseña y la apila.
     *
     * @param rootPage si la pantalla es una sección del menú: vacía la pila
     */
    private <T> void open(String pageTitle, boolean rootPage, Callable<T> load, Function<T, Node> build) {
        int mine = ++navigation;
        show(new Label("Cargando…"), pageTitle);
        background.run(load, result -> {
            if (mine != navigation) {
                return;
            }
            if (rootPage) {
                history.clear();
            }
            Page page = new Page(pageTitle, build.apply(result));
            history.push(page);
            show(page.content(), page.title());
        }, error -> {
            if (mine != navigation) {
                return;
            }
            if (error instanceof Catalog.SessionExpired) {
                onSessionExpired.run();
                return;
            }
            Label message = new Label(error instanceof IOException ? error.getMessage()
                    : "Error inesperado al cargar " + pageTitle + ": " + error);
            message.setWrapText(true);
            Button retry = new Button("Reintentar");
            retry.setOnAction(e -> open(pageTitle, rootPage, load, build));
            VBox box = new VBox(10, message, retry);
            box.setPadding(new Insets(16));
            show(box, pageTitle);
        });
    }

    private void goBack() {
        navigation++;
        // Si lo que se ve es un aviso de carga o de error, «Atrás» vuelve a la pantalla de debajo
        // sin sacarla de la pila; si se ve una página, hay que sacarla.
        if (history.peek() != null && showing == history.peek().content()) {
            history.pop();
        }
        Page previous = history.peek();
        if (previous != null) {
            show(previous.content(), previous.title());
        }
    }

    private void show(Node content, String pageTitle) {
        showing = content;
        title.setText(pageTitle);
        root.setCenter(content);
        boolean fromPage = history.peek() != null && history.peek().content() == content;
        back.setVisible(history.size() > (fromPage ? 1 : 0));
    }
}
