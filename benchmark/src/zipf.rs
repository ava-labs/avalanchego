use crate::{Args, TestRunner};
use firewood::db::{BatchOp, Db};
use firewood::logger::debug;
use firewood::v2::api::{Db as _, Proposal as _};
use rand::prelude::Distribution as _;
use rand::thread_rng;
use sha2::{Digest, Sha256};
use std::error::Error;

#[derive(Clone)]
pub struct Zipf;

impl TestRunner for Zipf {
    async fn run(&self, db: &Db, args: &Args) -> Result<(), Box<dyn Error>> {
        let rows = (args.number_of_batches * args.batch_size) as usize;
        let zipf = zipf::ZipfDistribution::new(rows, 1.0).unwrap();

        for batch_id in 0.. {
            let batch: Vec<BatchOp<_, _>> =
                generate_updates(batch_id, args.batch_size as usize, &zipf).collect();
            let proposal = db.propose(batch).await.expect("proposal should succeed");
            proposal.commit().await?;
        }
        unreachable!()
    }
}
fn generate_updates(
    batch_id: u32,
    batch_size: usize,
    zipf: &zipf::ZipfDistribution,
) -> impl Iterator<Item = BatchOp<Vec<u8>, Vec<u8>>> {
    let hash_of_batch_id = Sha256::digest(batch_id.to_ne_bytes()).to_vec();
    let rng = thread_rng();
    zipf.sample_iter(rng)
        .take(batch_size)
        .map(|inner_key| {
            let digest = Sha256::digest(inner_key.to_ne_bytes()).to_vec();
            debug!(
                "updating {:?} with digest {} to {}",
                inner_key,
                hex::encode(&digest),
                hex::encode(&hash_of_batch_id)
            );
            (digest, hash_of_batch_id.clone())
        })
        .map(|(key, value)| BatchOp::Put { key, value })
        .collect::<Vec<_>>()
        .into_iter()
}
