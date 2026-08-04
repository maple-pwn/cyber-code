use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{
    io::{BufRead, BufReader, Write},
    path::{Path, PathBuf},
    process::{Child, ChildStdin, Command, Stdio},
    sync::{
        Mutex,
        mpsc::{self, Receiver, RecvTimeoutError},
    },
    time::Duration,
};

const RUNTIME_EXECUTABLE_ENV: &str = "CYBER_CODE_RUNTIME_EXECUTABLE";
const RUNTIME_BEARER_ENV: &str = "CYBER_CODE_RUNTIME_BEARER";
const RUNTIME_PROTOCOL_VERSION: u64 = 1;
const MAX_RUNTIME_RESPONSE_BYTES: usize = 16 * 1024 * 1024;
const DEFAULT_STARTUP_TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct RuntimeBridgeRequest {
    pub id: String,
    #[serde(rename = "type")]
    pub request_type: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub handshake: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub command: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub task_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub after_cursor: Option<u64>,
}

pub fn prepare_request(request: &RuntimeBridgeRequest, bearer: &str) -> Result<Value, String> {
    let mut value = serde_json::to_value(request)
        .map_err(|_| "runtime request could not be encoded".to_string())?;
    let object = value
        .as_object_mut()
        .ok_or_else(|| "runtime request is invalid".to_string())?;
    object.insert("bearer".to_string(), Value::String(bearer.to_string()));
    Ok(value)
}

pub fn discover_runtime_executable(
    current_executable: &Path,
    development_override: Option<&Path>,
    development: bool,
) -> Result<PathBuf, String> {
    if development && let Some(path) = development_override {
        return Ok(path.to_path_buf());
    }
    let parent = current_executable
        .parent()
        .ok_or_else(|| "runtime executable could not be discovered".to_string())?;
    let name = if current_executable
        .extension()
        .is_some_and(|ext| ext == "exe")
    {
        "cyber-code.exe"
    } else {
        "cyber-code"
    };
    Ok(parent.join(name))
}

pub fn default_runtime_executable() -> Result<PathBuf, String> {
    let current = std::env::current_exe()
        .map_err(|_| "runtime executable could not be discovered".to_string())?;
    let override_path = std::env::var_os(RUNTIME_EXECUTABLE_ENV).map(PathBuf::from);
    discover_runtime_executable(&current, override_path.as_deref(), cfg!(debug_assertions))
}

pub fn generate_launch_secret() -> Result<String, String> {
    let mut bytes = [0_u8; 32];
    getrandom::fill(&mut bytes)
        .map_err(|_| "runtime launch credential could not be generated".to_string())?;
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        use std::fmt::Write;
        write!(&mut encoded, "{byte:02x}")
            .map_err(|_| "runtime launch credential could not be encoded".to_string())?;
    }
    Ok(encoded)
}

pub trait RuntimeProcess: Send {
    fn request(&mut self, request: Value, timeout: Duration) -> Result<Value, String>;
    fn stop(&mut self);
}

pub trait RuntimeLauncher: Send + Sync {
    fn launch(&self, executable: &Path, bearer: &str) -> Result<Box<dyn RuntimeProcess>, String>;
}

struct NativeRuntimeLauncher;

struct NativeRuntimeProcess {
    child: Child,
    input: ChildStdin,
    responses: Receiver<Result<Value, String>>,
}

impl RuntimeLauncher for NativeRuntimeLauncher {
    fn launch(&self, executable: &Path, bearer: &str) -> Result<Box<dyn RuntimeProcess>, String> {
        let mut child = Command::new(executable)
            .args(["runtime", "serve"])
            .env(RUNTIME_BEARER_ENV, bearer)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|_| "runtime_executable_unavailable".to_string())?;
        let input = child
            .stdin
            .take()
            .ok_or_else(|| "runtime_startup_failed".to_string())?;
        let output = child
            .stdout
            .take()
            .ok_or_else(|| "runtime_startup_failed".to_string())?;
        let (sender, responses) = mpsc::channel();
        std::thread::spawn(move || {
            let mut reader = BufReader::new(output);
            loop {
                match read_bounded_line(&mut reader, MAX_RUNTIME_RESPONSE_BYTES) {
                    Ok(Some(line)) => {
                        let response = serde_json::from_slice(&line)
                            .map_err(|_| "runtime_response_invalid".to_string());
                        if sender.send(response).is_err() {
                            break;
                        }
                    }
                    Ok(None) => break,
                    Err(code) => {
                        let _ = sender.send(Err(code));
                        break;
                    }
                }
            }
        });
        Ok(Box::new(NativeRuntimeProcess {
            child,
            input,
            responses,
        }))
    }
}

impl RuntimeProcess for NativeRuntimeProcess {
    fn request(&mut self, request: Value, timeout: Duration) -> Result<Value, String> {
        if self
            .child
            .try_wait()
            .map_err(|_| "runtime_crashed".to_string())?
            .is_some()
        {
            return Err("runtime_crashed".to_string());
        }
        let mut encoded =
            serde_json::to_vec(&request).map_err(|_| "runtime_request_invalid".to_string())?;
        if encoded.len() > MAX_RUNTIME_RESPONSE_BYTES {
            return Err("runtime_request_too_large".to_string());
        }
        encoded.push(b'\n');
        self.input
            .write_all(&encoded)
            .and_then(|_| self.input.flush())
            .map_err(|_| "runtime_crashed".to_string())?;
        match self.responses.recv_timeout(timeout) {
            Ok(response) => response,
            Err(RecvTimeoutError::Timeout) => Err("runtime_startup_timeout".to_string()),
            Err(RecvTimeoutError::Disconnected) => Err("runtime_crashed".to_string()),
        }
    }

    fn stop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

struct ManagedRuntime {
    process: Box<dyn RuntimeProcess>,
    bearer: String,
}

pub struct RuntimeProcessManager {
    executable: PathBuf,
    timeout: Duration,
    launcher: Box<dyn RuntimeLauncher>,
    running: Option<ManagedRuntime>,
}

#[derive(Default)]
pub struct RuntimeManagerState {
    manager: Mutex<Option<RuntimeProcessManager>>,
}

#[derive(Debug, Serialize)]
pub struct RuntimeStopReceipt {
    pub stopped: bool,
}

impl RuntimeProcessManager {
    pub fn new(executable: PathBuf) -> Self {
        Self::with_launcher(
            executable,
            DEFAULT_STARTUP_TIMEOUT,
            Box::new(NativeRuntimeLauncher),
        )
    }

    pub fn with_launcher(
        executable: PathBuf,
        timeout: Duration,
        launcher: Box<dyn RuntimeLauncher>,
    ) -> Self {
        Self {
            executable,
            timeout,
            launcher,
            running: None,
        }
    }

    pub fn start(&mut self) -> Result<Value, String> {
        if self.running.is_some() {
            return Err("runtime_already_running".to_string());
        }
        let bearer = generate_launch_secret()?;
        let mut process = self.launcher.launch(&self.executable, &bearer)?;
        let request = RuntimeBridgeRequest {
            id: "desktop-startup".to_string(),
            request_type: "handshake".to_string(),
            handshake: Some(serde_json::json!({
                "supportedProtocolVersions": [RUNTIME_PROTOCOL_VERSION],
                "afterCursor": 0
            })),
            command: None,
            task_id: None,
            after_cursor: None,
        };
        let prepared = prepare_request(&request, &bearer)?;
        let response = match process.request(prepared, self.timeout) {
            Ok(response) => response,
            Err(error) => {
                process.stop();
                return Err(error);
            }
        };
        if validate_runtime_response(&response, &request.id, &bearer).is_err() {
            process.stop();
            return Err("runtime_handshake_invalid".to_string());
        }
        if let Err(error) = validate_startup_handshake(&response) {
            process.stop();
            return Err(error);
        }
        self.running = Some(ManagedRuntime { process, bearer });
        Ok(response)
    }

    pub fn request(&mut self, request: RuntimeBridgeRequest) -> Result<Value, String> {
        let expected_id = request.id.clone();
        let running = self
            .running
            .as_mut()
            .ok_or_else(|| "runtime_not_running".to_string())?;
        let prepared = prepare_request(&request, &running.bearer)?;
        match running.process.request(prepared, self.timeout) {
            Ok(response) => {
                if validate_runtime_response(&response, &expected_id, &running.bearer).is_ok() {
                    return Ok(response);
                }
                running.process.stop();
                self.running = None;
                Err("runtime_response_invalid".to_string())
            }
            Err(error) => {
                running.process.stop();
                self.running = None;
                Err(error)
            }
        }
    }

    pub fn stop(&mut self) {
        if let Some(mut running) = self.running.take() {
            running.process.stop();
        }
    }

    pub fn restart(&mut self) -> Result<Value, String> {
        self.stop();
        self.start()
    }

    pub fn is_running(&self) -> bool {
        self.running.is_some()
    }
}

#[tauri::command]
pub fn runtime_start(state: tauri::State<'_, RuntimeManagerState>) -> Result<Value, String> {
    let mut guard = state
        .manager
        .lock()
        .map_err(|_| "runtime_manager_unavailable".to_string())?;
    if guard.is_none() {
        let executable = default_runtime_executable()?;
        *guard = Some(RuntimeProcessManager::new(executable));
    }
    guard
        .as_mut()
        .ok_or_else(|| "runtime_manager_unavailable".to_string())?
        .start()
}

#[tauri::command]
pub fn runtime_request(
    state: tauri::State<'_, RuntimeManagerState>,
    request: RuntimeBridgeRequest,
) -> Result<Value, String> {
    state
        .manager
        .lock()
        .map_err(|_| "runtime_manager_unavailable".to_string())?
        .as_mut()
        .ok_or_else(|| "runtime_not_running".to_string())?
        .request(request)
}

#[tauri::command]
pub fn runtime_restart(state: tauri::State<'_, RuntimeManagerState>) -> Result<Value, String> {
    state
        .manager
        .lock()
        .map_err(|_| "runtime_manager_unavailable".to_string())?
        .as_mut()
        .ok_or_else(|| "runtime_not_running".to_string())?
        .restart()
}

#[tauri::command]
pub fn runtime_stop(
    state: tauri::State<'_, RuntimeManagerState>,
) -> Result<RuntimeStopReceipt, String> {
    let mut guard = state
        .manager
        .lock()
        .map_err(|_| "runtime_manager_unavailable".to_string())?;
    let stopped = guard.as_ref().is_some_and(|manager| manager.is_running());
    if let Some(manager) = guard.as_mut() {
        manager.stop();
    }
    Ok(RuntimeStopReceipt { stopped })
}

fn validate_startup_handshake(response: &Value) -> Result<(), String> {
    if response.get("type").and_then(Value::as_str) == Some("error") {
        return match response.get("errorCode").and_then(Value::as_str) {
            Some("incompatible") => Err("runtime_version_mismatch".to_string()),
            Some("unauthorized") => Err("runtime_unauthorized".to_string()),
            _ => Err("runtime_handshake_invalid".to_string()),
        };
    }
    if response.get("type").and_then(Value::as_str) != Some("handshake") {
        return Err("runtime_handshake_invalid".to_string());
    }
    let handshake = response
        .get("handshake")
        .and_then(Value::as_object)
        .ok_or_else(|| "runtime_handshake_invalid".to_string())?;
    if handshake.get("protocolVersion").and_then(Value::as_u64) != Some(RUNTIME_PROTOCOL_VERSION) {
        return Err("runtime_version_mismatch".to_string());
    }
    if handshake.get("runtimeId").and_then(Value::as_str).is_none()
        || handshake.get("principal").and_then(Value::as_str).is_none()
        || handshake.get("role").and_then(Value::as_str).is_none()
        || handshake.get("source").and_then(Value::as_object).is_none()
    {
        return Err("runtime_handshake_invalid".to_string());
    }
    let source = handshake["source"]
        .as_object()
        .ok_or_else(|| "runtime_handshake_invalid".to_string())?;
    if source.get("mode").and_then(Value::as_str) != Some("local")
        || source.get("runtimeId") != handshake.get("runtimeId")
        || source.get("principal") != handshake.get("principal")
        || source.get("capabilities") != handshake.get("capabilities")
    {
        return Err("runtime_handshake_invalid".to_string());
    }
    Ok(())
}

fn validate_runtime_response(
    response: &Value,
    expected_id: &str,
    bearer: &str,
) -> Result<(), String> {
    let object = response
        .as_object()
        .ok_or_else(|| "runtime_response_invalid".to_string())?;
    if object.get("id").and_then(Value::as_str) != Some(expected_id)
        || object.get("type").and_then(Value::as_str).is_none()
        || object.contains_key("bearer")
        || response.to_string().contains(bearer)
    {
        return Err("runtime_response_invalid".to_string());
    }
    Ok(())
}

fn read_bounded_line<R: BufRead>(reader: &mut R, limit: usize) -> Result<Option<Vec<u8>>, String> {
    let mut line = Vec::new();
    loop {
        let available = reader
            .fill_buf()
            .map_err(|_| "runtime_crashed".to_string())?;
        if available.is_empty() {
            return if line.is_empty() {
                Ok(None)
            } else {
                Ok(Some(line))
            };
        }
        let newline = available.iter().position(|byte| *byte == b'\n');
        let consumed = newline.map_or(available.len(), |index| index + 1);
        let content = newline.unwrap_or(consumed);
        if line.len().saturating_add(content) > limit {
            return Err("runtime_response_too_large".to_string());
        }
        line.extend_from_slice(&available[..content]);
        reader.consume(consumed);
        if newline.is_some() {
            return Ok(Some(line));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::{
        collections::VecDeque,
        path::Path,
        sync::{Arc, Mutex},
        time::Duration,
    };

    struct ScriptedLauncher {
        responses: Arc<Mutex<VecDeque<Result<Value, String>>>>,
        secrets: Arc<Mutex<Vec<String>>>,
    }

    struct ScriptedProcess {
        responses: Arc<Mutex<VecDeque<Result<Value, String>>>>,
    }

    impl RuntimeLauncher for ScriptedLauncher {
        fn launch(
            &self,
            _executable: &Path,
            bearer: &str,
        ) -> Result<Box<dyn RuntimeProcess>, String> {
            self.secrets.lock().unwrap().push(bearer.to_string());
            Ok(Box::new(ScriptedProcess {
                responses: Arc::clone(&self.responses),
            }))
        }
    }

    impl RuntimeProcess for ScriptedProcess {
        fn request(&mut self, _request: Value, _timeout: Duration) -> Result<Value, String> {
            self.responses.lock().unwrap().pop_front().unwrap()
        }

        fn stop(&mut self) {}
    }

    fn manager_with_responses(
        responses: Vec<Result<Value, String>>,
    ) -> (RuntimeProcessManager, Arc<Mutex<Vec<String>>>) {
        let responses = Arc::new(Mutex::new(VecDeque::from(responses)));
        let secrets = Arc::new(Mutex::new(Vec::new()));
        let launcher = ScriptedLauncher {
            responses,
            secrets: Arc::clone(&secrets),
        };
        (
            RuntimeProcessManager::with_launcher(
                PathBuf::from("cyber-code"),
                Duration::from_millis(10),
                Box::new(launcher),
            ),
            secrets,
        )
    }

    fn handshake(version: u64) -> Value {
        json!({
            "id": "desktop-startup",
            "type": "handshake",
            "handshake": {
                "protocolVersion": version,
                "runtimeId": "runtime-1",
                "principal": "local-user",
                "role": "owner",
                "capabilities": ["events"],
                "source": {"mode":"local","runtimeId":"runtime-1","principal":"local-user","capabilities":["events"]}
            }
        })
    }

    #[test]
    fn webview_requests_cannot_supply_a_bearer() {
        let forged = json!({"id":"1","type":"health","bearer":"forged"});
        assert!(serde_json::from_value::<RuntimeBridgeRequest>(forged).is_err());

        let request: RuntimeBridgeRequest =
            serde_json::from_value(json!({"id":"1","type":"health"})).unwrap();
        let prepared = prepare_request(&request, "launch-secret").unwrap();
        assert_eq!(prepared["bearer"], "launch-secret");
        assert!(
            !serde_json::to_string(&request)
                .unwrap()
                .contains("launch-secret")
        );
    }

    #[test]
    fn production_discovery_uses_only_the_sibling_sidecar() {
        let current = Path::new("/Applications/CYBER.app/Contents/MacOS/cyber-desktop");
        let overridden = discover_runtime_executable(
            current,
            Some(Path::new("/tmp/developer-cyber-code")),
            false,
        )
        .unwrap();
        assert_eq!(
            overridden,
            Path::new("/Applications/CYBER.app/Contents/MacOS/cyber-code")
        );

        let development = discover_runtime_executable(
            current,
            Some(Path::new("/tmp/developer-cyber-code")),
            true,
        )
        .unwrap();
        assert_eq!(development, Path::new("/tmp/developer-cyber-code"));
    }

    #[test]
    fn every_runtime_launch_gets_a_distinct_secret() {
        let first = generate_launch_secret().unwrap();
        let second = generate_launch_secret().unwrap();
        assert_eq!(first.len(), 64);
        assert!(first.bytes().all(|byte| byte.is_ascii_hexdigit()));
        assert_ne!(first, second);
    }

    #[test]
    fn startup_secret_stays_native_and_successful_handshake_marks_running() {
        let (mut manager, secrets) = manager_with_responses(vec![Ok(handshake(1))]);
        let response = manager.start().unwrap();
        assert!(manager.is_running());
        assert_eq!(response["handshake"]["protocolVersion"], 1);
        let secret = secrets.lock().unwrap()[0].clone();
        assert!(!response.to_string().contains(&secret));
    }

    #[test]
    fn startup_timeout_or_crash_is_recoverable_by_explicit_restart() {
        for failure in ["runtime_startup_timeout", "runtime_crashed"] {
            let (mut manager, _) =
                manager_with_responses(vec![Err(failure.to_string()), Ok(handshake(1))]);
            assert_eq!(manager.start().unwrap_err(), failure);
            assert!(!manager.is_running());
            assert!(manager.restart().is_ok());
            assert!(manager.is_running());
        }
    }

    #[test]
    fn version_mismatch_never_falls_back_to_another_source() {
        let incompatible = json!({
            "id": "desktop-startup", "type": "error", "errorCode": "incompatible"
        });
        let (mut manager, secrets) = manager_with_responses(vec![Ok(incompatible)]);
        assert_eq!(manager.start().unwrap_err(), "runtime_version_mismatch");
        assert!(!manager.is_running());
        assert_eq!(secrets.lock().unwrap().len(), 1);
    }

    #[test]
    fn startup_rejects_mismatched_response_identity_or_non_local_source() {
        let mut mismatched_id = handshake(1);
        mismatched_id["id"] = json!("stale-request");
        let mut remote_source = handshake(1);
        remote_source["handshake"]["source"]["mode"] = json!("remote");
        for response in [mismatched_id, remote_source] {
            let (mut manager, _) = manager_with_responses(vec![Ok(response)]);
            assert_eq!(manager.start().unwrap_err(), "runtime_handshake_invalid");
            assert!(!manager.is_running());
        }
    }

    #[test]
    fn runtime_requests_reject_mismatched_or_credential_bearing_responses() {
        for response in [
            json!({"id":"other","type":"health","ready":true}),
            json!({"id":"health-1","type":"health","ready":true,"bearer":"leaked"}),
        ] {
            let (mut manager, _) = manager_with_responses(vec![Ok(handshake(1)), Ok(response)]);
            manager.start().unwrap();
            let request: RuntimeBridgeRequest =
                serde_json::from_value(json!({"id":"health-1","type":"health"})).unwrap();
            assert_eq!(
                manager.request(request).unwrap_err(),
                "runtime_response_invalid"
            );
            assert!(!manager.is_running());
        }
    }
}
