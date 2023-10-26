// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{path::PathBuf, sync::Arc};

use firewood::{
    db::{BatchOp, Db as PersistedDb, DbConfig},
    v2::api::{Db, DbView, Proposal},
};

#[tokio::test(flavor = "multi_thread")]
async fn smoke() -> Result<(), Box<dyn std::error::Error>> {
    let cfg = DbConfig::builder().truncate(true).build();
    let db = Arc::new(testdb(cfg).await?);
    let empty_hash = db.root_hash().await?;
    assert_ne!(empty_hash, [0; 32]);

    // insert a single key/value
    let (key, value) = (b"smoke", b"test");
    let batch_put = BatchOp::Put { key, value };
    let proposal = Arc::new(db.propose(vec![batch_put]).await?);
    proposal.commit().await?;

    // ensure the latest hash is different
    let latest = db.root_hash().await?;
    assert_ne!(empty_hash, latest);

    // fetch the view of the latest
    let view = db.revision(latest).await.unwrap();

    // check that the key/value is there
    let got_value = view.val(key).await.unwrap().unwrap();
    assert_eq!(got_value, value);

    // TODO: also fetch view of empty; this currently does not work, as you can't reference
    // the empty hash
    // let empty_view = db.revision(empty_hash).await.unwrap();
    // let value = empty_view.val(b"smoke").await.unwrap();
    // assert_eq!(value, None);

    Ok(())
}

async fn testdb(cfg: DbConfig) -> Result<impl firewood::v2::api::Db, firewood::v2::api::Error> {
    let tmpdbpath = tmp_dir().join("testdb");
    PersistedDb::new(tmpdbpath, &cfg).await
}

fn tmp_dir() -> PathBuf {
    option_env!("CARGO_TARGET_TMPDIR")
        .map(PathBuf::from)
        .unwrap_or(std::env::temp_dir())
}
