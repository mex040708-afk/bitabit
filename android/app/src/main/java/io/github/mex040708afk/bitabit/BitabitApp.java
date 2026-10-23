package io.github.mex040708afk.bitabit;

import android.app.Application;
import android.content.Context;
import android.util.Log;

/** La aplicación: crea una sola vez los {@link Services} que comparten todas las pantallas. */
public final class BitabitApp extends Application {

    static final String TAG = "Bitabit";

    private Services services;

    @Override
    public void onCreate() {
        super.onCreate();
        TokenStore tokens = new TokenStore(
                getSharedPreferences(TokenStore.PREFS_NAME, MODE_PRIVATE),
                new KeystoreTokenCipher(),
                warning -> Log.w(TAG, warning));
        services = new Services(tokens);
    }

    /** Los servicios de la app, desde cualquier pantalla. */
    public static Services services(Context context) {
        return ((BitabitApp) context.getApplicationContext()).services;
    }
}
