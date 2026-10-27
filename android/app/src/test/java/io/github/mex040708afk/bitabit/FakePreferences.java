package io.github.mex040708afk.bitabit;

import android.content.SharedPreferences;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

/** Unas {@link SharedPreferences} en memoria, para probar en la JVM sin Android. */
final class FakePreferences implements SharedPreferences {

    final Map<String, Object> values = new HashMap<>();

    @Override
    public Map<String, ?> getAll() {
        return new HashMap<>(values);
    }

    @Override
    public String getString(String key, String defValue) {
        return values.containsKey(key) ? (String) values.get(key) : defValue;
    }

    @Override
    @SuppressWarnings("unchecked")
    public Set<String> getStringSet(String key, Set<String> defValues) {
        return values.containsKey(key) ? (Set<String>) values.get(key) : defValues;
    }

    @Override
    public int getInt(String key, int defValue) {
        return values.containsKey(key) ? (Integer) values.get(key) : defValue;
    }

    @Override
    public long getLong(String key, long defValue) {
        return values.containsKey(key) ? (Long) values.get(key) : defValue;
    }

    @Override
    public float getFloat(String key, float defValue) {
        return values.containsKey(key) ? (Float) values.get(key) : defValue;
    }

    @Override
    public boolean getBoolean(String key, boolean defValue) {
        return values.containsKey(key) ? (Boolean) values.get(key) : defValue;
    }

    @Override
    public boolean contains(String key) {
        return values.containsKey(key);
    }

    @Override
    public Editor edit() {
        return new FakeEditor();
    }

    @Override
    public void registerOnSharedPreferenceChangeListener(OnSharedPreferenceChangeListener listener) {
        throw new UnsupportedOperationException();
    }

    @Override
    public void unregisterOnSharedPreferenceChangeListener(OnSharedPreferenceChangeListener listener) {
        throw new UnsupportedOperationException();
    }

    private final class FakeEditor implements Editor {

        private final Map<String, Object> puts = new HashMap<>();
        private final Set<String> removes = new HashSet<>();
        private boolean clear;

        @Override
        public Editor putString(String key, String value) {
            puts.put(key, value);
            return this;
        }

        @Override
        public Editor putStringSet(String key, Set<String> values) {
            puts.put(key, values);
            return this;
        }

        @Override
        public Editor putInt(String key, int value) {
            puts.put(key, value);
            return this;
        }

        @Override
        public Editor putLong(String key, long value) {
            puts.put(key, value);
            return this;
        }

        @Override
        public Editor putFloat(String key, float value) {
            puts.put(key, value);
            return this;
        }

        @Override
        public Editor putBoolean(String key, boolean value) {
            puts.put(key, value);
            return this;
        }

        @Override
        public Editor remove(String key) {
            removes.add(key);
            return this;
        }

        @Override
        public Editor clear() {
            clear = true;
            return this;
        }

        @Override
        public boolean commit() {
            apply();
            return true;
        }

        @Override
        public void apply() {
            // El mismo orden que Android: primero clear, después las bajas y las altas.
            if (clear) {
                values.clear();
            }
            values.keySet().removeAll(removes);
            values.putAll(puts);
        }
    }
}
