use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};
use tauri_plugin_notification::NotificationExt;

const OPERATIONS: [&str; 13] = [
    "capabilities",
    "notify",
    "store_secret",
    "load_secret",
    "delete_secret",
    "export_report",
    "pick_inputs",
    "runtime_start",
    "runtime_request",
    "runtime_restart",
    "runtime_stop",
    "cyber_agent_start",
    "cyber_agent_stop",
];
const NOTIFICATION_KINDS: [&str; 3] = ["approval_required", "task_succeeded", "task_failed"];
const MAX_REPORT_BYTES: usize = 16 * 1024 * 1024;
const MAX_INPUT_BYTES: u64 = 64 * 1024 * 1024;
const KEYRING_SERVICE: &str = "com.cyber.code.desktop";

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CapabilitiesResponse {
    pub operations: [&'static str; 13],
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct NotifyRequest {
    pub kind: String,
    pub title: String,
    pub body: String,
}

#[derive(Debug, Serialize)]
pub struct NotifyReceipt {
    pub accepted: bool,
}

pub trait NotificationSink {
    fn show(&self, title: &str, body: &str) -> Result<(), String>;
}

impl NotificationSink for tauri::AppHandle {
    fn show(&self, title: &str, body: &str) -> Result<(), String> {
        self.notification()
            .builder()
            .title(title)
            .body(body)
            .show()
            .map_err(|_| "notification could not be delivered".to_string())
    }
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct StoreSecretRequest {
    pub id: String,
    pub secret: String,
}

#[derive(Debug, Serialize)]
pub struct StoredSecretReceipt {
    pub id: String,
    pub stored: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct LoadSecretRequest {
    pub id: String,
}

#[derive(Debug, Serialize)]
pub struct LoadedSecret {
    pub id: String,
    pub secret: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct DeleteSecretRequest {
    pub id: String,
}

#[derive(Debug, Serialize)]
pub struct DeletedSecretReceipt {
    pub id: String,
    pub deleted: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ExportReportRequest {
    pub suggested_name: String,
    pub bytes: Vec<u8>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum ExportStatus {
    Exported,
    Cancelled,
}

#[derive(Debug, Serialize)]
pub struct ExportReportReceipt {
    pub status: ExportStatus,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SelectedInput {
    pub filename: String,
    pub media_type: String,
    pub bytes: Vec<u8>,
}

#[tauri::command]
pub fn capabilities() -> CapabilitiesResponse {
    debug_assert!(
        OPERATIONS
            .iter()
            .all(|operation| is_supported_operation(operation))
    );
    CapabilitiesResponse {
        operations: OPERATIONS,
    }
}

pub fn is_supported_operation(operation: &str) -> bool {
    OPERATIONS.contains(&operation)
}

fn require_identifier(id: &str) -> Result<(), String> {
    if id.is_empty()
        || id.len() > 128
        || !id
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.'))
    {
        return Err("credential identifier is invalid".into());
    }
    Ok(())
}

pub fn validate_notification(request: &NotifyRequest) -> Result<(), String> {
    if !NOTIFICATION_KINDS.contains(&request.kind.as_str())
        || request.title.trim().is_empty()
        || request.body.trim().is_empty()
        || request.title.len() > 120
        || request.body.len() > 1_000
    {
        return Err("notification request is invalid".into());
    }
    Ok(())
}

pub fn route_notification(
    sink: &impl NotificationSink,
    request: &NotifyRequest,
) -> Result<NotifyReceipt, String> {
    validate_notification(request)?;
    sink.show(&request.title, &request.body)?;
    Ok(NotifyReceipt { accepted: true })
}

pub fn validate_export_request(request: &ExportReportRequest) -> Result<(), String> {
    let name = request.suggested_name.as_str();
    if name.trim().is_empty()
        || name == "."
        || name == ".."
        || name.contains(['/', '\\'])
        || request.bytes.is_empty()
        || request.bytes.len() > MAX_REPORT_BYTES
    {
        return Err("report export request is invalid".into());
    }
    Ok(())
}

#[tauri::command]
pub fn notify(app: tauri::AppHandle, request: NotifyRequest) -> Result<NotifyReceipt, String> {
    route_notification(&app, &request)
}

#[tauri::command]
pub fn store_secret(request: StoreSecretRequest) -> Result<StoredSecretReceipt, String> {
    require_identifier(&request.id)?;
    if request.secret.is_empty() || request.secret.len() > 64 * 1024 {
        return Err("credential value is invalid".into());
    }
    let entry = keyring::Entry::new(KEYRING_SERVICE, &request.id)
        .map_err(|_| "credential service is unavailable".to_string())?;
    entry
        .set_password(&request.secret)
        .map_err(|_| "credential could not be stored".to_string())?;
    Ok(StoredSecretReceipt {
        id: request.id,
        stored: true,
    })
}

#[tauri::command]
pub fn load_secret(request: LoadSecretRequest) -> Result<LoadedSecret, String> {
    require_identifier(&request.id)?;
    let entry = keyring::Entry::new(KEYRING_SERVICE, &request.id)
        .map_err(|_| "credential service is unavailable".to_string())?;
    let secret = entry
        .get_password()
        .map_err(|_| "credential is unavailable".to_string())?;
    if secret.is_empty() || secret.len() > 64 * 1024 {
        return Err("credential value is invalid".into());
    }
    Ok(LoadedSecret {
        id: request.id,
        secret,
    })
}

#[tauri::command]
pub fn delete_secret(request: DeleteSecretRequest) -> Result<DeletedSecretReceipt, String> {
    require_identifier(&request.id)?;
    let entry = keyring::Entry::new(KEYRING_SERVICE, &request.id)
        .map_err(|_| "credential service is unavailable".to_string())?;
    entry
        .delete_credential()
        .map_err(|_| "credential could not be deleted".to_string())?;
    Ok(DeletedSecretReceipt {
        id: request.id,
        deleted: true,
    })
}

fn write_selected_report(path: &Path, bytes: &[u8]) -> Result<(), String> {
    if path
        .symlink_metadata()
        .is_ok_and(|metadata| metadata.file_type().is_symlink())
    {
        return Err("report destination cannot be a symbolic link".into());
    }
    std::fs::write(path, bytes).map_err(|_| "report could not be exported".to_string())
}

#[tauri::command]
pub async fn export_report(request: ExportReportRequest) -> Result<ExportReportReceipt, String> {
    validate_export_request(&request)?;
    let suggested_name = request.suggested_name;
    let selected = tauri::async_runtime::spawn_blocking(move || {
        rfd::FileDialog::new()
            .set_file_name(&suggested_name)
            .save_file()
    })
    .await
    .map_err(|_| "report destination selection failed".to_string())?;
    let Some(path) = selected else {
        return Ok(ExportReportReceipt {
            status: ExportStatus::Cancelled,
        });
    };
    write_selected_report(&path, &request.bytes)?;
    Ok(ExportReportReceipt {
        status: ExportStatus::Exported,
    })
}

fn input_media_type(path: &Path) -> &'static str {
    match path
        .extension()
        .and_then(|value| value.to_str())
        .unwrap_or("")
        .to_ascii_lowercase()
        .as_str()
    {
        "yaml" | "yml" => "application/yaml",
        "json" => "application/json",
        "xml" => "application/xml",
        "md" | "markdown" => "text/markdown",
        "txt" | "log" => "text/plain",
        "pdf" => "application/pdf",
        "docx" => "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        "zip" => "application/zip",
        "tar" => "application/x-tar",
        "tgz" | "gz" => "application/gzip",
        _ => "application/octet-stream",
    }
}

fn read_selected_input(path: &Path) -> Result<SelectedInput, String> {
    let metadata = path
        .symlink_metadata()
        .map_err(|_| "task input is unavailable".to_string())?;
    if metadata.file_type().is_symlink()
        || !metadata.is_file()
        || metadata.len() == 0
        || metadata.len() > MAX_INPUT_BYTES
    {
        return Err("task input must be a non-empty bounded regular file".into());
    }
    let filename = path
        .file_name()
        .and_then(|value| value.to_str())
        .filter(|value| !value.is_empty())
        .ok_or_else(|| "task input filename is invalid".to_string())?
        .to_string();
    let bytes = std::fs::read(path).map_err(|_| "task input could not be read".to_string())?;
    if bytes.is_empty() || bytes.len() as u64 > MAX_INPUT_BYTES {
        return Err("task input must be a non-empty bounded regular file".into());
    }
    Ok(SelectedInput {
        filename,
        media_type: input_media_type(path).to_string(),
        bytes,
    })
}

#[tauri::command]
pub async fn pick_inputs() -> Result<Vec<SelectedInput>, String> {
    let selected: Vec<PathBuf> =
        tauri::async_runtime::spawn_blocking(|| rfd::FileDialog::new().pick_files())
            .await
            .map_err(|_| "task input selection failed".to_string())?
            .unwrap_or_default();
    if selected.len() > 64 {
        return Err("too many task inputs selected".into());
    }
    selected
        .iter()
        .map(|path| read_selected_input(path))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;

    struct RecordingNotifications(RefCell<Vec<(String, String)>>);

    impl NotificationSink for RecordingNotifications {
        fn show(&self, title: &str, body: &str) -> Result<(), String> {
            self.0.borrow_mut().push((title.into(), body.into()));
            Ok(())
        }
    }

    #[test]
    fn native_operations_are_exactly_allowlisted() {
        assert_eq!(
            capabilities().operations,
            [
                "capabilities",
                "notify",
                "store_secret",
                "load_secret",
                "delete_secret",
                "export_report",
                "pick_inputs",
                "runtime_start",
                "runtime_request",
                "runtime_restart",
                "runtime_stop",
                "cyber_agent_start",
                "cyber_agent_stop",
            ]
        );
        assert!(!is_supported_operation("shell"));
    }

    #[test]
    fn notifications_allow_only_approval_and_terminal_task_states() {
        assert!(
            validate_notification(&NotifyRequest {
                kind: "approval_required".into(),
                title: "Approval".into(),
                body: "Review scope".into(),
            })
            .is_ok()
        );
        assert!(
            validate_notification(&NotifyRequest {
                kind: "chat_message".into(),
                title: "Hello".into(),
                body: "World".into(),
            })
            .is_err()
        );
    }

    #[test]
    fn valid_notifications_are_routed_once_through_the_native_sink() {
        let sink = RecordingNotifications(RefCell::new(Vec::new()));
        let receipt = route_notification(
            &sink,
            &NotifyRequest {
                kind: "task_failed".into(),
                title: "Task failed".into(),
                body: "Review the terminal state".into(),
            },
        )
        .unwrap();

        assert!(receipt.accepted);
        assert_eq!(
            sink.0.borrow().as_slice(),
            &[("Task failed".into(), "Review the terminal state".into())]
        );
    }

    #[test]
    fn export_rejects_path_traversal_and_accepts_a_plain_file_name() {
        assert!(
            validate_export_request(&ExportReportRequest {
                suggested_name: "../report.md".into(),
                bytes: vec![1],
            })
            .is_err()
        );
        assert!(
            validate_export_request(&ExportReportRequest {
                suggested_name: "report.md".into(),
                bytes: b"CYBER".to_vec(),
            })
            .is_ok()
        );
    }

    #[test]
    fn selected_task_input_is_read_with_bounded_metadata() {
        let root = std::env::temp_dir().join(format!("cyber-input-{}", std::process::id()));
        std::fs::create_dir_all(&root).unwrap();
        let path = root.join("api.yaml");
        std::fs::write(&path, b"openapi: 3.0.0").unwrap();
        let input = read_selected_input(&path).unwrap();
        assert_eq!(input.filename, "api.yaml");
        assert_eq!(input.media_type, "application/yaml");
        assert_eq!(input.bytes, b"openapi: 3.0.0");
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn secret_receipts_never_serialize_secret_values() {
        let receipt = StoredSecretReceipt {
            id: "deepseek".into(),
            stored: true,
        };
        let serialized = serde_json::to_string(&receipt).unwrap();
        assert_eq!(serialized, r#"{"id":"deepseek","stored":true}"#);
        assert!(!serialized.contains("top-secret"));
    }
}
