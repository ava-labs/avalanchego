use std::error::Error;

use firewood::{
    db::{BatchOp, Db},
    v2::api::{Db as _, Proposal as _},
};

use crate::{Args, TestRunner};

#[derive(Clone)]
pub struct TenKRandom;

impl TestRunner for TenKRandom {
    async fn run(&self, db: &Db, args: &Args) -> Result<(), Box<dyn Error>> {
        let mut low = 0;
        let mut high = args.number_of_batches * args.batch_size;
        let twenty_five_pct = args.batch_size / 4;

        loop {
            let batch: Vec<BatchOp<_, _>> = Self::generate_inserts(high, twenty_five_pct)
                .chain(Self::generate_deletes(low, twenty_five_pct))
                .chain(Self::generate_updates(
                    low + high / 2,
                    twenty_five_pct * 2,
                    low,
                ))
                .collect();
            let proposal = db.propose(batch).await.expect("proposal should succeed");
            proposal.commit().await?;
            low += twenty_five_pct;
            high += twenty_five_pct;
        }
    }
}
