package io.github.mex040708afk.bitabit.core;

import io.github.mex040708afk.bitabit.core.Contract.Track;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Objects;
import java.util.Optional;
import java.util.OptionalInt;
import java.util.Random;

/**
 * La cola de reproducción: las pistas en el orden en que van a sonar y cuál suena
 * ahora (ADR-022).
 *
 * <p>Solo guarda el estado; no reproduce nada. El reproductor le pide la pista
 * siguiente con {@link #next()} cuando el usuario la pide, o con
 * {@link #onTrackEnded()} cuando la actual acaba sola: con {@link RepeatMode#ONE}
 * la segunda repite y la primera avanza.
 *
 * <p>Una misma pista puede estar varias veces en la cola. Todos los índices son
 * posiciones en el orden de reproducción, el que devuelve {@link #tracks()}.
 *
 * <p>No es segura entre hilos: se usa desde un solo hilo (el principal en Android)
 * o con la sincronización del que la use. Los avisos a los {@link Listener} llegan
 * en el mismo hilo, dentro de la llamada que cambió la cola.
 */
public final class PlayQueue {

    /** Lo que pasa al llegar al final de la pista o de la cola. */
    public enum RepeatMode {
        /** Al acabar la última pista, la cola se para. */
        OFF,
        /** Al acabar la última pista, vuelve a la primera. */
        ALL,
        /** La pista actual se repite al acabar; el usuario sí puede pasar a otra. */
        ONE
    }

    /** Recibe un aviso cada vez que cambian las pistas de la cola o la actual. */
    public interface Listener {
        /**
         * @param queueChanged   si cambiaron las pistas o su orden
         * @param currentChanged si la pista actual es otra (o ya no hay); mover la
         *                       actual de sitio no cuenta, ni volver a empezarla
         */
        void onChanged(boolean queueChanged, boolean currentChanged);
    }

    /**
     * Umbral de {@link #previous(long)}: pasado este punto de la pista, «anterior»
     * vuelve a empezarla en lugar de ir a la de antes, como hacen casi todos los
     * reproductores.
     */
    public static final long RESTART_THRESHOLD_MS = 3000;

    // Envoltorio con identidad propia: así la misma pista dos veces son dos entradas
    // distintas, y el orden original se puede seguir sin confundirlas.
    private static final class Entry {
        final Track track;

        Entry(Track track) {
            this.track = Objects.requireNonNull(track, "pista nula en la cola");
        }
    }

    private final Random random;
    private final List<Listener> listeners = new ArrayList<>();
    private List<Entry> order = new ArrayList<>();
    // El orden de antes de barajar; solo se usa con el aleatorio activado.
    private List<Entry> original = new ArrayList<>();
    private int current = -1;
    private boolean shuffled;
    private RepeatMode repeatMode = RepeatMode.OFF;

    /** Una cola vacía que baraja con un {@link Random} propio. */
    public PlayQueue() {
        this(new Random());
    }

    /** Una cola vacía que baraja con {@code random}; los tests le pasan uno con semilla. */
    public PlayQueue(Random random) {
        this.random = Objects.requireNonNull(random, "random");
    }

    public void addListener(Listener listener) {
        listeners.add(Objects.requireNonNull(listener, "listener"));
    }

    public void removeListener(Listener listener) {
        listeners.remove(listener);
    }

    /** Las pistas en el orden en que van a sonar. Es una copia que no se puede modificar. */
    public List<Track> tracks() {
        List<Track> result = new ArrayList<>(order.size());
        for (Entry entry : order) {
            result.add(entry.track);
        }
        return Collections.unmodifiableList(result);
    }

    public int size() {
        return order.size();
    }

    public boolean isEmpty() {
        return order.isEmpty();
    }

    /** La pista que suena ahora; vacía si la cola está vacía o ya terminó. */
    public Optional<Track> current() {
        return current < 0 ? Optional.empty() : Optional.of(order.get(current).track);
    }

    /** La posición de la pista actual en {@link #tracks()}; vacía si no hay. */
    public OptionalInt currentIndex() {
        return current < 0 ? OptionalInt.empty() : OptionalInt.of(current);
    }

    public RepeatMode repeatMode() {
        return repeatMode;
    }

    public void setRepeatMode(RepeatMode mode) {
        repeatMode = Objects.requireNonNull(mode, "mode");
    }

    public boolean isShuffled() {
        return shuffled;
    }

    /**
     * Cambia toda la cola por {@code tracks} y hace actual la de {@code startIndex}.
     * Con el aleatorio activado, esa pista queda la primera y el resto se baraja.
     *
     * @throws IllegalArgumentException si la lista está vacía (para vaciar la cola,
     *                                  {@link #clear()})
     * @throws IndexOutOfBoundsException si {@code startIndex} no es de la lista
     */
    public void setTracks(List<Track> tracks, int startIndex) {
        Objects.requireNonNull(tracks, "tracks");
        if (tracks.isEmpty()) {
            throw new IllegalArgumentException("la lista de pistas está vacía; para vaciar la cola, clear()");
        }
        checkIndex(startIndex, tracks.size());
        Entry before = currentEntry();
        List<Entry> entries = wrap(tracks);
        order = entries;
        current = startIndex;
        if (shuffled) {
            original = new ArrayList<>(entries);
            shuffleKeepingCurrentFirst();
        }
        notifyChanged(true, before);
    }

    /** Hace actual la pista de {@code index} y la devuelve. */
    public Track playAt(int index) {
        checkIndex(index, order.size());
        Entry before = currentEntry();
        current = index;
        notifyChanged(false, before);
        return order.get(current).track;
    }

    /**
     * Pasa a la siguiente porque el usuario lo pide. Con {@link RepeatMode#ONE}
     * también avanza; al final de la cola solo vuelve a la primera con
     * {@link RepeatMode#ALL}.
     *
     * @return la nueva pista actual, o vacío si no hay siguiente (la actual no cambia)
     */
    public Optional<Track> next() {
        if (current < 0) {
            return Optional.empty();
        }
        int target;
        if (current + 1 < order.size()) {
            target = current + 1;
        } else if (repeatMode == RepeatMode.ALL) {
            target = 0;
        } else {
            return Optional.empty();
        }
        return Optional.of(playAt(target));
    }

    /**
     * Pasa a la siguiente porque la actual acabó sola. Con {@link RepeatMode#ONE}
     * devuelve la misma, para que vuelva a sonar; si no, hace lo mismo que {@link #next()}.
     *
     * @return la pista que debe sonar, o vacío si la cola terminó
     */
    public Optional<Track> onTrackEnded() {
        if (current >= 0 && repeatMode == RepeatMode.ONE) {
            return current();
        }
        return next();
    }

    /**
     * Va a la anterior. Si la actual lleva sonando más de {@link #RESTART_THRESHOLD_MS},
     * o es la primera y no hay {@link RepeatMode#ALL}, devuelve la misma: el
     * reproductor la empieza de nuevo.
     *
     * @param positionMs por dónde va la pista actual
     * @return la pista que debe sonar desde el principio, o vacío si no hay actual
     */
    public Optional<Track> previous(long positionMs) {
        if (current < 0) {
            return Optional.empty();
        }
        if (positionMs > RESTART_THRESHOLD_MS) {
            return current();
        }
        if (current > 0) {
            return Optional.of(playAt(current - 1));
        }
        if (repeatMode == RepeatMode.ALL) {
            return Optional.of(playAt(order.size() - 1));
        }
        return current();
    }

    /** Añade {@code tracks} al final de la cola. La actual no cambia. */
    public void addLast(List<Track> tracks) {
        List<Entry> entries = wrap(Objects.requireNonNull(tracks, "tracks"));
        if (entries.isEmpty()) {
            return;
        }
        Entry before = currentEntry();
        order.addAll(entries);
        if (shuffled) {
            original.addAll(entries);
        }
        notifyChanged(true, before);
    }

    /**
     * Añade {@code tracks} justo después de la actual, en ese orden, para que suenen
     * a continuación. Si no hay actual, las pone al principio.
     */
    public void addNext(List<Track> tracks) {
        List<Entry> entries = wrap(Objects.requireNonNull(tracks, "tracks"));
        if (entries.isEmpty()) {
            return;
        }
        Entry before = currentEntry();
        order.addAll(current + 1, entries);
        if (shuffled) {
            int originalIndex = before == null ? -1 : original.indexOf(before);
            original.addAll(originalIndex + 1, entries);
        }
        notifyChanged(true, before);
    }

    /**
     * Quita la pista de {@code index}. Si era la actual, pasa a serlo la que la seguía;
     * si era la última, la primera con {@link RepeatMode#ALL} o ninguna sin él.
     *
     * @return la pista quitada
     */
    public Track remove(int index) {
        checkIndex(index, order.size());
        Entry before = currentEntry();
        Entry removed = order.remove(index);
        if (shuffled) {
            original.remove(removed);
        }
        if (index < current) {
            current--;
        } else if (index == current && current == order.size()) {
            current = repeatMode == RepeatMode.ALL && !order.isEmpty() ? 0 : -1;
        }
        notifyChanged(true, before);
        return removed.track;
    }

    /**
     * Mueve la pista de {@code from} a {@code to}; las de en medio se desplazan un
     * puesto. La actual sigue siendo la misma pista, aunque cambie su índice.
     */
    public void move(int from, int to) {
        checkIndex(from, order.size());
        checkIndex(to, order.size());
        if (from == to) {
            return;
        }
        Entry before = currentEntry();
        order.add(to, order.remove(from));
        if (before != null) {
            current = order.indexOf(before);
        }
        notifyChanged(true, before);
    }

    /** Quita todas las pistas. Los modos de aleatorio y repetición se quedan como estaban. */
    public void clear() {
        if (order.isEmpty()) {
            return;
        }
        Entry before = currentEntry();
        order = new ArrayList<>();
        original = new ArrayList<>();
        current = -1;
        notifyChanged(true, before);
    }

    /**
     * Activa o desactiva el aleatorio. Al activarlo, la actual pasa a la primera
     * posición y el resto se baraja; al desactivarlo, vuelve el orden de antes,
     * con lo añadido y lo quitado entretanto. La actual sigue siendo la misma.
     */
    public void setShuffled(boolean shuffle) {
        if (shuffle == shuffled) {
            return;
        }
        Entry before = currentEntry();
        shuffled = shuffle;
        if (shuffle) {
            original = new ArrayList<>(order);
            shuffleKeepingCurrentFirst();
        } else {
            order = original;
            original = new ArrayList<>();
            current = before == null ? -1 : order.indexOf(before);
        }
        if (!order.isEmpty()) {
            notifyChanged(true, before);
        }
    }

    private void shuffleKeepingCurrentFirst() {
        Entry first = currentEntry();
        List<Entry> rest = new ArrayList<>(order);
        if (first != null) {
            rest.remove(current);
        }
        Collections.shuffle(rest, random);
        List<Entry> shuffledOrder = new ArrayList<>(order.size());
        if (first != null) {
            shuffledOrder.add(first);
            current = 0;
        }
        shuffledOrder.addAll(rest);
        order = shuffledOrder;
    }

    private Entry currentEntry() {
        return current < 0 ? null : order.get(current);
    }

    private void notifyChanged(boolean queueChanged, Entry before) {
        boolean currentChanged = before != currentEntry();
        if (!queueChanged && !currentChanged) {
            return;
        }
        // Copia: un listener puede quitarse a sí mismo durante el aviso.
        for (Listener listener : new ArrayList<>(listeners)) {
            listener.onChanged(queueChanged, currentChanged);
        }
    }

    private static List<Entry> wrap(List<Track> tracks) {
        List<Entry> entries = new ArrayList<>(tracks.size());
        for (Track track : tracks) {
            entries.add(new Entry(track));
        }
        return entries;
    }

    // Objects.checkIndex no existe en Android hasta la API 30.
    private static void checkIndex(int index, int size) {
        if (index < 0 || index >= size) {
            throw new IndexOutOfBoundsException("índice " + index + " fuera de la cola de " + size);
        }
    }
}
