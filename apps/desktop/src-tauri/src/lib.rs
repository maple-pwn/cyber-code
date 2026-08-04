#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
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
