use anyhow::Result;
use clap::{Parser, Subcommand};

pub mod create;

#[derive(Parser)]
#[command(author, version, about, long_about = None)]
#[command(propagate_version = true)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Create a new firewood database
    Create(create::Options),
}

fn main() -> Result<()> {
    let cli = Cli::parse();

    match &cli.command {
        Commands::Create(opts) => create::run(opts),
        _ => unreachable!("Subcommand not found. Exhausted list of subcommands"),
    }
}
