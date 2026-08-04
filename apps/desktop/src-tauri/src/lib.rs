mod commands;
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
            tauri_plugin_window_state::Builder::default()
                .with_state_flags(
                    tauri_plugin_window_state::StateFlags::POSITION
                        | tauri_plugin_window_state::StateFlags::SIZE
                        | tauri_plugin_window_state::StateFlags::MAXIMIZED,
                )
                .build(),
        )
        .plugin(tauri_plugin_notification::init())
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
        ])
        .run(tauri::generate_context!())
        .expect("failed to run CYBER desktop shell");
}

#[cfg(test)]
mod tests {
    use serde_json::Value;

    #[test]
    fn desktop_configuration_has_no_shell_or_filesystem_plugins() {
        let config: Value = serde_json::from_str(include_str!("../tauri.conf.json")).unwrap();
        let serialized = config.to_string();
        assert_eq!(config["productName"], "CYBER Desktop");
        assert!(!serialized.contains("shell"));
        assert!(!serialized.contains("fs:"));
    }
}
