package io.github.mex040708afk.bitabit;

import java.util.Optional;

/** La navegación entre pantallas. La implementa {@link MainActivity}; los Fragments la piden a su actividad. */
public interface Navigator {

    /** Retoma la sesión guardada y va al inicio, o al inicio de sesión si no hay. */
    void start();

    /** El inicio de sesión, con un aviso opcional (sesión caducada, sesión cerrada…). */
    void showLogin(Optional<String> notice);

    /** La pantalla principal, con la sesión ya abierta. */
    void showHome();

    /** La pantalla de diagnóstico de la fase 1. */
    void openDiagnostic();
}
