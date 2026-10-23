package io.github.mex040708afk.bitabit;

import android.content.Intent;
import android.os.Bundle;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.graphics.Insets;
import androidx.core.view.ViewCompat;
import androidx.core.view.WindowInsetsCompat;
import androidx.fragment.app.Fragment;
import java.util.Optional;

/**
 * La única actividad de la app (ADR-023): un contenedor donde cada pantalla es un
 * Fragment. Al arrancar retoma la sesión guardada o pide la contraseña.
 */
public final class MainActivity extends AppCompatActivity implements Navigator {

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);
        // Desde targetSdk 35 la app se dibuja bajo las barras del sistema, y
        // fitsSystemWindows no llega al contenedor de Fragments: el margen se pone aquí.
        ViewCompat.setOnApplyWindowInsetsListener(findViewById(R.id.container), (view, insets) -> {
            Insets bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() | WindowInsetsCompat.Type.ime());
            view.setPadding(bars.left, bars.top, bars.right, bars.bottom);
            return WindowInsetsCompat.CONSUMED;
        });
        // Al recrearse, el FragmentManager ya repone la pantalla que había.
        if (savedInstanceState == null) {
            start();
        }
    }

    @Override
    public void start() {
        Services services = BitabitApp.services(this);
        services.run(this, services::restore,
                restored -> {
                    if (restored.isPresent()) {
                        showHome();
                    } else {
                        showLogin(Optional.empty());
                    }
                },
                e -> showLogin(Optional.of(ErrorMessages.describe(e).format(getResources()))));
    }

    @Override
    public void showLogin(Optional<String> notice) {
        show(LoginFragment.newInstance(notice));
    }

    @Override
    public void showHome() {
        show(new HomeFragment());
    }

    @Override
    public void openDiagnostic() {
        Intent intent = new Intent(this, DiagnosticActivity.class);
        BitabitApp.services(this).session()
                .ifPresent(s -> intent.putExtra(DiagnosticActivity.EXTRA_SERVER, s.server()));
        startActivity(intent);
    }

    private void show(Fragment screen) {
        // La respuesta de la API puede llegar con la actividad en segundo plano. Perder esta
        // transacción no es grave: cada pantalla comprueba la sesión al crearse y se corrige.
        getSupportFragmentManager().beginTransaction()
                .replace(R.id.container, screen)
                .commitAllowingStateLoss();
    }
}
