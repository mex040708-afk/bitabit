package io.github.mex040708afk.bitabit.desktop;

import java.util.concurrent.Callable;
import java.util.function.Consumer;

/**
 * Hace {@code task} fuera del hilo de JavaFX y vuelve a él con el resultado o con el error
 * (ADR-026). {@code done} y {@code failed} se llaman siempre en el hilo de JavaFX.
 */
@FunctionalInterface
interface Background {

    <T> void run(Callable<T> task, Consumer<T> done, Consumer<Throwable> failed);
}
