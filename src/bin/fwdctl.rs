use anyhow::Result;
use clap::{Parser, Subcommand};

pub mod create;
pub mod insert;

#[derive(Parser)]
#[command(author, version, about, long_about = None)]
#[command(propagate_version = true)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
    #[arg(
        long,
        short = 'l',
        required = false,
        help = "Log level. Respects RUST_LOG.",
        value_name = "LOG_LEVEL",
        num_args = 1,
        value_parser = ["debug", "info"],
        default_value_t = String::from("info"),
    )]
    log_level: String,
}

#[derive(Subcommand)]
enum Commands {
    /// Create a new firewood database
    Create(create::Options),
    /// Insert a key/value pair into the database
    Insert(insert::Options),
}

fn main() -> Result<()> {
    let cli = Cli::parse();

    env_logger::init_from_env(
        env_logger::Env::default().filter_or(env_logger::DEFAULT_FILTER_ENV, cli.log_level.to_string()),
    );

    match &cli.command {
        Commands::Create(opts) => create::run(opts),
        Commands::Insert(opts) => insert::run(opts),
    }
}
