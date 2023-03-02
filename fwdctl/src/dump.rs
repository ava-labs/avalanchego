use anyhow::{anyhow, Result};
use clap::Args;
use firewood::db::{DBConfig, WALConfig, DB};
use log;

#[derive(Debug, Args)]
pub struct Options {
    /// The database path (if no path is provided, return an error). Defaults to firewood.
    #[arg(
        required = true,
        value_name = "DB_NAME",
        default_value_t = String::from("firewood"),
        help = "Name of the database"
    )]
    pub db: String,
}

pub fn run(opts: &Options) -> Result<()> {
    log::debug!("dump database {:?}", opts);
    let cfg = DBConfig::builder()
        .truncate(false)
        .wal(WALConfig::builder().max_revisions(10).build());

    let db = match DB::new(opts.db.as_str(), &cfg.build()) {
        Ok(db) => db,
        Err(_) => return Err(anyhow!("db not available")),
    };

    let mut stdout = std::io::stdout();
    if let Err(_) = db.kv_dump(&mut stdout) {
        return Err(anyhow!("database dump not successful"));
    }
    Ok(())
}
