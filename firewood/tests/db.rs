// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::{
    db::{DbConfig, WalConfig},
    v2::api::{self, BatchOp, Db as _, DbView, Proposal},
};
use tokio::task::block_in_place;

use std::{collections::VecDeque, env::temp_dir, path::PathBuf, sync::Arc};

mod common;
use common::TestDbCreator;

// TODO: use a trait
macro_rules! kv_dump {
    ($e: ident) => {{
        let mut s = Vec::new();
        $e.kv_dump(&mut s).unwrap();
        String::from_utf8(s).unwrap()
    }};
}

#[tokio::test(flavor = "multi_thread")]
async fn test_basic_metrics() {
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
        );

    let db = TestDbCreator::builder()
        .cfg(cfg.build())
        .test_name("test_basic_metrics")
        .build()
        .create()
        .await;

    // let metrics = db.metrics();
    // TODO: kv_get is no longer a valid metric, and DbRev has no access to Db.metrics (yet)
    //assert_eq!(metrics.kv_get.hit_count.get(), 0);

    // TODO: we can't fetch the revision for the empty tree, so insert a single value
    Arc::new(
        db.propose(vec![BatchOp::Put {
            key: b"a",
            value: b"b",
        }])
        .await
        .unwrap(),
    )
    .commit()
    .await
    .unwrap();

    let root = db.root_hash().await.unwrap();
    let rev = db.revision(root).await.unwrap();
    rev.val("a").await.ok().unwrap().unwrap();
    //assert_eq!(metrics.val.hit_count.get(), 1);
}

#[tokio::test(flavor = "multi_thread")]
async fn test_revisions() {
    use rand::{rngs::StdRng, Rng, SeedableRng};
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
        .build();

    let rng = std::cell::RefCell::new(StdRng::seed_from_u64(42));
    let max_len0 = 8;
    let max_len1 = 4;
    let keygen = || {
        let (len0, len1): (usize, usize) = {
            let mut rng = rng.borrow_mut();
            (
                rng.gen_range(1..max_len0 + 1),
                rng.gen_range(1..max_len1 + 1),
            )
        };
        let key: Vec<u8> = (0..len0)
            .map(|_| rng.borrow_mut().gen_range(0..2))
            .chain((0..len1).map(|_| rng.borrow_mut().gen()))
            .collect();
        key
    };

    for i in 0..10 {
        let db = TestDbCreator::builder()
            .cfg(cfg.clone())
            .test_name("test_revisions")
            .build()
            .create()
            .await;
        let mut dumped = VecDeque::new();
        let mut hashes: VecDeque<api::HashKey> = VecDeque::new();
        for _ in 0..10 {
            {
                let mut batch = Vec::new();
                let m = rng.borrow_mut().gen_range(1..20);
                for _ in 0..m {
                    let key = keygen();
                    let val: Vec<u8> = (0..8).map(|_| rng.borrow_mut().gen()).collect();
                    let write = BatchOp::Put {
                        key,
                        value: val.to_vec(),
                    };
                    batch.push(write);
                }
                let proposal = Arc::new(db.propose(batch).await.unwrap());
                proposal.commit().await.unwrap();
            }
            while dumped.len() > 10 {
                dumped.pop_back();
                hashes.pop_back();
            }
            let root_hash = db.root_hash().await.unwrap();
            hashes.push_front(root_hash);
            dumped.push_front(kv_dump!(db));
            for (dump, hash) in dumped.iter().zip(hashes.iter().cloned()) {
                let rev = db.revision(hash).await.unwrap();
                assert_eq!(rev.root_hash().await.unwrap(), hash);
                assert_eq!(kv_dump!(rev), *dump, "not the same: Pass {i}");
            }
        }

        let db = db.reopen().await;
        for (dump, hash) in dumped.iter().zip(hashes.iter().cloned()) {
            let rev = db.revision(hash).await.unwrap();
            rev.root_hash().await.unwrap();
            assert_eq!(
                *dump,
                block_in_place(|| kv_dump!(rev)),
                "not the same: pass {i}"
            );
        }
    }
}

#[tokio::test(flavor = "multi_thread")]
async fn create_db_issue_proof() {
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
        );

    let mut tmpdir: PathBuf = std::env::var_os("CARGO_TARGET_DIR")
        .unwrap_or(temp_dir().into())
        .into();
    tmpdir.push("/tmp/test_db_proof");

    let db = firewood::db::Db::new(tmpdir, &cfg.truncate(true).build())
        .await
        .unwrap();

    let items = vec![
        ("d", "verb"),
        ("do", "verb"),
        ("doe", "reindeer"),
        ("e", "coin"),
    ];

    let mut batch = Vec::new();
    for (k, v) in items {
        let write = BatchOp::Put {
            key: k.as_bytes(),
            value: v.as_bytes().to_vec(),
        };
        batch.push(write);
    }
    let proposal = Arc::new(db.propose(batch).await.unwrap());
    proposal.commit().await.unwrap();

    let root_hash = db.root_hash().await.unwrap();

    // Add second commit
    let mut batch = Vec::new();
    for (k, v) in Vec::from([("x", "two")]).iter() {
        let write = BatchOp::Put {
            key: k.to_string().as_bytes().to_vec(),
            value: v.as_bytes().to_vec(),
        };
        batch.push(write);
    }
    let proposal = Arc::new(db.propose(batch).await.unwrap());
    proposal.commit().await.unwrap();

    let rev = db.revision(root_hash).await.unwrap();
    let key = "doe".as_bytes();
    let root_hash = rev.root_hash().await.unwrap();

    match rev.single_key_proof(key).await {
        Ok(proof) => {
            let verification = proof.unwrap().verify(key, root_hash).unwrap();
            assert!(verification.is_some());
        }
        Err(e) => {
            panic!("Error: {}", e);
        }
    }

    let missing_key = "dog".as_bytes();
    // The proof for the missing key will return the path to the missing key
    if let Err(e) = rev.single_key_proof(missing_key).await {
        println!("Error: {}", e);
        // TODO do type assertion on error
    }
}

macro_rules! assert_val {
    ($rev: ident, $key:literal, $expected_val:literal) => {
        let actual = $rev.val($key.as_bytes()).await.unwrap().unwrap();
        assert_eq!(actual, $expected_val.as_bytes().to_vec());
    };
}

#[ignore = "ref: https://github.com/ava-labs/firewood/issues/457"]
#[tokio::test(flavor = "multi_thread")]
async fn db_proposal() -> Result<(), api::Error> {
    let cfg = DbConfig::builder()
        .wal(WalConfig::builder().max_revisions(10).build())
        .build();

    let db = TestDbCreator::builder()
        .cfg(cfg)
        .test_name("db_proposal")
        .build()
        .create()
        .await;

    let batch = vec![
        BatchOp::Put {
            key: b"k",
            value: "v".as_bytes().to_vec(),
        },
        BatchOp::Delete { key: b"z" },
    ];

    let proposal = Arc::new(db.propose(batch).await?);
    assert_val!(proposal, "k", "v");

    let batch_2 = vec![BatchOp::Put {
        key: b"k2",
        value: "v2".as_bytes().to_vec(),
    }];
    let proposal_2 = Arc::new(proposal.clone().propose(batch_2).await?);
    assert_val!(proposal_2, "k", "v");
    assert_val!(proposal_2, "k2", "v2");

    proposal.clone().commit().await?;
    proposal_2.commit().await?;

    let t1 = tokio::spawn({
        let proposal = proposal.clone();
        async move {
            let another_batch = vec![BatchOp::Put {
                key: b"another_k_t1",
                value: "another_v_t1".as_bytes().to_vec(),
            }];
            let another_proposal = proposal.clone().propose(another_batch).await.unwrap();
            let rev = another_proposal.get_revision();
            assert_val!(rev, "k", "v");
            assert_val!(rev, "another_k_t1", "another_v_t1");
            // The proposal is invalid and cannot be committed
            assert!(Arc::new(another_proposal).commit().await.is_err());
        }
    });
    let t2 = tokio::spawn({
        let proposal = proposal.clone();
        async move {
            let another_batch = vec![BatchOp::Put {
                key: b"another_k_t2",
                value: "another_v_t2".as_bytes().to_vec(),
            }];
            let another_proposal = proposal.clone().propose(another_batch).await.unwrap();
            let rev = another_proposal.get_revision();
            assert_val!(rev, "k", "v");
            assert_val!(rev, "another_k_t2", "another_v_t2");
            assert!(Arc::new(another_proposal).commit().await.is_err());
        }
    });
    let (first, second) = tokio::join!(t1, t2);
    first.unwrap();
    second.unwrap();

    // Recursive commit

    let batch = vec![BatchOp::Put {
        key: b"k3",
        value: "v3".as_bytes().to_vec(),
    }];
    let proposal = Arc::new(db.propose(batch).await?);
    assert_val!(proposal, "k", "v");
    assert_val!(proposal, "k2", "v2");
    assert_val!(proposal, "k3", "v3");

    let batch_2 = vec![BatchOp::Put {
        key: b"k4",
        value: "v4".as_bytes().to_vec(),
    }];
    let proposal_2 = Arc::new(proposal.clone().propose(batch_2).await?);
    assert_val!(proposal_2, "k", "v");
    assert_val!(proposal_2, "k2", "v2");
    assert_val!(proposal_2, "k3", "v3");
    assert_val!(proposal_2, "k4", "v4");

    proposal_2.commit().await?;
    Ok(())
}
