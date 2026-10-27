package io.github.mex040708afk.bitabit;

import android.os.Handler;
import android.os.Looper;
import android.util.Log;
import androidx.lifecycle.Lifecycle;
import androidx.lifecycle.LifecycleOwner;
import io.github.mex040708afk.bitabit.core.BitabitClient;
import io.github.mex040708afk.bitabit.core.Contract;
import java.io.IOException;
import java.security.GeneralSecurityException;
import java.util.Optional;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Consumer;

/**
 * Lo que comparten las pantallas (ADR-023): la sesión abierta con su {@link BitabitClient},
 * el ejecutor donde se hacen las llamadas a la API y el {@link Handler} del hilo principal
 * para volver con el resultado. Sin ViewModel ni LiveData.
 */
public final class Services {

    /** Hilos del ejecutor: deja cargar a la vez, por ejemplo, una lista y sus carátulas. */
    private static final int THREADS = 4;

    private final ExecutorService executor;
    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final TokenStore tokens;
    // La escriben los hilos del ejecutor (al iniciar o retomar la sesión) y la lee la interfaz.
    private volatile Optional<ActiveSession> session = Optional.empty();

    Services(TokenStore tokens) {
        this.tokens = tokens;
        AtomicInteger count = new AtomicInteger();
        this.executor = Executors.newFixedThreadPool(THREADS,
                work -> new Thread(work, "bitabit-api-" + count.incrementAndGet()));
    }

    /** El ejecutor de la app, para las llamadas que no encajen en {@link #run}. */
    public ExecutorService executor() {
        return executor;
    }

    /** El {@link Handler} del hilo principal. */
    public Handler mainHandler() {
        return mainHandler;
    }

    /** La sesión abierta, si la hay. */
    public Optional<ActiveSession> session() {
        return session;
    }

    /** El último servidor usado, para rellenar el inicio de sesión. */
    Optional<String> lastServer() {
        return tokens.lastServer();
    }

    /**
     * Hace {@code work} en el ejecutor y entrega el resultado o el error en el hilo
     * principal. Si {@code owner} ya se ha destruido cuando llega, no se entrega: la
     * pantalla que lo pidió ya no existe.
     */
    public <T> void run(LifecycleOwner owner, Callable<T> work, Consumer<T> onResult, Consumer<Exception> onError) {
        executor.execute(() -> {
            try {
                T result = work.call();
                deliver(owner, () -> onResult.accept(result));
            } catch (Exception e) {
                // Ni el token ni la contraseña van en los mensajes de core ni en los de aquí.
                Log.w(BitabitApp.TAG, "falló una tarea en segundo plano: " + e);
                deliver(owner, () -> onError.accept(e));
            }
        });
    }

    private void deliver(LifecycleOwner owner, Runnable callback) {
        mainHandler.post(() -> {
            if (owner.getLifecycle().getCurrentState() != Lifecycle.State.DESTROYED) {
                callback.run();
            }
        });
    }

    /**
     * Retoma la sesión guardada, si la hay y sigue siendo legible. No comprueba con el
     * servidor que el token valga: eso lo dice la primera llamada, con un 401. Se llama
     * desde el ejecutor, porque descifra con el Keystore.
     */
    Optional<ActiveSession> restore() {
        Optional<TokenStore.SavedSession> saved = tokens.load();
        if (!saved.isPresent()) {
            return Optional.empty();
        }
        TokenStore.SavedSession s = saved.get();
        BitabitClient client;
        try {
            client = new BitabitClient(s.server());
            client.useToken(s.token());
        } catch (IllegalArgumentException e) {
            Log.w(BitabitApp.TAG, "la sesión guardada no es válida, se borra: " + e.getMessage());
            tokens.clear();
            return Optional.empty();
        }
        ActiveSession active = new ActiveSession(s.server(), s.sessionId(), client);
        session = Optional.of(active);
        return session;
    }

    /**
     * Inicia sesión y la guarda. Antes comprueba que el servidor es un Bitabit con una API
     * que esta app entiende. Se llama desde el ejecutor.
     *
     * @return si la sesión quedó guardada; si no, valdrá hasta que se cierre la app
     * @throws IllegalArgumentException si la dirección o la contraseña no son válidas
     */
    boolean login(String server, String password, String device) throws IOException {
        BitabitClient client = new BitabitClient(server);
        Contract.Info info = client.info();
        if (!LoginRules.supportsApi(info.getApiVersion())) {
            throw new IncompatibleServerException(info.getApiVersion());
        }
        Contract.Login login = client.login(password, device);
        long sessionId = login.getSession().getId();
        boolean saved = true;
        try {
            tokens.save(server, sessionId, login.getToken());
        } catch (GeneralSecurityException e) {
            Log.w(BitabitApp.TAG, "no se pudo guardar la sesión: " + e.getClass().getSimpleName());
            saved = false;
        }
        session = Optional.of(new ActiveSession(server, sessionId, client));
        return saved;
    }

    /** Olvida la sesión, aquí y en lo guardado. No la cierra en el servidor. */
    void endSession() {
        Optional<ActiveSession> current = session;
        session = Optional.empty();
        current.ifPresent(s -> s.client().forgetToken());
        tokens.clear();
    }

    /** Una sesión abierta: el servidor, su ID y el cliente que lleva su token. */
    public static final class ActiveSession {

        private final String server;
        private final long sessionId;
        private final BitabitClient client;

        ActiveSession(String server, long sessionId, BitabitClient client) {
            this.server = server;
            this.sessionId = sessionId;
            this.client = client;
        }

        public String server() {
            return server;
        }

        public long sessionId() {
            return sessionId;
        }

        public BitabitClient client() {
            return client;
        }
    }
}
