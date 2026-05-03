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
