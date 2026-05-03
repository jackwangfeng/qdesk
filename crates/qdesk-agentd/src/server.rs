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

    #[tokio::test]
    async fn health_returns_ok() {
        let (s, _) = state();
        let app = router(s);
        let resp = app
            .oneshot(Request::builder().uri("/health").body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
    }

    #[tokio::test]
    async fn screenshot_returns_png_bytes() {
        let (s, _) = state();
        let app = router(s);
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
