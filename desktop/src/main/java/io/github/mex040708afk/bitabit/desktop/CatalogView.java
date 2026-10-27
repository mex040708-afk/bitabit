package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.BitabitClient.AlbumSort;
import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import io.github.mex040708afk.bitabit.core.Contract.Artist;
import io.github.mex040708afk.bitabit.core.Contract.ArtistSummary;
import io.github.mex040708afk.bitabit.core.Contract.SearchResult;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import java.io.IOException;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.function.BiConsumer;
import java.util.function.Consumer;
import java.util.function.Function;
import javafx.animation.PauseTransition;
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
import javafx.scene.control.Tab;
import javafx.scene.control.TabPane;
import javafx.scene.control.TableCell;
import javafx.scene.control.TableColumn;
import javafx.scene.control.TableView;
import javafx.scene.control.TextField;
import javafx.scene.input.KeyCode;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.HBox;
import javafx.scene.layout.Priority;
import javafx.scene.layout.StackPane;
import javafx.scene.layout.VBox;
import javafx.util.Duration;

/**
 * El catálogo: artistas, álbumes de un artista, todos los álbumes, las pistas de un álbum y
 * la búsqueda (ADR-026). Cada pantalla se pide al servidor en segundo plano y queda guardada en una pila,
 * así «Atrás» vuelve sin recargar y con el desplazamiento donde estaba. Solo se toca desde
 * el hilo de JavaFX.
 */
final class CatalogView {

    private record Page(String title, Node content) {
    }

    private static final String SEARCH_HINT = "Escribe para buscar artistas, álbumes y canciones.";

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

    /** La página de búsqueda se crea una vez: al volver a ella siguen el texto y los resultados. */
    private final TextField searchField = new TextField();
    private final StackPane searchResults = new StackPane();
    private final VBox searchPage = new VBox(8, searchField, searchResults);
    /** Espera a que se deje de escribir antes de preguntar al servidor. */
    private final PauseTransition searchPause = new PauseTransition(Duration.millis(300));
    /** Como {@link #navigation}, pero para las búsquedas: solo vale la respuesta de la última. */
    private int searchNumber;

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

        searchField.setPromptText("Artista, álbum o canción");
        searchField.textProperty().addListener((obs, old, text) -> searchPause.playFromStart());
        searchPause.setOnFinished(e -> search());
        searchField.setOnAction(e -> {
            searchPause.stop();
            search();
        });
        searchPage.setPadding(new Insets(0, 12, 12, 12));
        VBox.setVgrow(searchResults, Priority.ALWAYS);
        searchResults.getChildren().setAll(hint(SEARCH_HINT));
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

    void showSearch() {
        navigation++;
        history.clear();
        Page page = new Page("Buscar", searchPage);
        history.push(page);
        show(page.content(), page.title());
        searchField.requestFocus();
    }

    private void search() {
        int mine = ++searchNumber;
        switch (Catalog.query(searchField.getText())) {
            case Catalog.Query.Empty empty -> results(hint(SEARCH_HINT));
            case Catalog.Query.TooLong tooLong -> results(hint("La búsqueda tiene " + tooLong.chars()
                    + " caracteres y el máximo es " + Catalog.MAX_QUERY_CHARS + "."));
            case Catalog.Query.Text query -> {
                results(hint("Buscando…"));
                background.run(() -> catalog.search(query.text()), result -> {
                    if (mine == searchNumber) {
                        results(found(query.text(), result));
                    }
                }, error -> {
                    if (mine != searchNumber) {
                        return;
                    }
                    if (error instanceof Catalog.SessionExpired) {
                        onSessionExpired.run();
                        return;
                    }
                    results(failure(error instanceof IOException ? error.getMessage()
                            : "Error inesperado al buscar: " + error, this::search));
                });
            }
        }
    }

    private void results(Node content) {
        searchResults.getChildren().setAll(content);
    }

    /** Una pestaña por tipo, solo para los que traen algo, empezando por la primera que tenga. */
    private Node found(String text, SearchResult result) {
        TabPane tabs = new TabPane();
        tabs.setTabClosingPolicy(TabPane.TabClosingPolicy.UNAVAILABLE);
        if (!result.getArtists().isEmpty()) {
            tabs.getTabs().add(tab("Artistas", result.getArtists().size(), artistList(result.getArtists())));
        }
        if (!result.getAlbums().isEmpty()) {
            AlbumGrid grid = new AlbumGrid(covers, true, this::showAlbum);
            grid.setAlbums(result.getAlbums());
            tabs.getTabs().add(tab("Álbumes", result.getAlbums().size(), grid));
        }
        if (!result.getTracks().isEmpty()) {
            tabs.getTabs().add(tab("Pistas", result.getTracks().size(), foundTracks(result.getTracks())));
        }
        return tabs.getTabs().isEmpty() ? hint("No se ha encontrado nada para «" + text + "».") : tabs;
    }

    private static Tab tab(String name, int count, Node content) {
        return new Tab(name + " (" + count + ")", content);
    }

    /** Una pista abre su álbum (doble clic o Intro), mientras no se pueda reproducir desde aquí. */
    private Node foundTracks(List<Track> tracks) {
        TableView<Track> table = new TableView<>();
        table.setColumnResizePolicy(TableView.CONSTRAINED_RESIZE_POLICY_FLEX_LAST_COLUMN);
        table.getColumns().add(column("Título", 280, (Track t) -> Display.foundRow(t).title(), Pos.CENTER_LEFT));
        table.getColumns().add(column("Artista", 180, (Track t) -> Display.foundRow(t).artist(), Pos.CENTER_LEFT));
        table.getColumns().add(column("Álbum", 200, (Track t) -> Display.foundRow(t).album(), Pos.CENTER_LEFT));
        table.getColumns().add(column("Duración", 90, (Track t) -> Display.foundRow(t).duration(), Pos.CENTER_RIGHT));
        table.getItems().setAll(tracks);
        Consumer<Track> open = track -> {
            if (track != null && track.getAlbum() != null) {
                showAlbum(track.getAlbum().getId(), track.getAlbum().getTitle());
            }
        };
        table.setOnMouseClicked(e -> {
            if (e.getClickCount() == 2) {
                open.accept(table.getSelectionModel().getSelectedItem());
            }
        });
        table.setOnKeyPressed(e -> {
            if (e.getCode() == KeyCode.ENTER) {
                open.accept(table.getSelectionModel().getSelectedItem());
            }
        });
        return table;
    }

    private void showArtist(ArtistSummary summary) {
        open(Display.name(summary.getName()), false, () -> catalog.artist(summary.getId()), (Artist artist) -> {
            AlbumGrid grid = new AlbumGrid(covers, false, this::showAlbum);
            grid.setAlbums(artist.getAlbums());
            return emptyOr(artist.getAlbums().isEmpty(), "Este artista no tiene álbumes.", grid);
        });
    }

    private void showAlbum(AlbumSummary summary) {
        showAlbum(summary.getId(), summary.getTitle());
    }

    private void showAlbum(long albumId, String albumTitle) {
        open(Display.name(albumTitle), false, () -> catalog.album(albumId), this::albumDetail);
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
            if (e.getCode() == KeyCode.ENTER) {
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
            if (e.getCode() == KeyCode.ENTER) {
                playSelected.run();
            }
        });
        VBox.setVgrow(table, Priority.ALWAYS);
        return new VBox(header, table);
    }

    private static <R> TableColumn<R, String> column(String name, double width, Function<R, String> value,
            Pos alignment) {
        TableColumn<R, String> column = new TableColumn<>(name);
        column.setPrefWidth(width);
        column.setSortable(false);
        column.setReorderable(false);
        column.setCellValueFactory(cell -> new ReadOnlyStringWrapper(value.apply(cell.getValue())));
        column.setStyle("-fx-alignment: " + (alignment == Pos.CENTER_RIGHT ? "CENTER-RIGHT" : "CENTER-LEFT") + ";");
        return column;
    }

    /** Un texto centrado si no hay nada que enseñar, o el contenido. */
    private static Node emptyOr(boolean empty, String text, Node content) {
        return empty ? hint(text) : content;
    }

    private static Node hint(String text) {
        Label label = new Label(text);
        label.setWrapText(true);
        VBox box = new VBox(label);
        box.setAlignment(Pos.CENTER);
        return box;
    }

    /** El error con su contexto y un botón para volver a intentarlo. */
    private static Node failure(String text, Runnable retry) {
        Label message = new Label(text);
        message.setWrapText(true);
        Button again = new Button("Reintentar");
        again.setOnAction(e -> retry.run());
        VBox box = new VBox(10, message, again);
        box.setPadding(new Insets(16));
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
            show(failure(error instanceof IOException ? error.getMessage()
                    : "Error inesperado al cargar " + pageTitle + ": " + error,
                    () -> open(pageTitle, rootPage, load, build)), pageTitle);
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
