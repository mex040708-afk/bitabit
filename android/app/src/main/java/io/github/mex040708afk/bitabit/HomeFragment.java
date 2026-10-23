package io.github.mex040708afk.bitabit;

import android.os.Bundle;
import android.view.View;
import android.widget.Button;
import android.widget.TextView;
import androidx.fragment.app.Fragment;
import io.github.mex040708afk.bitabit.core.BitabitClient;
import io.github.mex040708afk.bitabit.core.Contract;
import java.util.Optional;

/**
 * Pantalla principal provisional: dice a qué servidor se está conectado y deja cerrar la
 * sesión. El catálogo llegará aquí.
 */
public final class HomeFragment extends Fragment {

    private TextView serverInfo;
    private TextView status;
    private Button retry;
    private Button logout;

    public HomeFragment() {
        super(R.layout.fragment_home);
    }

    @Override
    public void onViewCreated(View view, Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);
        Optional<Services.ActiveSession> session = BitabitApp.services(requireContext()).session();
        if (!session.isPresent()) {
            // Se recreó la app después de que el sistema la cerrara: hay que retomar la sesión.
            navigator().start();
            return;
        }
        serverInfo = view.findViewById(R.id.server_info);
        status = view.findViewById(R.id.status);
        retry = view.findViewById(R.id.retry);
        logout = view.findViewById(R.id.logout);
        retry.setOnClickListener(v -> load(session.get()));
        logout.setOnClickListener(v -> logout(session.get()));
        view.findViewById(R.id.diagnostic).setOnClickListener(v -> navigator().openDiagnostic());
        load(session.get());
    }

    private void load(Services.ActiveSession session) {
        retry.setVisibility(View.GONE);
        serverInfo.setText(getString(R.string.home_server, session.server()));
        status.setText(R.string.home_loading);
        BitabitClient client = session.client();
        BitabitApp.services(requireContext()).run(getViewLifecycleOwner(),
                () -> {
                    Contract.Info info = client.info();
                    // info() no pide sesión: esta llamada es la que dice si el token aún vale.
                    client.sessions();
                    return info;
                },
                info -> status.setText(getString(R.string.home_info, info.getService(),
                        info.getServerVersion(), info.getApiVersion())),
                e -> {
                    if (ErrorMessages.isSessionRejected(e)) {
                        BitabitApp.services(requireContext()).endSession();
                        navigator().showLogin(Optional.of(getString(R.string.session_expired)));
                        return;
                    }
                    status.setText(ErrorMessages.describe(e).format(getResources()));
                    retry.setVisibility(View.VISIBLE);
                });
    }

    private void logout(Services.ActiveSession session) {
        logout.setEnabled(false);
        status.setText(R.string.logout_running);
        Services services = BitabitApp.services(requireContext());
        services.run(getViewLifecycleOwner(),
                () -> {
                    session.client().closeSession(session.sessionId());
                    return Boolean.TRUE;
                },
                closed -> {
                    services.endSession();
                    navigator().showLogin(Optional.of(getString(R.string.logged_out)));
                },
                e -> {
                    // Se olvida igualmente: quedarse sin poder salir sería peor que dejar la
                    // sesión abierta en el servidor, que se puede cerrar desde otro dispositivo.
                    services.endSession();
                    String notice = ErrorMessages.isSessionRejected(e)
                            ? getString(R.string.logged_out)
                            : getString(R.string.logout_offline, ErrorMessages.describe(e).format(getResources()));
                    navigator().showLogin(Optional.of(notice));
                });
    }

    private Navigator navigator() {
        return (Navigator) requireActivity();
    }
}
