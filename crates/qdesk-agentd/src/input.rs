use crate::error::AgentError;
use async_trait::async_trait;
use qdesk_protocol::{Action, MouseButton};
use std::process::Stdio;
use tokio::process::Command;

#[async_trait]
pub trait InputDriver: Send + Sync + 'static {
    async fn execute(&self, action: &Action) -> Result<(), AgentError>;
}

/// Production impl: shells out to xdotool with the given DISPLAY.
#[derive(Clone)]
pub struct XdotoolInput {
    pub display: String,
}

impl XdotoolInput {
    fn cmd(&self) -> Command {
        let mut c = Command::new("xdotool");
        c.env("DISPLAY", &self.display);
        c.stdout(Stdio::null()).stderr(Stdio::piped());
        c
    }

    async fn run(&self, args: Vec<String>) -> Result<(), AgentError> {
        let child = self
            .cmd()
            .args(&args)
            .spawn()
            .map_err(|e| AgentError::Input(format!("spawn xdotool {args:?}: {e}")))?;
        let output = child
            .wait_with_output()
            .await
            .map_err(|e| AgentError::Input(format!("wait xdotool {args:?}: {e}")))?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            return Err(AgentError::Input(format!(
                "xdotool {args:?} -> {}: {}",
                output.status,
                stderr.trim()
            )));
        }
        Ok(())
    }
}

fn button_num(b: MouseButton) -> &'static str {
    match b {
        MouseButton::Left => "1",
        MouseButton::Middle => "2",
        MouseButton::Right => "3",
    }
}

#[async_trait]
impl InputDriver for XdotoolInput {
    async fn execute(&self, action: &Action) -> Result<(), AgentError> {
        match action {
            Action::Click { x, y, button } => {
                self.run(vec![
                    "mousemove".into(),
                    x.to_string(),
                    y.to_string(),
                    "click".into(),
                    button_num(*button).into(),
                ])
                .await
            }
            other => Err(AgentError::BadRequest(format!(
                "action not yet implemented: {other:?}"
            ))),
        }
    }
}

#[cfg(test)]
pub mod test_support {
    use super::*;
    use std::sync::Arc;
    use tokio::sync::Mutex;

    #[derive(Clone, Default)]
    pub struct MockInput {
        pub recorded: Arc<Mutex<Vec<Action>>>,
    }

    #[async_trait]
    impl InputDriver for MockInput {
        async fn execute(&self, action: &Action) -> Result<(), AgentError> {
            self.recorded.lock().await.push(action.clone());
            Ok(())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::test_support::*;
    use super::*;

    #[tokio::test]
    async fn mock_input_records_action() {
        let m = MockInput::default();
        let act = Action::Click { x: 10, y: 20, button: MouseButton::Left };
        m.execute(&act).await.unwrap();
        let recorded = m.recorded.lock().await;
        assert_eq!(recorded.len(), 1);
        assert_eq!(recorded[0], act);
    }
}
