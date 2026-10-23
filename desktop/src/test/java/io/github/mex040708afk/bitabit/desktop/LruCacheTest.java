package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public class LruCacheTest {

    @Test
    public void neverHoldsMoreThanItsCapacity() {
        LruCache<Integer, String> cache = new LruCache<>(3);
        for (int i = 0; i < 10; i++) {
            cache.put(i, "v" + i);
        }
        assertEquals(3, cache.size());
        assertTrue(cache.get(9).isPresent());
        assertFalse(cache.get(0).isPresent());
    }

    @Test
    public void evictsTheLeastRecentlyUsed() {
        LruCache<Integer, String> cache = new LruCache<>(2);
        cache.put(1, "a");
        cache.put(2, "b");
        cache.get(1);
        cache.put(3, "c");

        assertTrue(cache.get(1).isPresent());
        assertFalse(cache.get(2).isPresent());
        assertTrue(cache.get(3).isPresent());
    }

    @Test
    public void replacingAKeyDoesNotGrowIt() {
        LruCache<Integer, String> cache = new LruCache<>(2);
        cache.put(1, "a");
        cache.put(1, "b");
        assertEquals(1, cache.size());
        assertEquals("b", cache.get(1).orElseThrow());
    }

    @Test(expected = IllegalArgumentException.class)
    public void rejectsACapacityOfZero() {
        new LruCache<Integer, String>(0);
    }
}
