use anyhow::{anyhow, Result};
use clap::Args;
use firewood::db::{DBConfig, WALConfig, DB};
use log;
use std::str;

#[derive(Debug, Args)]
pub struct Options {
    /// The key to get the value for
    #[arg(long, short = 'k', required = true, value_name = "KEY", help = "Key to get")]
    pub key: String,

    /// The database path (if no path is provided, return an error). Defaults to firewood.
    #[arg(
        long,
        required = false,
        value_name = "DB_NAME",
        default_value_t = String::from("firewood"),
        help = "Name of the database"
    )]
    pub db: String,
}

pub fn run(opts: &Options) -> Result<()> {
    log::debug!("get key value pair {:?}", opts);
    let cfg = DBConfig::builder()
        .truncate(false)
        .wal(WALConfig::builder().max_revisions(10).build());

    let db = match DB::new(opts.db.as_str(), &cfg.build()) {
        Ok(db) => db,
        Err(_) => return Err(anyhow!("db not available")),
    };

    match db.kv_get(opts.key.as_bytes()) {
        Ok(val) => {
            let s = match str::from_utf8(&val) {
                Ok(v) => v,
                Err(e) => return Err(anyhow!("Invalid UTF-8 sequence: {}", e)),
            };
            println!("{:?}", s);
            if val.is_empty() {
                return Err(anyhow!("no value found for key"))
            }
            return Ok(())
        }
        Err(_) => return Err(anyhow!("key not found")),
    }
}
