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
