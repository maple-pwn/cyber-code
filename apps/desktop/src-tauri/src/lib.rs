mod commands;
mod runtime_process;
mod security;
mod window;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(
            |app, arguments, _working_directory| {
                let forwarded = arguments.get(1..).unwrap_or_default();
                if window::secondary_launch_allowed(forwarded) {
                    window::focus_main_window(app);
                }
            },
        ))
        .plugin(
            tauri::plugin::Builder::<tauri::Wry>::new("navigation-policy")
                .on_navigation(|_webview, url| {
                    security::is_navigation_allowed(url, cfg!(debug_assertions))
                })
                .build(),
        )
        .plugin(
            tauri_plugin_window_state::Builder::default()
                .with_state_flags(
                    tauri_plugin_window_state::StateFlags::POSITION
                        | tauri_plugin_window_state::StateFlags::SIZE
                        | tauri_plugin_window_state::StateFlags::MAXIMIZED,
                )
                .build(),
        )
        .plugin(tauri_plugin_notification::init())
        .manage(runtime_process::RuntimeManagerState::default())
        .setup(|app| {
            if let Some(window) = tauri::Manager::get_webview_window(app, "main") {
                window::clamp_main_window(&window)?;
            }
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::capabilities,
            commands::notify,
            commands::store_secret,
            commands::delete_secret,
            commands::export_report,
            runtime_process::runtime_start,
            runtime_process::runtime_request,
            runtime_process::runtime_restart,
            runtime_process::runtime_stop,
        ])
        .run(tauri::generate_context!())
        .expect("failed to run CYBER desktop shell");
}

#[cfg(test)]
mod tests {
    use serde_json::Value;
    use std::{fs, path::PathBuf};

    fn repository_root() -> PathBuf {
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../..")
    }

    #[test]
    fn desktop_configuration_has_no_shell_or_filesystem_plugins() {
        let config: Value = serde_json::from_str(include_str!("../tauri.conf.json")).unwrap();
        let serialized = config.to_string();
        assert_eq!(config["productName"], "CYBER Desktop");
        assert!(!serialized.contains("shell"));
        assert!(!serialized.contains("fs:"));
    }

    #[test]
    fn desktop_manifest_enforces_a_closed_content_and_bundle_policy() {
        let config: Value = serde_json::from_str(include_str!("../tauri.conf.json")).unwrap();
        let security = &config["app"]["security"];
        let csp = security["csp"].as_str().unwrap();

        for directive in [
            "default-src 'self'",
            "object-src 'none'",
            "base-uri 'none'",
            "frame-src 'none'",
            "form-action 'none'",
        ] {
            assert!(
                csp.contains(directive),
                "missing CSP directive: {directive}"
            );
        }
        assert_eq!(security["capabilities"], serde_json::json!(["main-window"]));
        assert_eq!(config["bundle"]["active"], true);
    }

    #[test]
    fn main_window_capability_is_local_and_grants_no_native_plugins() {
        let path = repository_root().join("apps/desktop/src-tauri/capabilities/main-window.json");
        let capability: Value = serde_json::from_str(
            &fs::read_to_string(path).expect("main-window capability must exist"),
        )
        .unwrap();

        assert_eq!(capability["identifier"], "main-window");
        assert_eq!(capability["windows"], serde_json::json!(["main"]));
        assert_eq!(capability["permissions"], serde_json::json!([]));
        assert!(capability.get("remote").is_none());
        assert!(!capability.to_string().contains('*'));
    }

    #[test]
    fn continuous_integration_builds_unsigned_desktop_artifacts_on_all_platforms() {
        let workflow = fs::read_to_string(repository_root().join(".github/workflows/ci.yml"))
            .expect("CI workflow must exist");

        for required in [
            "desktop-checks:",
            "cargo fmt --check",
            "cargo clippy --all-targets -- -D warnings",
            "cargo audit",
            "cargo test",
            "test:coverage",
            "test:e2e",
            "desktop-build:",
            "ubuntu-latest",
            "windows-latest",
            "macos-latest",
            "tauri build --debug",
            "bundle: deb",
            "bundle: nsis",
            "bundle: app",
            "--bundles ${{ matrix.bundle }}",
            "actions/upload-artifact@v4",
        ] {
            assert!(workflow.contains(required), "missing CI gate: {required}");
        }
        for forbidden in [
            "TAURI_SIGNING_PRIVATE_KEY",
            "APPLE_CERTIFICATE",
            "APPLE_API_KEY",
            "secrets.",
        ] {
            assert!(
                !workflow.contains(forbidden),
                "CI must remain unsigned: {forbidden}"
            );
        }
    }

    #[test]
    fn signing_workflow_requires_a_protected_release_environment() {
        let workflow =
            fs::read_to_string(repository_root().join(".github/workflows/release-bundle.yml"))
                .expect("release workflow must exist");

        assert!(workflow.contains("environment: protected-release"));
        assert!(workflow.contains("CYBER_CODE_UPDATE_SIGNING_KEY"));
    }
}
