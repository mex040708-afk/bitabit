package io.github.mex040708afk.bitabit;

/** Las reglas del inicio de sesión que no dependen de Android. */
final class LoginRules {

    /** Lo más largo que admite el contrato para el nombre del dispositivo. */
    static final int MAX_DEVICE_CHARS = 64;
    /** El primer número de {@code apiVersion} que entiende esta app (core habla la v1). */
    private static final String SUPPORTED_MAJOR = "1";
    private static final String FALLBACK_DEVICE = "Android";

    private LoginRules() {
    }

    /**
     * El nombre con el que el dispositivo aparece en la lista de sesiones, a partir de
     * {@code Build.MODEL}. El contrato pide de 1 a 64 caracteres y algunos fabricantes dejan
     * el modelo vacío.
     */
    static String deviceName(String model) {
        String name = model == null ? "" : model.trim();
        if (name.isEmpty()) {
            return FALLBACK_DEVICE;
        }
        if (name.codePointCount(0, name.length()) > MAX_DEVICE_CHARS) {
            name = name.substring(0, name.offsetByCodePoints(0, MAX_DEVICE_CHARS)).trim();
        }
        return name;
    }

    /** Si la versión de la API del servidor es una que esta app entiende. */
    static boolean supportsApi(String apiVersion) {
        if (apiVersion == null) {
            return false;
        }
        int dot = apiVersion.indexOf('.');
        String major = dot < 0 ? apiVersion : apiVersion.substring(0, dot);
        return SUPPORTED_MAJOR.equals(major.trim());
    }
}
