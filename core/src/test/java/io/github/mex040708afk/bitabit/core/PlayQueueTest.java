package io.github.mex040708afk.bitabit.core;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import io.github.mex040708afk.bitabit.core.Contract.Track;
import io.github.mex040708afk.bitabit.core.PlayQueue.RepeatMode;
import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import org.junit.Before;
import org.junit.Test;

public class PlayQueueTest {

    private PlayQueue queue;
    private List<String> events;

    @Before
    public void setUp() {
        queue = new PlayQueue(new Random(42));
        events = new ArrayList<>();
        queue.addListener((queueChanged, currentChanged) -> events.add(queueChanged + "/" + currentChanged));
    }

    /** Una pista leída de un JSON del contrato, como la manda el servidor. */
    static Track track(long id) {
        return Json.fromJson("{\"id\":" + id + ",\"title\":\"Pista " + id + "\",\"artist\":\"Grupo\","
                + "\"album\":{\"id\":1,\"title\":\"Disco\"},\"albumArtist\":{\"id\":1,\"name\":\"Grupo\"},"
                + "\"discNumber\":1,\"trackNumber\":" + id + ",\"durationMs\":180000,\"files\":[]}", Track.class);
    }

    private static List<Track> tracks(int count) {
        List<Track> result = new ArrayList<>();
        for (int i = 1; i <= count; i++) {
            result.add(track(i));
        }
        return result;
    }

    private List<Long> ids() {
        List<Long> result = new ArrayList<>();
        for (Track track : queue.tracks()) {
            result.add(track.getId());
        }
        return result;
    }

    private long currentId() {
        return queue.current().get().getId();
    }

    @Test
    public void newQueueIsEmpty() {
        assertTrue(queue.isEmpty());
        assertEquals(0, queue.size());
        assertFalse(queue.current().isPresent());
        assertFalse(queue.currentIndex().isPresent());
        assertFalse(queue.next().isPresent());
        assertFalse(queue.onTrackEnded().isPresent());
        assertFalse(queue.previous(0).isPresent());
        assertEquals(RepeatMode.OFF, queue.repeatMode());
        assertFalse(queue.isShuffled());
        assertTrue(events.isEmpty());
    }

    @Test
    public void emptyQueueRejectsIndexes() {
        assertThrows(IndexOutOfBoundsException.class, () -> queue.playAt(0));
        assertThrows(IndexOutOfBoundsException.class, () -> queue.remove(0));
        assertThrows(IndexOutOfBoundsException.class, () -> queue.move(0, 0));
    }

    @Test
    public void setTracksStartsAtTheGivenIndex() {
        queue.setTracks(tracks(3), 1);
        assertEquals(List.of(1L, 2L, 3L), ids());
        assertEquals(2, currentId());
        assertEquals(1, queue.currentIndex().getAsInt());
        assertEquals(List.of("true/true"), events);
    }

    @Test
    public void setTracksRejectsEmptyListAndBadIndex() {
        assertThrows(IllegalArgumentException.class, () -> queue.setTracks(List.of(), 0));
        assertThrows(IndexOutOfBoundsException.class, () -> queue.setTracks(tracks(2), 2));
        assertThrows(IndexOutOfBoundsException.class, () -> queue.setTracks(tracks(2), -1));
        assertThrows(NullPointerException.class, () -> queue.setTracks(null, 0));
        assertTrue(queue.isEmpty());
    }

    @Test
    public void setTracksRejectsNullTrack() {
        List<Track> list = new ArrayList<>(tracks(1));
        list.add(null);
        assertThrows(NullPointerException.class, () -> queue.setTracks(list, 0));
        assertTrue(queue.isEmpty());
    }

    @Test
    public void tracksIsAnUnmodifiableCopy() {
        List<Track> input = new ArrayList<>(tracks(2));
        queue.setTracks(input, 0);
        input.clear();
        List<Track> out = queue.tracks();
        assertEquals(2, out.size());
        assertThrows(UnsupportedOperationException.class, () -> out.add(track(9)));
        queue.addLast(List.of(track(3)));
        assertEquals(2, out.size());
    }

    @Test
    public void nextAdvancesAndStopsAtTheEndWithoutRepeat() {
        queue.setTracks(tracks(2), 0);
        assertEquals(2, queue.next().get().getId());
        assertFalse(queue.next().isPresent());
        assertEquals(2, currentId());
        assertFalse(queue.onTrackEnded().isPresent());
        assertEquals(2, currentId());
    }

    @Test
    public void repeatAllWrapsAtTheEnd() {
        queue.setTracks(tracks(2), 1);
        queue.setRepeatMode(RepeatMode.ALL);
        assertEquals(1, queue.next().get().getId());
        queue.playAt(1);
        assertEquals(1, queue.onTrackEnded().get().getId());
    }

    @Test
    public void repeatAllWithOneTrackReplaysIt() {
        queue.setTracks(tracks(1), 0);
        queue.setRepeatMode(RepeatMode.ALL);
        events.clear();
        assertEquals(1, queue.next().get().getId());
        assertTrue(events.isEmpty());
    }

    @Test
    public void repeatOneRepeatsOnlyWhenTheTrackEnds() {
        queue.setTracks(tracks(3), 0);
        queue.setRepeatMode(RepeatMode.ONE);
        assertEquals(1, queue.onTrackEnded().get().getId());
        assertEquals(0, queue.currentIndex().getAsInt());
        assertEquals(2, queue.next().get().getId());
    }

    @Test
    public void repeatOneDoesNotWrapWhenTheUserSkipsTheLast() {
        queue.setTracks(tracks(2), 1);
        queue.setRepeatMode(RepeatMode.ONE);
        assertFalse(queue.next().isPresent());
        assertEquals(2, queue.onTrackEnded().get().getId());
    }

    @Test
    public void previousRestartsAfterThreeSeconds() {
        queue.setTracks(tracks(3), 2);
        events.clear();
        assertEquals(3, queue.previous(3001).get().getId());
        assertEquals(2, queue.currentIndex().getAsInt());
        assertTrue(events.isEmpty());
    }

    @Test
    public void previousGoesBackWithinThreeSeconds() {
        queue.setTracks(tracks(3), 2);
        assertEquals(2, queue.previous(3000).get().getId());
        assertEquals(1, queue.previous(0).get().getId());
    }

    @Test
    public void previousOnTheFirstRestartsItWithoutRepeat() {
        queue.setTracks(tracks(3), 0);
        assertEquals(1, queue.previous(0).get().getId());
        assertEquals(0, queue.currentIndex().getAsInt());
    }

    @Test
    public void previousOnTheFirstGoesToTheLastWithRepeatAll() {
        queue.setTracks(tracks(3), 0);
        queue.setRepeatMode(RepeatMode.ALL);
        assertEquals(3, queue.previous(0).get().getId());
    }

    @Test
    public void playAtChangesTheCurrentAndNotifies() {
        queue.setTracks(tracks(3), 0);
        events.clear();
        assertEquals(3, queue.playAt(2).getId());
        assertEquals(List.of("false/true"), events);
        events.clear();
        queue.playAt(2);
        assertTrue(events.isEmpty());
    }

    @Test
    public void addLastAppendsWithoutChangingTheCurrent() {
        queue.setTracks(tracks(2), 0);
        events.clear();
        queue.addLast(List.of(track(7), track(8)));
        assertEquals(List.of(1L, 2L, 7L, 8L), ids());
        assertEquals(1, currentId());
        assertEquals(List.of("true/false"), events);
    }

    @Test
    public void addLastToAnEmptyQueueLeavesNoCurrent() {
        queue.addLast(List.of(track(7)));
        assertEquals(1, queue.size());
        assertFalse(queue.current().isPresent());
        assertEquals(7, queue.playAt(0).getId());
    }

    @Test
    public void addingNothingDoesNotNotify() {
        queue.setTracks(tracks(2), 0);
        events.clear();
        queue.addLast(List.of());
        queue.addNext(List.of());
        assertTrue(events.isEmpty());
    }

    @Test
    public void addNextInsertsAfterTheCurrent() {
        queue.setTracks(tracks(3), 1);
        queue.addNext(List.of(track(7), track(8)));
        assertEquals(List.of(1L, 2L, 7L, 8L, 3L), ids());
        assertEquals(2, currentId());
        assertEquals(7, queue.next().get().getId());
    }

    @Test
    public void addNextWithoutCurrentInsertsAtTheStart() {
        queue.addLast(List.of(track(1)));
        queue.addNext(List.of(track(7)));
        assertEquals(List.of(7L, 1L), ids());
    }

    @Test
    public void sameTrackCanBeQueuedTwice() {
        Track same = track(5);
        queue.setTracks(List.of(same, same), 0);
        assertSame(same, queue.remove(1));
        assertEquals(1, queue.size());
        assertEquals(0, queue.currentIndex().getAsInt());
    }

    @Test
    public void removeBeforeTheCurrentKeepsIt() {
        queue.setTracks(tracks(4), 2);
        events.clear();
        assertEquals(1, queue.remove(0).getId());
        assertEquals(3, currentId());
        assertEquals(1, queue.currentIndex().getAsInt());
        assertEquals(List.of("true/false"), events);
    }

    @Test
    public void removeAfterTheCurrentKeepsIt() {
        queue.setTracks(tracks(4), 1);
        queue.remove(3);
        assertEquals(2, currentId());
        assertEquals(List.of(1L, 2L, 3L), ids());
    }

    @Test
    public void removeTheCurrentMovesToTheFollowing() {
        queue.setTracks(tracks(3), 1);
        events.clear();
        assertEquals(2, queue.remove(1).getId());
        assertEquals(3, currentId());
        assertEquals(List.of("true/true"), events);
    }

    @Test
    public void removeTheCurrentLastLeavesNoCurrentWithoutRepeat() {
        queue.setTracks(tracks(3), 2);
        queue.remove(2);
        assertFalse(queue.current().isPresent());
        assertFalse(queue.next().isPresent());
        assertEquals(2, queue.size());
    }

    @Test
    public void removeTheCurrentLastWrapsWithRepeatAll() {
        queue.setTracks(tracks(3), 2);
        queue.setRepeatMode(RepeatMode.ALL);
        queue.remove(2);
        assertEquals(1, currentId());
    }

    @Test
    public void removeTheOnlyTrackEmptiesTheQueue() {
        queue.setTracks(tracks(1), 0);
        queue.setRepeatMode(RepeatMode.ALL);
        queue.remove(0);
        assertTrue(queue.isEmpty());
        assertFalse(queue.current().isPresent());
    }

    @Test
    public void moveTheCurrentKeepsItCurrent() {
        queue.setTracks(tracks(4), 0);
        events.clear();
        queue.move(0, 3);
        assertEquals(List.of(2L, 3L, 4L, 1L), ids());
        assertEquals(1, currentId());
        assertEquals(3, queue.currentIndex().getAsInt());
        assertEquals(List.of("true/false"), events);
    }

    @Test
    public void moveAcrossTheCurrentShiftsItsIndex() {
        queue.setTracks(tracks(4), 2);
        queue.move(3, 0);
        assertEquals(List.of(4L, 1L, 2L, 3L), ids());
        assertEquals(3, currentId());
        assertEquals(3, queue.currentIndex().getAsInt());
        queue.move(0, 3);
        assertEquals(2, queue.currentIndex().getAsInt());
    }

    @Test
    public void moveToTheSamePlaceDoesNothing() {
        queue.setTracks(tracks(2), 0);
        events.clear();
        queue.move(1, 1);
        assertTrue(events.isEmpty());
        assertThrows(IndexOutOfBoundsException.class, () -> queue.move(0, 2));
    }

    @Test
    public void clearEmptiesButKeepsModes() {
        queue.setTracks(tracks(3), 1);
        queue.setRepeatMode(RepeatMode.ALL);
        queue.setShuffled(true);
        events.clear();
        queue.clear();
        assertTrue(queue.isEmpty());
        assertFalse(queue.current().isPresent());
        assertEquals(RepeatMode.ALL, queue.repeatMode());
        assertTrue(queue.isShuffled());
        assertEquals(List.of("true/true"), events);
        events.clear();
        queue.clear();
        assertTrue(events.isEmpty());
    }

    @Test
    public void shufflePutsTheCurrentFirstAndKeepsAllTracks() {
        queue.setTracks(tracks(10), 4);
        events.clear();
        queue.setShuffled(true);
        assertTrue(queue.isShuffled());
        assertEquals(5, currentId());
        assertEquals(0, queue.currentIndex().getAsInt());
        List<Long> shuffled = ids();
        assertEquals(10, shuffled.size());
        assertEquals(10, new java.util.HashSet<>(shuffled).size());
        assertNotEquals(List.of(5L, 1L, 2L, 3L, 4L, 6L, 7L, 8L, 9L, 10L), shuffled);
        assertEquals(List.of("true/false"), events);
    }

    @Test
    public void unshuffleRestoresTheOriginalOrderAndKeepsTheCurrent() {
        queue.setTracks(tracks(10), 4);
        queue.setShuffled(true);
        queue.playAt(6);
        long playing = currentId();
        queue.setShuffled(false);
        assertFalse(queue.isShuffled());
        assertEquals(List.of(1L, 2L, 3L, 4L, 5L, 6L, 7L, 8L, 9L, 10L), ids());
        assertEquals(playing, currentId());
        assertEquals(playing - 1, queue.currentIndex().getAsInt());
    }

    @Test
    public void shuffleIsDeterministicWithASeed() {
        PlayQueue other = new PlayQueue(new Random(42));
        other.setTracks(tracks(10), 0);
        other.setShuffled(true);
        queue.setTracks(tracks(10), 0);
        queue.setShuffled(true);
        List<Long> otherIds = new ArrayList<>();
        for (Track track : other.tracks()) {
            otherIds.add(track.getId());
        }
        assertEquals(otherIds, ids());
    }

    @Test
    public void shuffleWithOneTrack() {
        queue.setTracks(tracks(1), 0);
        queue.setShuffled(true);
        assertEquals(List.of(1L), ids());
        assertEquals(1, currentId());
        queue.setShuffled(false);
        assertEquals(List.of(1L), ids());
        assertEquals(1, currentId());
    }

    @Test
    public void shuffleOnAnEmptyQueueDoesNotNotify() {
        queue.setShuffled(true);
        assertTrue(queue.isShuffled());
        queue.setShuffled(false);
        assertTrue(events.isEmpty());
    }

    @Test
    public void shuffleTwiceDoesNothing() {
        queue.setTracks(tracks(5), 0);
        queue.setShuffled(true);
        List<Long> once = ids();
        events.clear();
        queue.setShuffled(true);
        assertEquals(once, ids());
        assertTrue(events.isEmpty());
    }

    @Test
    public void shuffleWithoutCurrentShufflesEverything() {
        queue.addLast(tracks(5));
        queue.setShuffled(true);
        assertFalse(queue.current().isPresent());
        assertEquals(5, queue.size());
        queue.setShuffled(false);
        assertEquals(List.of(1L, 2L, 3L, 4L, 5L), ids());
    }

    @Test
    public void setTracksWhileShuffledKeepsTheStartFirstAndRemembersTheOrder() {
        queue.setShuffled(true);
        queue.setTracks(tracks(6), 3);
        assertEquals(4, currentId());
        assertEquals(0, queue.currentIndex().getAsInt());
        queue.setShuffled(false);
        assertEquals(List.of(1L, 2L, 3L, 4L, 5L, 6L), ids());
        assertEquals(3, queue.currentIndex().getAsInt());
    }

    @Test
    public void changesWhileShuffledReachTheOriginalOrder() {
        queue.setTracks(tracks(5), 2);
        queue.setShuffled(true);
        queue.addLast(List.of(track(9)));
        queue.addNext(List.of(track(8)));
        int indexOfFour = ids().indexOf(4L);
        queue.remove(indexOfFour);
        assertEquals(8, queue.next().get().getId());
        queue.setShuffled(false);
        assertEquals(List.of(1L, 2L, 3L, 8L, 5L, 9L), ids());
        assertEquals(8, currentId());
    }

    @Test
    public void removedListenerGetsNoMoreEvents() {
        List<String> other = new ArrayList<>();
        PlayQueue.Listener listener = (q, c) -> other.add("x");
        queue.addListener(listener);
        queue.setTracks(tracks(2), 0);
        queue.removeListener(listener);
        queue.next();
        assertEquals(1, other.size());
        assertEquals(2, events.size());
    }

    @Test
    public void listenerCanRemoveItselfDuringTheEvent() {
        PlayQueue.Listener[] self = new PlayQueue.Listener[1];
        List<String> other = new ArrayList<>();
        self[0] = (q, c) -> {
            other.add("x");
            queue.removeListener(self[0]);
        };
        queue.addListener(self[0]);
        queue.setTracks(tracks(2), 0);
        queue.next();
        assertEquals(1, other.size());
    }

    @Test
    public void listenerErrorsPropagate() {
        queue.addListener((q, c) -> {
            throw new IllegalStateException("fallo del listener");
        });
        assertThrows(IllegalStateException.class, () -> queue.setTracks(tracks(1), 0));
    }

    @Test
    public void tracksAreTheSameObjects() {
        Track one = track(1);
        queue.setTracks(List.of(one), 0);
        assertSame(one, queue.current().get());
    }
}
