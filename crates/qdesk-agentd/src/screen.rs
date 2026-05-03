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

/// Production impl: shells out to `scrot - -` to write PNG to stdout.
#[derive(Clone)]
pub struct ScrotScreen {
    pub display: String, // e.g. ":99"
}

#[async_trait]
impl ScreenSource for ScrotScreen {
    async fn capture(&self) -> Result<Vec<u8>, AgentError> {
        let mut child = Command::new("scrot")
            .arg("--silent")
            .arg("--overwrite")
            .arg("/tmp/qdesk_capture.png")
            .env("DISPLAY", &self.display)
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| AgentError::Capture(format!("spawn scrot: {e}")))?;
        let status = child
            .wait()
            .await
            .map_err(|e| AgentError::Capture(format!("wait scrot: {e}")))?;
        if !status.success() {
            return Err(AgentError::Capture(format!("scrot exited {status}")));
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
    use tokio::sync::Mutex;

    /// Test impl: returns a fixed PNG byte sequence and counts calls.
    #[derive(Clone, Default)]
    pub struct MockScreen {
        pub bytes: Arc<Mutex<Vec<u8>>>,
        pub calls: Arc<Mutex<u32>>,
    }

    impl MockScreen {
        pub fn with_png(bytes: Vec<u8>) -> Self {
            Self {
                bytes: Arc::new(Mutex::new(bytes)),
                calls: Arc::new(Mutex::new(0)),
            }
        }
    }

    #[async_trait]
    impl ScreenSource for MockScreen {
        async fn capture(&self) -> Result<Vec<u8>, AgentError> {
            *self.calls.lock().await += 1;
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
        assert_eq!(*m.calls.lock().await, 1);
    }
}
