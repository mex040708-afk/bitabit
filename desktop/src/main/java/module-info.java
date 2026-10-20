module io.github.mex040708afk.bitabit.desktop {
    requires javafx.controls;
    requires com.google.gson;
    requires java.net.http;

    // JavaFX crea la ventana por reflexión desde javafx.graphics.
    exports io.github.mex040708afk.bitabit.desktop to javafx.graphics;
}
