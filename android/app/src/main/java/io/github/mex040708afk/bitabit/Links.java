package io.github.mex040708afk.bitabit;

/** Junta el servidor elegido con un enlace pegado de «bitabit link». */
final class Links {

    private Links() {
    }

    /**
     * Devuelve la URL que hay que pedir, o null si el servidor no es http(s) o
     * el enlace no tiene «/media/». Del enlace se usa desde «/media/» en
     * adelante, así que da igual el servidor que traiga: bitabit link dice
     * localhost, y en el emulador el servidor es 10.0.2.2.
     */
    static String mediaUrl(String server, String pasted) {
        String origin = server.trim();
        while (origin.endsWith("/")) {
            origin = origin.substring(0, origin.length() - 1);
        }
        if (!origin.startsWith("http://") && !origin.startsWith("https://")) {
            return null;
        }
        String link = pasted.trim();
        int start = link.indexOf("/media/");
        if (start < 0) {
            return null;
        }
        return origin + link.substring(start);
    }
}
