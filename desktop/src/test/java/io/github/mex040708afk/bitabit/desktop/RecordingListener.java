package io.github.mex040708afk.bitabit.desktop;

import java.io.IOException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.OptionalDouble;
import java.util.function.Predicate;

/** Apunta cada aviso del Player como texto, para comparar y para esperar. */
class RecordingListener implements Player.Listener {

    private final List<String> calls = new ArrayList<>();
    private volatile OptionalDouble position = OptionalDouble.empty();
    private volatile OptionalDouble duration = OptionalDouble.empty();
    private volatile Optional<IOException> failure = Optional.empty();

    @Override
    public void onPosition(OptionalDouble seconds) {
        position = seconds;
        record("position " + text(seconds));
    }

    @Override
    public void onDuration(OptionalDouble seconds) {
        duration = seconds;
        record("duration " + text(seconds));
    }

    @Override
    public void onPause(boolean paused) {
        record("pause " + paused);
    }

    @Override
    public void onDecodedFormat(Optional<AudioParams> params) {
        record("decoded " + params.map(AudioParams::describe).orElse("-"));
    }

    @Override
    public void onOutputFormat(Optional<AudioParams> params) {
        record("output " + params.map(AudioParams::describe).orElse("-"));
    }

    @Override
    public void onTrackEnded(EndReason reason, Optional<String> error) {
        record("ended " + reason + error.map(e -> " " + e).orElse(""));
    }

    @Override
    public void onFailure(IOException error) {
        failure = Optional.of(error);
        record("failure");
    }

    synchronized List<String> calls() {
        return List.copyOf(calls);
    }

    OptionalDouble position() {
        return position;
    }

    OptionalDouble duration() {
        return duration;
    }

    Optional<IOException> failure() {
        return failure;
    }

    /** Espera a que llegue un aviso que cumpla la condición, y lo devuelve. */
    synchronized String await(Predicate<String> condition, Duration timeout) throws InterruptedException {
        long deadline = System.nanoTime() + timeout.toNanos();
        int seen = 0;
        while (true) {
            for (; seen < calls.size(); seen++) {
                if (condition.test(calls.get(seen))) {
                    return calls.get(seen);
                }
            }
            long left = deadline - System.nanoTime();
            if (left <= 0) {
                throw new AssertionError("no llegó el aviso esperado en " + timeout.toSeconds() + " s: " + calls);
            }
            wait(Math.max(1, left / 1_000_000));
        }
    }

    /** Olvida lo apuntado, para esperar solo lo que llegue después. */
    synchronized void clear() {
        calls.clear();
    }

    private synchronized void record(String call) {
        calls.add(call);
        notifyAll();
    }

    private static String text(OptionalDouble seconds) {
        return seconds.isPresent() ? Double.toString(seconds.getAsDouble()) : "-";
    }
}
