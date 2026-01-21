//! Avalanche Node binary.
//!
//! This is the main entry point for running an Avalanche node.

use std::path::PathBuf;
use std::sync::Arc;

use clap::{Parser, Subcommand};
use tracing::{error, info, Level};
use tracing_subscriber::FmtSubscriber;

mod config;
mod node;

use config::Config;
use node::Node;

/// Avalanche Node CLI
#[derive(Parser)]
#[command(name = "avalanche-node")]
#[command(author = "Avalanche")]
#[command(version = "0.1.0")]
#[command(about = "Avalanche blockchain node", long_about = None)]
struct Cli {
    /// Configuration file path
    #[arg(short, long, default_value = "config.toml")]
    config: PathBuf,

    /// Data directory
    #[arg(short, long)]
    data_dir: Option<PathBuf>,

    /// Log level (trace, debug, info, warn, error)
    #[arg(short, long, default_value = "info")]
    log_level: String,

    #[command(subcommand)]
    command: Option<Commands>,
}

#[derive(Subcommand)]
enum Commands {
    /// Start the node
    Start,
    /// Show node version
    Version,
    /// Show node status
    Status,
    /// Initialize a new node
    Init {
        /// Network ID
        #[arg(short, long, default_value = "1")]
        network_id: u32,
    },
    /// Validate configuration
    Validate,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let cli = Cli::parse();

    // Setup logging
    let log_level = match cli.log_level.to_lowercase().as_str() {
        "trace" => Level::TRACE,
        "debug" => Level::DEBUG,
        "info" => Level::INFO,
        "warn" => Level::WARN,
        "error" => Level::ERROR,
        _ => Level::INFO,
    };

    let subscriber = FmtSubscriber::builder()
        .with_max_level(log_level)
        .with_target(true)
        .with_thread_ids(true)
        .with_file(true)
        .with_line_number(true)
        .finish();

    tracing::subscriber::set_global_default(subscriber)
        .expect("setting default subscriber failed");

    match &cli.command {
        Some(Commands::Version) => {
            println!("avalanche-node version 0.1.0");
            println!("Built with Rust");
        }
        Some(Commands::Init { network_id }) => {
            info!("Initializing node for network {}", network_id);
            let config = Config::default_for_network(*network_id);
            let config_path = &cli.config;

            let toml = toml::to_string_pretty(&config)?;
            std::fs::write(config_path, toml)?;
            info!("Configuration written to {:?}", config_path);
        }
        Some(Commands::Validate) => {
            info!("Validating configuration at {:?}", cli.config);
            match Config::load(&cli.config) {
                Ok(config) => {
                    if let Err(e) = config.validate() {
                        error!("Configuration invalid: {}", e);
                        std::process::exit(1);
                    }
                    info!("Configuration is valid");
                    info!("Network ID: {}", config.network.network_id);
                    info!("HTTP Port: {}", config.api.http_port);
                    info!("Staking Port: {}", config.network.staking_port);
                }
                Err(e) => {
                    error!("Failed to load configuration: {}", e);
                    std::process::exit(1);
                }
            }
        }
        Some(Commands::Status) => {
            // TODO: Connect to running node and get status
            println!("Node status: Not implemented yet");
        }
        Some(Commands::Start) | None => {
            info!("Starting Avalanche node...");

            // Load configuration
            let config = if cli.config.exists() {
                Config::load(&cli.config)?
            } else {
                info!("No config file found, using defaults");
                Config::default()
            };

            // Override data directory if specified
            let config = if let Some(data_dir) = cli.data_dir {
                Config {
                    data_dir,
                    ..config
                }
            } else {
                config
            };

            // Validate configuration
            config.validate()?;

            info!("Network ID: {}", config.network.network_id);
            info!("Data directory: {:?}", config.data_dir);

            // Create and start node
            let node = Node::new(config).await?;

            info!("Node initialized, starting services...");

            // Run node
            if let Err(e) = node.run().await {
                error!("Node error: {}", e);
                std::process::exit(1);
            }
        }
    }

    Ok(())
}
