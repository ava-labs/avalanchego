use anyhow::{anyhow, Result};
use clap::Args;
use firewood::db::{DBConfig, WALConfig, DB};
use log;

#[derive(Debug, Args)]
pub struct Options {
    /// The key to get the value for
    #[arg(long, required = true, value_name = "KEY", help = "Key to get")]
    pub key: String,

    /// The database path (if no path is provided, return an error). Defaults to firewood.
    #[arg(
        required = true,
        value_name = "DB_NAME",
        default_value_t = String::from("firewood"),
        help = "Name of the database"
    )]
    pub db_path: String,
}

pub fn run(opts: &Options) -> Result<()> {
    log::debug!("get key value pair {:?}", opts);
    let cfg = DBConfig::builder()
        .truncate(false)
        .wal(WALConfig::builder().max_revisions(10).build());

    let db = match DB::new(opts.db_path.as_str(), &cfg.build()) {
        Ok(db) => db,
        Err(_) => return Err(anyhow!("error opening database")),
    };

    match db.get_code(opts.key.as_bytes()) {
        Ok(val) => log::info!("{:#?}", val),
        Err(_) => return Err(anyhow!("error getting key")),
    }

    Ok(())
}
