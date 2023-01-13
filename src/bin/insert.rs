use anyhow::{anyhow, Result};
use clap::Args;
use firewood::db::{DBConfig, WALConfig, DB};
use log;

#[derive(Debug, Args)]
pub struct Options {
    /// The key to insert
    #[arg(long, short = 'k', required = true, value_name = "KEY", help = "Key to insert")]
    pub key: String,

    /// The value to insert
    #[arg(long, short = 'v', required = true, value_name = "VALUE", help = "Value to insert")]
    pub value: String,

    /// The database path (if no path is provided, return an error). Defaults to firewood.
    #[arg(
        required = false,
        value_name = "DB_NAME",
        default_value_t = String::from("firewood"),
        help = "Name of the database"
    )]
    pub db_path: String,
}

pub fn run(opts: &Options) -> Result<()> {
    log::debug!("inserting key value pair {:?}", opts);
    let cfg = DBConfig::builder()
        .truncate(false)
        .wal(WALConfig::builder().max_revisions(10).build());

    let db = match DB::new(opts.db_path.as_str(), &cfg.build()) {
        Ok(db) => db,
        Err(_) => return Err(anyhow!("error opening database")),
    };

    let x = match db
        .new_writebatch()
        .kv_insert(opts.key.clone(), opts.value.bytes().collect())
    {
        Ok(insertion) => {
            insertion.commit();
            println!("{}", opts.key);

            Ok(())
        }
        Err(_) => return Err(anyhow!("error inserting key/value pair into the database")),
    };
    x
}
