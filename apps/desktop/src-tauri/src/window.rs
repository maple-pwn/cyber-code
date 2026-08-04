use tauri::{Manager, PhysicalPosition, PhysicalSize, Position, Size};

const MIN_WINDOW_WIDTH: i32 = 1_024;
const MIN_WINDOW_HEIGHT: i32 = 768;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct WindowRect {
    pub x: i32,
    pub y: i32,
    pub width: i32,
    pub height: i32,
}

pub fn clamp_window_rect(restored: WindowRect, monitor: WindowRect) -> WindowRect {
    let width = restored.width.max(MIN_WINDOW_WIDTH).min(monitor.width);
    let height = restored.height.max(MIN_WINDOW_HEIGHT).min(monitor.height);
    let max_x = monitor
        .x
        .saturating_add(monitor.width.saturating_sub(width));
    let max_y = monitor
        .y
        .saturating_add(monitor.height.saturating_sub(height));
    WindowRect {
        x: restored.x.clamp(monitor.x, max_x),
        y: restored.y.clamp(monitor.y, max_y),
        width,
        height,
    }
}

pub fn secondary_launch_allowed(arguments: &[String]) -> bool {
    arguments.is_empty()
}

pub fn focus_main_window(app: &tauri::AppHandle) {
    let Some(window) = app.get_webview_window("main") else {
        return;
    };
    let _ = window.unminimize();
    let _ = window.show();
    let _ = window.set_focus();
}

pub fn clamp_main_window(window: &tauri::WebviewWindow) -> Result<(), String> {
    let Some(monitor) = window
        .current_monitor()
        .map_err(|_| "current monitor is unavailable".to_string())?
    else {
        return Ok(());
    };
    let position = window
        .outer_position()
        .map_err(|_| "window position is unavailable".to_string())?;
    let size = window
        .outer_size()
        .map_err(|_| "window size is unavailable".to_string())?;
    let monitor_position = monitor.position();
    let monitor_size = monitor.size();
    let clamped = clamp_window_rect(
        WindowRect {
            x: position.x,
            y: position.y,
            width: i32::try_from(size.width).unwrap_or(i32::MAX),
            height: i32::try_from(size.height).unwrap_or(i32::MAX),
        },
        WindowRect {
            x: monitor_position.x,
            y: monitor_position.y,
            width: i32::try_from(monitor_size.width).unwrap_or(i32::MAX),
            height: i32::try_from(monitor_size.height).unwrap_or(i32::MAX),
        },
    );
    window
        .set_size(Size::Physical(PhysicalSize::new(
            u32::try_from(clamped.width).unwrap_or(1_024),
            u32::try_from(clamped.height).unwrap_or(768),
        )))
        .map_err(|_| "window size could not be restored".to_string())?;
    window
        .set_position(Position::Physical(PhysicalPosition::new(
            clamped.x, clamped.y,
        )))
        .map_err(|_| "window position could not be restored".to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn restored_window_is_clamped_to_the_current_monitor() {
        let monitor = WindowRect {
            x: 0,
            y: 0,
            width: 1_920,
            height: 1_080,
        };
        let offscreen = WindowRect {
            x: 5_000,
            y: 4_000,
            width: 1_280,
            height: 800,
        };

        assert_eq!(
            clamp_window_rect(offscreen, monitor),
            WindowRect {
                x: 640,
                y: 280,
                width: 1_280,
                height: 800
            }
        );
    }

    #[test]
    fn restored_window_never_shrinks_below_the_supported_layout() {
        let monitor = WindowRect {
            x: 0,
            y: 0,
            width: 1_920,
            height: 1_080,
        };
        let tiny = WindowRect {
            x: 20,
            y: 20,
            width: 400,
            height: 300,
        };

        assert_eq!(
            clamp_window_rect(tiny, monitor),
            WindowRect {
                x: 20,
                y: 20,
                width: 1_024,
                height: 768
            }
        );
    }

    #[test]
    fn secondary_launch_rejects_deep_links_and_external_urls() {
        assert!(secondary_launch_allowed(&[]));
        assert!(!secondary_launch_allowed(&[
            "cyber://mission/reports".into()
        ]));
        assert!(!secondary_launch_allowed(&[
            "https://attacker.example".into()
        ]));
        assert!(!secondary_launch_allowed(&["file:///tmp/payload".into()]));
    }
}
