# qdesk Phase 0 — Plan 1: Sandbox + agentd

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the foundational Linux sandbox container and the in-sandbox HTTP daemon (`qdesk-agentd`) that exposes screenshot + input-injection over HTTP. After this plan, you can `docker run` a sandbox and drive it from outside via curl.

**Architecture:** A Docker image based on Ubuntu with Xvfb (virtual display), xfce4 (window manager), and Chromium pre-installed. Inside the container runs `qdesk-agentd`, a Rust HTTP server (axum) on port 7878 that exposes `/health`, `/screenshot`, and `/actions` endpoints. The daemon shells out to `scrot` for screenshots and `xdotool` for input injection. Trait-based design (`ScreenSource`, `InputDriver`) makes unit-testing possible without needing X11.

**Tech Stack:** Rust 1.80+, axum 0.7, tokio, serde, anyhow, tracing, tower (test utility), Docker (Ubuntu 24.04 base), Xvfb, xfce4, chromium, scrot, xdotool.

**Spec reference:** `docs/superpowers/specs/2026-05-03-qdesk-design.md` §4.2, §5, §6.2 (sessions/actions/screenshot APIs only — DOM and snapshots are out of scope for Phase 0).

**Independence:** This plan is self-contained — its output (the sandbox image + agentd binary) is testable on its own via curl. Plan 2 (control plane) consumes this; Plan 3 (runner) consumes Plan 2.

---

## File Structure (locked in here)

```
qdesk/
├── Cargo.toml                          # workspace root
├── .gitignore
├── README.md
├── crates/
│   ├── qdesk-protocol/
│   │   ├── Cargo.toml
│   │   └── src/
│   │       └── lib.rs                  # Action enum, ActionResult, common types
│   └── qdesk-agentd/
│       ├── Cargo.toml
│       └── src/
│           ├── main.rs                 # binary entrypoint: parse args, start server
│           ├── server.rs               # axum router + handlers
│           ├── screen.rs               # ScreenSource trait + ScrotScreen impl
│           ├── input.rs                # InputDriver trait + XdotoolInput impl
│           └── error.rs                # AgentError -> HTTP response
├── images/
│   └── ubuntu-chrome/
│       ├── Dockerfile                  # build the sandbox image
│       └── entrypoint.sh               # start Xvfb, xfce4, agentd in order
└── docs/
    └── superpowers/                    # specs & plans (already exists)
```

**Why this layout:**
- `qdesk-protocol` is a tiny shared crate so Plan 2 (control plane) and Plan 3 (runner) can reuse `Action` types without duplication.
- `screen.rs` and `input.rs` are split by responsibility — both expose a trait for unit testing without X11.
- `server.rs` handlers are thin glue; logic lives in trait impls.
- `images/` is for container builds (kept out of `crates/` because not Rust).

---

## Pre-Plan Setup

- [ ] **Initialize git repo at `/home/jeffwang/work/qdesk`**

```bash
cd /home/jeffwang/work/qdesk
git init -b main
```

Expected: `Initialized empty Git repository in /home/jeffwang/work/qdesk/.git/`

- [ ] **Stage existing files**

```bash
git add LICENSE docs/
git status
```

Expected: `LICENSE`, `docs/superpowers/specs/...md`, `docs/superpowers/plans/...md` shown as new files.

- [ ] **First commit**

```bash
git commit -m "chore: initial spec, plan, license"
```

---

## Task 1: Cargo workspace + .gitignore + README

**Files:**
- Create: `Cargo.toml` (workspace)
- Create: `.gitignore`
- Create: `README.md`

- [ ] **Step 1: Create `.gitignore`**

```gitignore
/target
**/*.rs.bk
.env
*.log
.DS_Store
.vscode/
.idea/
```

> **Note:** `Cargo.lock` is intentionally NOT gitignored. This workspace contains a binary crate (`qdesk-agentd`); Cargo's guidance is to commit `Cargo.lock` for binary crates so Docker builds and CI are reproducible.

- [ ] **Step 2: Create `Cargo.toml` (workspace root)**

```toml
[workspace]
resolver = "2"
members = [
    "crates/qdesk-protocol",
    "crates/qdesk-agentd",
]

[workspace.package]
version = "0.1.0"
edition = "2021"
license = "Apache-2.0"
repository = "https://github.com/jeffwang/qdesk"

[workspace.dependencies]
anyhow = "1"
axum = "0.7"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
tokio = { version = "1", features = ["full"] }
tower = "0.5"
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter"] }
thiserror = "1"
http = "1"
http-body-util = "0.1"
mime = "0.3"
async-trait = "0.1"
clap = { version = "4", features = ["derive"] }
```

- [ ] **Step 3: Create `README.md`**

```markdown
# qdesk

AI-native testing platform — describe tests in natural language, AI agents execute them in cloud sandboxes, tests self-heal when UIs change.

**Status:** Phase 0 (MVP). See `docs/superpowers/specs/` for design and `docs/superpowers/plans/` for implementation plans.

## Quickstart (Phase 0 sandbox)

```bash
docker build -t qdesk/ubuntu-chrome:dev -f images/ubuntu-chrome/Dockerfile .
docker run -d -p 7878:7878 --name qdesk-sbx qdesk/ubuntu-chrome:dev
curl http://localhost:7878/health
curl http://localhost:7878/screenshot --output /tmp/screen.png
```

## License

Apache 2.0 — see `LICENSE`.
```

- [ ] **Step 4: Commit**

```bash
git add Cargo.toml .gitignore README.md
git commit -m "chore: cargo workspace + gitignore + readme"
```

---

## Task 2: `qdesk-protocol` crate — shared types

**Files:**
- Create: `crates/qdesk-protocol/Cargo.toml`
- Create: `crates/qdesk-protocol/src/lib.rs`

- [ ] **Step 1: Write the failing test**

Create `crates/qdesk-protocol/Cargo.toml`:

```toml
[package]
name = "qdesk-protocol"
version.workspace = true
edition.workspace = true
license.workspace = true

[dependencies]
serde = { workspace = true }
serde_json = { workspace = true }
```

Create `crates/qdesk-protocol/src/lib.rs` (just enough to compile, with failing test):

```rust
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq)]
pub struct Point {
    pub x: i32,
    pub y: i32,
}

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
        from: Point,
        to: Point,
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
        let a = Action::Drag {
            from: Point { x: 1, y: 2 },
            to: Point { x: 3, y: 4 },
        };
        let json = serde_json::to_string(&a).unwrap();
        assert_eq!(
            json,
            r#"{"type":"drag","from":{"x":1,"y":2},"to":{"x":3,"y":4}}"#
        );
        let back: Action = serde_json::from_str(&json).unwrap();
        assert_eq!(back, a);
    }
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cargo test -p qdesk-protocol`
Expected: `5 passed`

- [ ] **Step 3: Commit**

```bash
git add crates/qdesk-protocol
git commit -m "feat(protocol): action enum and result types"
```

---

## Task 3: `qdesk-agentd` skeleton + health endpoint

**Files:**
- Create: `crates/qdesk-agentd/Cargo.toml`
- Create: `crates/qdesk-agentd/src/main.rs`
- Create: `crates/qdesk-agentd/src/server.rs`
- Create: `crates/qdesk-agentd/src/error.rs`

- [ ] **Step 1: Write the failing test (in `server.rs`)**

Create `crates/qdesk-agentd/Cargo.toml`:

```toml
[package]
name = "qdesk-agentd"
version.workspace = true
edition.workspace = true
license.workspace = true

[dependencies]
qdesk-protocol = { path = "../qdesk-protocol" }
anyhow = { workspace = true }
async-trait = { workspace = true }
axum = { workspace = true }
clap = { workspace = true }
http = { workspace = true }
serde = { workspace = true }
serde_json = { workspace = true }
thiserror = { workspace = true }
tokio = { workspace = true }
tower = { workspace = true }
tracing = { workspace = true }
tracing-subscriber = { workspace = true }

[dev-dependencies]
http-body-util = { workspace = true }
mime = { workspace = true }
```

Create `crates/qdesk-agentd/src/error.rs`:

```rust
use axum::{http::StatusCode, response::{IntoResponse, Response}, Json};
use serde_json::json;

#[derive(Debug, thiserror::Error)]
pub enum AgentError {
    #[error("input failed: {0}")]
    Input(String),
    #[error("screen capture failed: {0}")]
    Capture(String),
    #[error("invalid request: {0}")]
    BadRequest(String),
}

impl IntoResponse for AgentError {
    fn into_response(self) -> Response {
        let status = match self {
            AgentError::BadRequest(_) => StatusCode::BAD_REQUEST,
            _ => StatusCode::INTERNAL_SERVER_ERROR,
        };
        let body = Json(json!({ "error": self.to_string() }));
        (status, body).into_response()
    }
}
```

Create `crates/qdesk-agentd/src/server.rs`:

```rust
use axum::{routing::get, Json, Router};
use qdesk_protocol::HealthResponse;

pub fn router() -> Router {
    Router::new().route("/health", get(health))
}

async fn health() -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "ok".into(),
        version: env!("CARGO_PKG_VERSION").into(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use http::{Request, StatusCode};
    use http_body_util::BodyExt;
    use tower::ServiceExt;

    #[tokio::test]
    async fn health_returns_ok() {
        let app = router();
        let resp = app
            .oneshot(Request::builder().uri("/health").body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
        let bytes = resp.into_body().collect().await.unwrap().to_bytes();
        let body: HealthResponse = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(body.status, "ok");
        assert!(!body.version.is_empty());
    }
}
```

Create `crates/qdesk-agentd/src/main.rs`:

```rust
mod error;
mod server;

use clap::Parser;
use tracing_subscriber::EnvFilter;

#[derive(Parser)]
#[command(name = "qdesk-agentd")]
#[command(about = "qdesk in-sandbox HTTP daemon")]
struct Cli {
    #[arg(long, default_value = "0.0.0.0:7878")]
    listen: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()))
        .init();

    let cli = Cli::parse();
    let app = server::router();
    let listener = tokio::net::TcpListener::bind(&cli.listen).await?;
    tracing::info!(addr = %cli.listen, "qdesk-agentd listening");
    axum::serve(listener, app).await?;
    Ok(())
}
```

Add the crate to workspace `members` in root `Cargo.toml` if not already there (it was added in Task 1).

- [ ] **Step 2: Run tests**

Run: `cargo test -p qdesk-agentd`
Expected: `1 passed` — `health_returns_ok`

- [ ] **Step 3: Run binary manually to sanity check**

Run:
```bash
cargo run -p qdesk-agentd -- --listen 127.0.0.1:7878 &
sleep 1
curl -s http://127.0.0.1:7878/health
kill %1
```
Expected output: `{"status":"ok","version":"0.1.0"}`

- [ ] **Step 4: Commit**

```bash
git add crates/qdesk-agentd
git commit -m "feat(agentd): skeleton with health endpoint"
```

---

## Task 4: `ScreenSource` trait + screenshot endpoint

**Files:**
- Create: `crates/qdesk-agentd/src/screen.rs`
- Modify: `crates/qdesk-agentd/src/server.rs`
- Modify: `crates/qdesk-agentd/src/main.rs`

- [ ] **Step 1: Write the failing test**

Create `crates/qdesk-agentd/src/screen.rs`:

```rust
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
```

Update `crates/qdesk-agentd/src/server.rs`:

```rust
use crate::screen::ScreenSource;
use axum::{
    extract::State,
    http::{header, StatusCode},
    response::{IntoResponse, Response},
    routing::get,
    Json, Router,
};
use qdesk_protocol::HealthResponse;
use std::sync::Arc;

#[derive(Clone)]
pub struct AppState {
    pub screen: Arc<dyn ScreenSource>,
}

pub fn router(state: AppState) -> Router {
    Router::new()
        .route("/health", get(health))
        .route("/screenshot", get(screenshot))
        .with_state(state)
}

async fn health() -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "ok".into(),
        version: env!("CARGO_PKG_VERSION").into(),
    })
}

async fn screenshot(State(s): State<AppState>) -> Response {
    match s.screen.capture().await {
        Ok(bytes) => (
            StatusCode::OK,
            [(header::CONTENT_TYPE, "image/png")],
            bytes,
        )
            .into_response(),
        Err(e) => e.into_response(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::screen::test_support::MockScreen;
    use axum::body::Body;
    use http::Request;
    use http_body_util::BodyExt;
    use tower::ServiceExt;

    fn test_state() -> AppState {
        AppState {
            screen: Arc::new(MockScreen::with_png(vec![0x89, 0x50, 0x4E, 0x47])),
        }
    }

    #[tokio::test]
    async fn health_returns_ok() {
        let app = router(test_state());
        let resp = app
            .oneshot(Request::builder().uri("/health").body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
    }

    #[tokio::test]
    async fn screenshot_returns_png_bytes() {
        let app = router(test_state());
        let resp = app
            .oneshot(Request::builder().uri("/screenshot").body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
        assert_eq!(
            resp.headers().get(header::CONTENT_TYPE).unwrap(),
            "image/png"
        );
        let bytes = resp.into_body().collect().await.unwrap().to_bytes();
        assert_eq!(&bytes[..4], &[0x89, 0x50, 0x4E, 0x47]);
    }
}
```

Update `crates/qdesk-agentd/src/main.rs`:

```rust
mod error;
mod screen;
mod server;

use clap::Parser;
use server::AppState;
use std::sync::Arc;
use tracing_subscriber::EnvFilter;

#[derive(Parser)]
#[command(name = "qdesk-agentd")]
struct Cli {
    #[arg(long, default_value = "0.0.0.0:7878")]
    listen: String,
    #[arg(long, default_value = ":99")]
    display: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()))
        .init();

    let cli = Cli::parse();
    let state = AppState {
        screen: Arc::new(screen::ScrotScreen { display: cli.display.clone() }),
    };
    let app = server::router(state);
    let listener = tokio::net::TcpListener::bind(&cli.listen).await?;
    tracing::info!(addr = %cli.listen, display = %cli.display, "qdesk-agentd listening");
    axum::serve(listener, app).await?;
    Ok(())
}
```

- [ ] **Step 2: Run tests**

Run: `cargo test -p qdesk-agentd`
Expected: 3 passed (mock_screen_returns_bytes_and_counts, health_returns_ok, screenshot_returns_png_bytes)

- [ ] **Step 3: Commit**

```bash
git add crates/qdesk-agentd/src
git commit -m "feat(agentd): screenshot endpoint with ScreenSource trait"
```

---

## Task 5: `InputDriver` trait + click action

**Files:**
- Create: `crates/qdesk-agentd/src/input.rs`
- Modify: `crates/qdesk-agentd/src/server.rs`
- Modify: `crates/qdesk-agentd/src/main.rs`

- [ ] **Step 1: Write the failing test**

Create `crates/qdesk-agentd/src/input.rs`:

```rust
use crate::error::AgentError;
use async_trait::async_trait;
use qdesk_protocol::{Action, MouseButton};
use tokio::process::Command;

#[async_trait]
pub trait InputDriver: Send + Sync + 'static {
    async fn execute(&self, action: &Action) -> Result<(), AgentError>;
}

#[derive(Clone)]
pub struct XdotoolInput {
    pub display: String,
}

impl XdotoolInput {
    fn cmd(&self) -> Command {
        let mut c = Command::new("xdotool");
        c.env("DISPLAY", &self.display);
        c
    }

    async fn run(&self, args: Vec<String>) -> Result<(), AgentError> {
        let status = self
            .cmd()
            .args(&args)
            .status()
            .await
            .map_err(|e| AgentError::Input(format!("spawn xdotool {args:?}: {e}")))?;
        if !status.success() {
            return Err(AgentError::Input(format!("xdotool {args:?} -> {status}")));
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
```

Update `crates/qdesk-agentd/src/server.rs` (add `input` field + `/actions` POST handler):

```rust
use crate::input::InputDriver;
use crate::screen::ScreenSource;
use axum::{
    extract::State,
    http::{header, StatusCode},
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use qdesk_protocol::{Action, ActionResult, HealthResponse};
use std::sync::Arc;

#[derive(Clone)]
pub struct AppState {
    pub screen: Arc<dyn ScreenSource>,
    pub input: Arc<dyn InputDriver>,
}

pub fn router(state: AppState) -> Router {
    Router::new()
        .route("/health", get(health))
        .route("/screenshot", get(screenshot))
        .route("/actions", post(actions))
        .with_state(state)
}

async fn health() -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "ok".into(),
        version: env!("CARGO_PKG_VERSION").into(),
    })
}

async fn screenshot(State(s): State<AppState>) -> Response {
    match s.screen.capture().await {
        Ok(bytes) => (
            StatusCode::OK,
            [(header::CONTENT_TYPE, "image/png")],
            bytes,
        )
            .into_response(),
        Err(e) => e.into_response(),
    }
}

async fn actions(
    State(s): State<AppState>,
    Json(action): Json<Action>,
) -> Result<Json<ActionResult>, crate::error::AgentError> {
    s.input.execute(&action).await?;
    Ok(Json(ActionResult { ok: true, screen_changed: true }))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::input::test_support::MockInput;
    use crate::screen::test_support::MockScreen;
    use axum::body::Body;
    use http::Request;
    use http_body_util::BodyExt;
    use qdesk_protocol::MouseButton;
    use tower::ServiceExt;

    fn state() -> (AppState, MockInput) {
        let input = MockInput::default();
        let s = AppState {
            screen: Arc::new(MockScreen::with_png(vec![0x89, 0x50, 0x4E, 0x47])),
            input: Arc::new(input.clone()),
        };
        (s, input)
    }

    #[tokio::test]
    async fn click_action_is_dispatched() {
        let (s, input) = state();
        let app = router(s);
        let body = serde_json::to_vec(&Action::Click { x: 100, y: 200, button: MouseButton::Left })
            .unwrap();
        let resp = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/actions")
                    .header("content-type", "application/json")
                    .body(Body::from(body))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
        let bytes = resp.into_body().collect().await.unwrap().to_bytes();
        let result: ActionResult = serde_json::from_slice(&bytes).unwrap();
        assert!(result.ok);
        let recorded = input.recorded.lock().await;
        assert_eq!(recorded.len(), 1);
    }

    #[tokio::test]
    async fn malformed_action_returns_400() {
        let (s, _input) = state();
        let app = router(s);
        let resp = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/actions")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"type":"not_a_real_action"}"#))
                    .unwrap(),
            )
            .await
            .unwrap();
        // axum's Json extractor returns 422 (Unprocessable Entity) on bad JSON.
        assert!(resp.status().is_client_error());
    }
}
```

Update `crates/qdesk-agentd/src/main.rs`:

```rust
mod error;
mod input;
mod screen;
mod server;

use clap::Parser;
use server::AppState;
use std::sync::Arc;
use tracing_subscriber::EnvFilter;

#[derive(Parser)]
#[command(name = "qdesk-agentd")]
struct Cli {
    #[arg(long, default_value = "0.0.0.0:7878")]
    listen: String,
    #[arg(long, default_value = ":99")]
    display: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()))
        .init();

    let cli = Cli::parse();
    let state = AppState {
        screen: Arc::new(screen::ScrotScreen { display: cli.display.clone() }),
        input: Arc::new(input::XdotoolInput { display: cli.display.clone() }),
    };
    let app = server::router(state);
    let listener = tokio::net::TcpListener::bind(&cli.listen).await?;
    tracing::info!(addr = %cli.listen, display = %cli.display, "qdesk-agentd listening");
    axum::serve(listener, app).await?;
    Ok(())
}
```

- [ ] **Step 2: Run tests**

Run: `cargo test -p qdesk-agentd`
Expected: 5 passed.

- [ ] **Step 3: Commit**

```bash
git add crates/qdesk-agentd/src
git commit -m "feat(agentd): InputDriver trait + click action endpoint"
```

---

## Task 6: Type & Key actions

**Files:**
- Modify: `crates/qdesk-agentd/src/input.rs:execute()`

- [ ] **Step 1: Write the failing tests**

Add to `crates/qdesk-agentd/src/input.rs` (extend the `tests` module):

```rust
#[cfg(test)]
mod more_tests {
    use super::test_support::*;
    use super::*;

    #[tokio::test]
    async fn mock_records_type_action() {
        let m = MockInput::default();
        m.execute(&Action::Type { text: "hello".into() }).await.unwrap();
        assert_eq!(
            m.recorded.lock().await[0],
            Action::Type { text: "hello".into() }
        );
    }

    #[tokio::test]
    async fn mock_records_key_action() {
        let m = MockInput::default();
        let a = Action::Key { keys: vec!["ctrl".into(), "s".into()] };
        m.execute(&a).await.unwrap();
        assert_eq!(m.recorded.lock().await[0], a);
    }
}
```

- [ ] **Step 2: Run them to verify mock-side passes already (it's generic)**

Run: `cargo test -p qdesk-agentd input::more_tests`
Expected: 2 passed.

- [ ] **Step 3: Implement Type & Key in `XdotoolInput::execute`**

Replace the `match` arms in the `XdotoolInput` impl of `execute` with:

```rust
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
            Action::Type { text } => {
                self.run(vec!["type".into(), "--delay".into(), "10".into(), text.clone()])
                    .await
            }
            Action::Key { keys } => {
                // xdotool key uses + to combine modifiers, e.g. "ctrl+s"
                let combo = keys.join("+");
                self.run(vec!["key".into(), combo]).await
            }
            other => Err(AgentError::BadRequest(format!(
                "action not yet implemented: {other:?}"
            ))),
        }
    }
}
```

- [ ] **Step 4: Run all tests**

Run: `cargo test -p qdesk-agentd`
Expected: 7 passed.

- [ ] **Step 5: Commit**

```bash
git add crates/qdesk-agentd/src/input.rs
git commit -m "feat(agentd): type and key actions via xdotool"
```

---

## Task 7: Scroll & Drag actions

**Files:**
- Modify: `crates/qdesk-agentd/src/input.rs:execute()`

- [ ] **Step 1: Add tests**

Append to the `more_tests` module in `crates/qdesk-agentd/src/input.rs`:

```rust
    #[tokio::test]
    async fn mock_records_scroll_action() {
        let m = MockInput::default();
        let a = Action::Scroll { x: 100, y: 200, dx: 0, dy: -3 };
        m.execute(&a).await.unwrap();
        assert_eq!(m.recorded.lock().await[0], a);
    }

    #[tokio::test]
    async fn mock_records_drag_action() {
        let m = MockInput::default();
        let a = Action::Drag {
            from: qdesk_protocol::Point { x: 10, y: 20 },
            to: qdesk_protocol::Point { x: 30, y: 40 },
        };
        m.execute(&a).await.unwrap();
        assert_eq!(m.recorded.lock().await[0], a);
    }
```

- [ ] **Step 2: Implement scroll & drag in `XdotoolInput::execute`**

Extend the match arms (replace the catch-all `other` arm):

```rust
            Action::Scroll { x, y, dx: _, dy } => {
                // xdotool scrolls via button 4 (up) and 5 (down). One click per "tick".
                let button = if *dy < 0 { "5" } else { "4" };
                let ticks = dy.unsigned_abs() as u32;
                self.run(vec![
                    "mousemove".into(),
                    x.to_string(),
                    y.to_string(),
                ])
                .await?;
                for _ in 0..ticks.max(1) {
                    self.run(vec!["click".into(), button.into()]).await?;
                }
                Ok(())
            }
            Action::Drag { from, to } => {
                self.run(vec![
                    "mousemove".into(),
                    from.x.to_string(),
                    from.y.to_string(),
                    "mousedown".into(),
                    "1".into(),
                    "mousemove".into(),
                    to.x.to_string(),
                    to.y.to_string(),
                    "mouseup".into(),
                    "1".into(),
                ])
                .await
            }
            Action::Wait { ms } => {
                tokio::time::sleep(std::time::Duration::from_millis(*ms)).await;
                Ok(())
            }
```

After this, the `match` should cover every `Action` variant exhaustively — remove the `other =>` catch-all branch.

- [ ] **Step 3: Run all tests**

Run: `cargo test -p qdesk-agentd`
Expected: 9 passed.

- [ ] **Step 4: Confirm no warnings about unhandled match arm**

Run: `cargo build -p qdesk-agentd 2>&1 | grep -i warn` — expect no relevant warnings.

- [ ] **Step 5: Commit**

```bash
git add crates/qdesk-agentd/src/input.rs
git commit -m "feat(agentd): scroll, drag, and wait actions"
```

---

## Task 8: Sandbox Dockerfile

**Files:**
- Create: `images/ubuntu-chrome/Dockerfile`
- Create: `images/ubuntu-chrome/entrypoint.sh`

- [ ] **Step 1: Create `images/ubuntu-chrome/entrypoint.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

DISPLAY_NUM="${DISPLAY_NUM:-99}"
RES="${RES:-1920x1080x24}"

# Start Xvfb.
Xvfb ":${DISPLAY_NUM}" -screen 0 "${RES}" -ac +extension RANDR -nolisten tcp &
XVFB_PID=$!
export DISPLAY=":${DISPLAY_NUM}"

# Wait for X to be ready.
for _ in $(seq 1 30); do
    if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then
        break
    fi
    sleep 0.2
done

# Start a minimal window manager (xfwm4 alone, no full xfce session).
xfwm4 --display "$DISPLAY" --replace &
WM_PID=$!

# Set a neutral root color so screenshots aren't black.
xsetroot -solid '#202020' -display "$DISPLAY"

# Cleanup on exit.
cleanup() {
    kill "$WM_PID" 2>/dev/null || true
    kill "$XVFB_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Hand off to qdesk-agentd in foreground.
exec /usr/local/bin/qdesk-agentd \
    --listen "0.0.0.0:7878" \
    --display ":${DISPLAY_NUM}"
```

Make it executable:
```bash
chmod +x images/ubuntu-chrome/entrypoint.sh
```

- [ ] **Step 2: Create `images/ubuntu-chrome/Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1.7

# ---- Builder stage: compile qdesk-agentd ----
FROM rust:1.80-slim-bookworm AS builder
WORKDIR /src

# Copy workspace files.
COPY Cargo.toml ./
COPY crates ./crates

RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config libssl-dev \
    && rm -rf /var/lib/apt/lists/*

RUN cargo build --release -p qdesk-agentd

# ---- Runtime stage ----
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb xfwm4 xsetroot x11-utils \
        scrot xdotool \
        chromium-browser fonts-noto-core fonts-noto-cjk \
        ca-certificates dbus-x11 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/target/release/qdesk-agentd /usr/local/bin/qdesk-agentd
COPY images/ubuntu-chrome/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 7878
ENV DISPLAY_NUM=99 RES=1920x1080x24

ENTRYPOINT ["/entrypoint.sh"]
```

> Note: Ubuntu 24.04 ships `chromium-browser` as a snap-redirect package. If that fails, swap to `chromium` (different package on Debian) or `google-chrome-stable` from Google's repo. The entrypoint doesn't need Chromium running by default — it's there for tests to launch via the agent.

- [ ] **Step 3: Build the image**

```bash
docker build -t qdesk/ubuntu-chrome:dev -f images/ubuntu-chrome/Dockerfile .
```

Expected: builds successfully. First build ~5–10 min; subsequent builds use cache.

If `chromium-browser` install fails, edit Dockerfile to use `chromium` and rebuild.

- [ ] **Step 4: Commit**

```bash
git add images/ubuntu-chrome
git commit -m "feat(image): ubuntu-chrome sandbox dockerfile and entrypoint"
```

---

## Task 9: Smoke test the running container (end-to-end)

**Files:**
- Create: `scripts/smoke-sandbox.sh`

- [ ] **Step 1: Create `scripts/smoke-sandbox.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-qdesk/ubuntu-chrome:dev}"
NAME="qdesk-smoke-$$"
PORT=$(shuf -i 30000-39999 -n 1)

cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "==> starting container on port $PORT"
docker run -d --name "$NAME" -p "${PORT}:7878" "$IMAGE"

echo "==> waiting for /health"
for i in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
        echo "    ready after ${i} attempts"
        break
    fi
    sleep 0.5
    if [ "$i" = "30" ]; then echo "TIMEOUT"; docker logs "$NAME"; exit 1; fi
done

echo "==> /health"
curl -fsS "http://127.0.0.1:${PORT}/health" | tee /dev/stderr; echo

echo "==> /screenshot to /tmp/qdesk-smoke.png"
curl -fsS "http://127.0.0.1:${PORT}/screenshot" --output /tmp/qdesk-smoke.png
file /tmp/qdesk-smoke.png

echo "==> POST /actions { type: wait, ms: 100 }"
curl -fsS -X POST "http://127.0.0.1:${PORT}/actions" \
    -H 'content-type: application/json' \
    -d '{"type":"wait","ms":100}' | tee /dev/stderr; echo

echo "==> POST /actions { type: click, x: 50, y: 50 }"
curl -fsS -X POST "http://127.0.0.1:${PORT}/actions" \
    -H 'content-type: application/json' \
    -d '{"type":"click","x":50,"y":50}' | tee /dev/stderr; echo

echo "PASS"
```

```bash
chmod +x scripts/smoke-sandbox.sh
```

- [ ] **Step 2: Run the smoke test**

```bash
./scripts/smoke-sandbox.sh
```

Expected output:
- `==> /health` → `{"status":"ok","version":"0.1.0"}`
- `/tmp/qdesk-smoke.png: PNG image data, 1920 x 1080, 8-bit/color RGB`
- Both action POSTs return `{"ok":true,"screen_changed":true}`
- Final line: `PASS`

If the screenshot is corrupt or wrong size, check `docker logs qdesk-smoke-*` for Xvfb / agentd errors.

- [ ] **Step 3: Commit**

```bash
git add scripts/smoke-sandbox.sh
git commit -m "test(smoke): end-to-end sandbox smoke test script"
```

---

## Task 10: README quickstart polish + doc

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README**

Replace the Quickstart section in `README.md`:

```markdown
## Quickstart (Phase 0 sandbox)

Build and run a single sandbox container:

```bash
docker build -t qdesk/ubuntu-chrome:dev -f images/ubuntu-chrome/Dockerfile .
docker run -d --rm --name qdesk-sbx -p 7878:7878 qdesk/ubuntu-chrome:dev

# Health
curl http://localhost:7878/health
# => {"status":"ok","version":"0.1.0"}

# Take a screenshot
curl http://localhost:7878/screenshot --output /tmp/screen.png
file /tmp/screen.png  # PNG image data, 1920 x 1080, 8-bit/color RGB

# Click at (100, 200)
curl -X POST http://localhost:7878/actions \
    -H 'content-type: application/json' \
    -d '{"type":"click","x":100,"y":200}'

# Type text
curl -X POST http://localhost:7878/actions \
    -H 'content-type: application/json' \
    -d '{"type":"type","text":"hello world"}'

# Press Ctrl+S
curl -X POST http://localhost:7878/actions \
    -H 'content-type: application/json' \
    -d '{"type":"key","keys":["ctrl","s"]}'

docker stop qdesk-sbx
```

Or run the smoke test:
```bash
./scripts/smoke-sandbox.sh
```

## Layout

- `crates/qdesk-protocol/` — wire types shared across host & sandbox
- `crates/qdesk-agentd/`   — in-sandbox HTTP daemon (binary)
- `images/ubuntu-chrome/`  — Dockerfile + entrypoint for default sandbox image
- `docs/superpowers/specs/` — design documents
- `docs/superpowers/plans/` — implementation plans
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): phase 0 sandbox quickstart"
```

---

## Verification Checklist (before marking Plan 1 done)

Run all of these in order:

- [ ] `cargo fmt --all -- --check` — code is formatted
- [ ] `cargo clippy --all-targets -- -D warnings` — no warnings
- [ ] `cargo test --workspace` — all unit tests pass (≥9 in agentd, 5 in protocol)
- [ ] `docker build -t qdesk/ubuntu-chrome:dev -f images/ubuntu-chrome/Dockerfile .` — builds clean
- [ ] `./scripts/smoke-sandbox.sh` — prints `PASS`
- [ ] `git status` — working tree clean

If any of these fail, fix it before declaring Plan 1 complete.

---

## What's NOT in Plan 1 (deferred to later plans)

- Control plane (`qdesk-control`) HTTP API — **Plan 2**
- Docker orchestration crate (`qdesk-runtime`) — **Plan 2**
- Multi-session management, port allocation — **Plan 2**
- SQLite session store — **Plan 2**
- LLM agent + Agent Mode test runner — **Plan 3**
- `.qdesk` test file parser — **Plan 3**
- Excalidraw demo — **Plan 3**
- DOM / a11y endpoint — Phase 1 (post-MVP)
- Snapshots / restore — Phase 2

After Plan 1 passes verification, return for `2026-05-03-qdesk-phase-0-plan-2-control.md`.
