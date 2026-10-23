package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.ApiException;
import java.io.IOException;
import java.io.InterruptedIOException;
import java.net.ConnectException;
import java.net.UnknownHostException;

/**
 * Los errores de la red, contados en la ventana. Nunca se enseña el texto de la
 * petición: solo el estado, el código del contrato y lo que dice el error de red.
 */
final class Messages {

    private Messages() {
    }

    /** El error de iniciar sesión en {@code server}. */
    static String login(String server, IOException e) {
        if (e instanceof ApiException api) {
            return switch (api.status()) {
                case 401 -> api.attemptsLeft()
                        .map(left -> "Contraseña incorrecta. " + (left == 1
                                ? "Queda 1 intento antes de que el servidor bloquee este equipo."
                                : "Quedan " + left + " intentos antes de que el servidor bloquee este equipo."))
                        .orElse("Contraseña incorrecta.");
                case 429 -> api.retryAfter()
                        .map(seconds -> "Demasiados intentos fallidos: el servidor ha bloqueado este equipo. "
                                + "Vuelve a probar dentro de " + duration(seconds) + ".")
                        .orElse("Demasiados intentos fallidos: el servidor ha bloqueado este equipo.");
                default -> api(server, api);
            };
        }
        return network(server, e);
    }

    /** Un error de la red o del servidor fuera del inicio de sesión. */
    static String request(String server, IOException e) {
        if (e instanceof ApiException api) {
            return api(server, api);
        }
        return network(server, e);
    }

    private static String api(String server, ApiException api) {
        return switch (api.status()) {
            case 400 -> "El servidor de " + server + " no entiende la petición (400 " + api.code() + ").";
            case 401 -> "El servidor de " + server + " ya no acepta la sesión.";
            case 404 -> "En " + server + " no hay una API de Bitabit compatible (404).";
            default -> api.status() >= 500
                    ? "El servidor de " + server + " ha fallado (" + api.status() + "). Prueba más tarde."
                    : "El servidor de " + server + " ha respondido " + api.status()
                            + (api.code().isEmpty() ? "" : " " + api.code()) + ".";
        };
    }

    private static String network(String server, IOException e) {
        if (e instanceof UnknownHostException) {
            return "No se encuentra el servidor " + server + ": revisa la dirección.";
        }
        if (e instanceof ConnectException) {
            return "No se pudo conectar con " + server + ": ¿está encendido y en esa dirección?";
        }
        if (e instanceof InterruptedIOException) {
            return "El servidor " + server + " no ha respondido a tiempo.";
        }
        return "Error de red con " + server + ": " + e.getClass().getSimpleName()
                + (e.getMessage() == null ? "" : " (" + e.getMessage() + ")") + ".";
    }

    /** Una espera en segundos, en horas, minutos y segundos: «23 h 59 min», «5 min 3 s», «40 s». */
    static String duration(int seconds) {
        int hours = seconds / 3600;
        int minutes = seconds % 3600 / 60;
        int rest = seconds % 60;
        if (hours > 0) {
            // Con horas por delante, los segundos no ayudan; se redondea hacia arriba para no
            // prometer un desbloqueo antes de tiempo.
            if (rest > 0) {
                minutes++;
                if (minutes == 60) {
                    return (hours + 1) + " h";
                }
            }
            return hours + " h" + (minutes > 0 ? " " + minutes + " min" : "");
        }
        if (minutes > 0) {
            return minutes + " min" + (rest > 0 ? " " + rest + " s" : "");
        }
        return Math.max(rest, 1) + " s";
    }
}
