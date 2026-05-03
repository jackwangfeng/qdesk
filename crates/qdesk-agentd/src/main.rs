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
