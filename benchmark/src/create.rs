use std::error::Error;
use std::time::Instant;

use firewood::db::Db;
use firewood::v2::api::{Db as _, Proposal as _};

use pretty_duration::pretty_duration;

use crate::{Args, TestRunner};

#[derive(Clone)]
pub struct Create;

impl TestRunner for Create {
    async fn run(&self, db: &Db, args: &Args) -> Result<(), Box<dyn Error>> {
        let keys = args.batch_size;
        let start = Instant::now();

        for key in 0..args.number_of_batches {
            let batch = Self::generate_inserts(key * keys, args.batch_size).collect();

            let proposal = db.propose(batch).await.expect("proposal should succeed");
            proposal.commit().await?;
        }
        let duration = start.elapsed();
        println!(
            "Generated and inserted {} batches of size {keys} in {}",
            args.number_of_batches,
            pretty_duration(&duration, None)
        );

        Ok(())
    }
}
