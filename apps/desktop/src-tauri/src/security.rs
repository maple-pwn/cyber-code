pub fn is_navigation_allowed(url: &tauri::Url, development: bool) -> bool {
    if !url.username().is_empty() || url.password().is_some() {
        return false;
    }

    let packaged_origin =
        (url.scheme() == "tauri" && url.host_str() == Some("localhost") && url.port().is_none())
            || (url.scheme() == "http"
                && url.host_str() == Some("tauri.localhost")
                && url.port().is_none());
    let development_origin = development
        && url.scheme() == "http"
        && url.host_str() == Some("127.0.0.1")
        && url.port() == Some(1_420);

    packaged_origin || development_origin
}

#[cfg(test)]
mod tests {
    use super::*;

    fn url(value: &str) -> tauri::Url {
        value.parse().unwrap()
    }

    #[test]
    fn navigation_accepts_only_packaged_app_origins_and_the_exact_dev_server() {
        assert!(is_navigation_allowed(
            &url("tauri://localhost/reports"),
            false
        ));
        assert!(is_navigation_allowed(
            &url("http://tauri.localhost/findings"),
            false
        ));
        assert!(is_navigation_allowed(
            &url("http://127.0.0.1:1420/mission-control"),
            true
        ));

        for candidate in [
            "https://attacker.example/payload",
            "http://127.0.0.1:1421/payload",
            "http://localhost:1420/payload",
            "file:///tmp/payload.html",
            "javascript:alert(1)",
        ] {
            assert!(!is_navigation_allowed(&url(candidate), true), "{candidate}");
        }
    }
}
