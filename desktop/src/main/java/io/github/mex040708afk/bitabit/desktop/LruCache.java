package io.github.mex040708afk.bitabit.desktop;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Optional;

/**
 * Una caché con tope: al pasarse echa lo que hace más tiempo que no se usa. Leer una
 * clave la renueva. No es segura entre hilos; las carátulas solo la tocan desde el hilo de JavaFX.
 */
final class LruCache<K, V> {

    private final Map<K, V> entries;

    LruCache(int capacity) {
        if (capacity < 1) {
            throw new IllegalArgumentException("el tope tiene que ser 1 o más: " + capacity);
        }
        entries = new LinkedHashMap<>(16, 0.75f, true) {
            private static final long serialVersionUID = 1L;

            @Override
            protected boolean removeEldestEntry(Map.Entry<K, V> eldest) {
                return size() > capacity;
            }
        };
    }

    Optional<V> get(K key) {
        return Optional.ofNullable(entries.get(key));
    }

    void put(K key, V value) {
        entries.put(key, value);
    }

    int size() {
        return entries.size();
    }
}
