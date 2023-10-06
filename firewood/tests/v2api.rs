// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{error::Error, path::PathBuf, sync::Arc};

use firewood::{
    db::{BatchOp, Db as PersistedDb, DbConfig, DbError, WalConfig},
    v2::api::{Db, DbView, Proposal},
};

#[tokio::test(flavor = "multi_thread")]
async fn smoke() -> Result<(), Box<dyn Error>> {
    let cfg = DbConfig::builder()
        .meta_ncached_pages(1024)
        .meta_ncached_files(128)
        .payload_ncached_pages(1024)
        .payload_ncached_files(128)
        .payload_file_nbit(16)
        .payload_regn_nbit(16)
        .wal(
            WalConfig::builder()
                .file_nbit(15)
                .block_nbit(8)
                .max_revisions(10)
                .build(),
        )
        .truncate(true)
        .build();
    let db = Arc::new(testdb(cfg).await?);
    let empty_hash = db.root_hash().await?;
    assert_ne!(empty_hash, [0; 32]);

    // insert a single key/value
    let (key, value) = (b"smoke", b"test");
    let batch_put = BatchOp::Put { key, value };
    let proposal: Arc<firewood::db::Proposal> = db.propose(vec![batch_put]).await?.into();
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

async fn testdb(cfg: DbConfig) -> Result<PersistedDb, DbError> {
    let tmpdbpath = tmp_dir().join("testdb");
    tokio::task::spawn_blocking(move || PersistedDb::new(tmpdbpath, &cfg))
        .await
        .unwrap()
}

fn tmp_dir() -> PathBuf {
    option_env!("CARGO_TARGET_TMPDIR")
        .map(PathBuf::from)
        .unwrap_or(std::env::temp_dir())
}
