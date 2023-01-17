use anyhow::{anyhow, Result};
use clap::Args;
use firewood::db::{DBConfig, WALConfig, DB};
use log;

#[derive(Debug, Args)]
pub struct Options {
    /// The key to delete
    #[arg(long, required = true, value_name = "KEY", help = "Key to delete")]
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
    log::debug!("deleting key {:?}", opts);
    let cfg = DBConfig::builder()
        .truncate(false)
        .wal(WALConfig::builder().max_revisions(10).build());

    let db = match DB::new(opts.db.as_str(), &cfg.build()) {
        Ok(db) => db,
        Err(_) => return Err(anyhow!("error opening database")),
    };

    let mut account: Option<Vec<u8>> = None;
    let x = match db.new_writebatch().kv_remove(opts.key.clone(), &mut account) {
        Ok(wb) => {
            wb.commit();
            println!("{}", opts.key);

            Ok(())
        }
        Err(_) => Err(anyhow!("error deleting key")),
    };
    x
}
