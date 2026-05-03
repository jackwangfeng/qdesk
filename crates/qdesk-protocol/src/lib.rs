use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum Action {
    Click {
        x: i32,
        y: i32,
        #[serde(default)]
        button: MouseButton,
    },
    Type {
        text: String,
    },
    Key {
        keys: Vec<String>,
    },
    Scroll {
        x: i32,
        y: i32,
        dx: i32,
        dy: i32,
    },
    Drag {
        from: (i32, i32),
        to: (i32, i32),
    },
    Wait {
        ms: u64,
    },
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Default)]
#[serde(rename_all = "snake_case")]
pub enum MouseButton {
    #[default]
    Left,
    Right,
    Middle,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ActionResult {
    pub ok: bool,
    pub screen_changed: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HealthResponse {
    pub status: String,
    pub version: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn click_action_round_trips() {
        let a = Action::Click { x: 10, y: 20, button: MouseButton::Left };
        let json = serde_json::to_string(&a).unwrap();
        assert_eq!(json, r#"{"type":"click","x":10,"y":20,"button":"left"}"#);
        let back: Action = serde_json::from_str(&json).unwrap();
        assert_eq!(back, a);
    }

    #[test]
    fn click_button_defaults_to_left() {
        let a: Action = serde_json::from_str(r#"{"type":"click","x":5,"y":6}"#).unwrap();
        assert_eq!(a, Action::Click { x: 5, y: 6, button: MouseButton::Left });
    }

    #[test]
    fn type_action_round_trips() {
        let a = Action::Type { text: "hello".into() };
        let json = serde_json::to_string(&a).unwrap();
        assert_eq!(json, r#"{"type":"type","text":"hello"}"#);
    }

    #[test]
    fn key_action_round_trips() {
        let a = Action::Key { keys: vec!["ctrl".into(), "s".into()] };
        let json = serde_json::to_string(&a).unwrap();
        let back: Action = serde_json::from_str(&json).unwrap();
        assert_eq!(back, a);
    }

    #[test]
    fn drag_action_round_trips() {
        let a = Action::Drag { from: (1, 2), to: (3, 4) };
        let back: Action = serde_json::from_str(&serde_json::to_string(&a).unwrap()).unwrap();
        assert_eq!(back, a);
    }
}
