use crate::error::AgentError;
use async_trait::async_trait;
use std::process::Stdio;
use tokio::io::AsyncReadExt;
use tokio::process::Command;

#[async_trait]
pub trait ScreenSource: Send + Sync + 'static {
    /// Returns PNG-encoded bytes of the current display.
    async fn capture(&self) -> Result<Vec<u8>, AgentError>;
}

/// Production impl: shells out to `scrot --silent --overwrite /tmp/qdesk_capture.png`,
/// then reads the file back. Uses a fixed temp-file path — see [`ScrotScreen::capture`]
/// for concurrency caveats.
#[derive(Clone)]
pub struct ScrotScreen {
    pub display: String, // e.g. ":99"
}

#[async_trait]
impl ScreenSource for ScrotScreen {
    async fn capture(&self) -> Result<Vec<u8>, AgentError> {
        // NOTE: This writes to a fixed path. Concurrent calls will race on the file.
        // Acceptable for Phase 0 (single-tenant container). Fix before multi-session support.
        let child = Command::new("scrot")
            .arg("--silent")
            .arg("--overwrite")
            .arg("/tmp/qdesk_capture.png")
            .env("DISPLAY", &self.display)
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| AgentError::Capture(format!("spawn scrot: {e}")))?;
        let output = child
            .wait_with_output()
            .await
            .map_err(|e| AgentError::Capture(format!("wait scrot: {e}")))?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            return Err(AgentError::Capture(format!(
                "scrot exited {}: {}",
                output.status,
                stderr.trim()
            )));
        }
        let mut bytes = Vec::new();
        let mut f = tokio::fs::File::open("/tmp/qdesk_capture.png")
            .await
            .map_err(|e| AgentError::Capture(format!("open output: {e}")))?;
        f.read_to_end(&mut bytes)
            .await
            .map_err(|e| AgentError::Capture(format!("read output: {e}")))?;
        Ok(bytes)
    }
}

#[cfg(test)]
pub mod test_support {
    use super::*;
    use std::sync::Arc;
    use std::sync::atomic::{AtomicU32, Ordering};
    use tokio::sync::Mutex;

    /// Test impl: returns a fixed PNG byte sequence and counts calls.
    #[derive(Clone, Default)]
    pub struct MockScreen {
        pub bytes: Arc<Mutex<Vec<u8>>>,
        pub calls: Arc<AtomicU32>,
    }

    impl MockScreen {
        pub fn with_png(bytes: Vec<u8>) -> Self {
            Self {
                bytes: Arc::new(Mutex::new(bytes)),
                calls: Arc::new(AtomicU32::new(0)),
            }
        }

        pub fn call_count(&self) -> u32 {
            self.calls.load(Ordering::Relaxed)
        }
    }

    #[async_trait]
    impl ScreenSource for MockScreen {
        async fn capture(&self) -> Result<Vec<u8>, AgentError> {
            self.calls.fetch_add(1, Ordering::Relaxed);
            Ok(self.bytes.lock().await.clone())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::test_support::*;
    use super::*;

    #[tokio::test]
    async fn mock_screen_returns_bytes_and_counts() {
        let m = MockScreen::with_png(vec![0x89, 0x50, 0x4E, 0x47]); // PNG magic
        let out = m.capture().await.unwrap();
        assert_eq!(out, vec![0x89, 0x50, 0x4E, 0x47]);
        assert_eq!(m.call_count(), 1);
    }
}
