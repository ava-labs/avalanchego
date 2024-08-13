// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;

#[derive(Debug, Args)]
pub struct Options {
    /// The key to insert
    #[arg(required = true, value_name = "KEY", help = "Key to insert")]
    pub key: String,

    /// The value to insert
    #[arg(required = true, value_name = "VALUE", help = "Value to insert")]
    pub value: String,

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

// pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
//     log::debug!("inserting key value pair {:?}", opts);
//     let cfg = DbConfig::builder().truncate(false);

//     let db = Db::new(opts.db.clone(), cfg.build()).await?;

//     let batch: Vec<BatchOp<Vec<u8>, Vec<u8>>> = vec![BatchOp::Put {
//         key: opts.key.clone().into(),
//         value: opts.value.bytes().collect(),
//     }];
//     let proposal = db.propose(batch).await?;
//     proposal.commit().await?;

//     println!("{}", opts.key);
//     Ok(())
// }
