package io.github.mex040708afk.bitabit;

import android.os.Build;
import android.os.Bundle;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;
import android.widget.Toast;
import androidx.fragment.app.Fragment;
import java.util.Optional;

/** El inicio de sesión: servidor y contraseña. La contraseña no se guarda nunca (ADR-021). */
public final class LoginFragment extends Fragment {

    private static final String ARG_NOTICE = "notice";

    private EditText server;
    private EditText password;
    private Button login;
    private TextView status;

    public LoginFragment() {
        super(R.layout.fragment_login);
    }

    static LoginFragment newInstance(Optional<String> notice) {
        LoginFragment fragment = new LoginFragment();
        Bundle args = new Bundle();
        notice.ifPresent(text -> args.putString(ARG_NOTICE, text));
        fragment.setArguments(args);
        return fragment;
    }

    @Override
    public void onViewCreated(View view, Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);
        Services services = BitabitApp.services(requireContext());
        if (services.session().isPresent()) {
            // Se recreó la pantalla mientras terminaba un inicio de sesión.
            navigator().showHome();
            return;
        }
        server = view.findViewById(R.id.server);
        password = view.findViewById(R.id.password);
        login = view.findViewById(R.id.login);
        status = view.findViewById(R.id.status);
        if (savedInstanceState == null) {
            server.setText(services.lastServer().orElse(getString(R.string.server_default)));
        }
        Bundle args = getArguments();
        if (args != null && args.getString(ARG_NOTICE) != null) {
            status.setText(args.getString(ARG_NOTICE));
        }
        login.setOnClickListener(v -> login(services));
        view.findViewById(R.id.diagnostic).setOnClickListener(v -> navigator().openDiagnostic());
    }

    private void login(Services services) {
        String address = server.getText().toString().trim();
        String secret = password.getText().toString();
        String device = LoginRules.deviceName(Build.MODEL);
        setBusy(true);
        status.setText(R.string.login_running);
        services.run(getViewLifecycleOwner(),
                () -> services.login(address, secret, device),
                saved -> {
                    if (!saved) {
                        Toast.makeText(requireContext(), R.string.login_not_saved, Toast.LENGTH_LONG).show();
                    }
                    navigator().showHome();
                },
                e -> {
                    setBusy(false);
                    status.setText(ErrorMessages.describe(e).format(getResources()));
                });
    }

    private void setBusy(boolean busy) {
        login.setEnabled(!busy);
        server.setEnabled(!busy);
        password.setEnabled(!busy);
    }

    private Navigator navigator() {
        return (Navigator) requireActivity();
    }
}
