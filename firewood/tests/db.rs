use firewood::{
    db::{BatchOp, Db as PersistedDb, DbConfig, DbError, WalConfig},
    merkle::TrieHash,
};

use std::{
    collections::VecDeque,
    fs::remove_dir_all,
    ops::{Deref, DerefMut},
    path::Path,
    sync::Arc,
};

// TODO: use a trait
macro_rules! kv_dump {
    ($e: ident) => {{
        let mut s = Vec::new();
        $e.kv_root_hash().unwrap();
        $e.kv_dump(&mut s).unwrap();
        String::from_utf8(s).unwrap()
    }};
}

struct Db<'a, P: AsRef<Path> + ?Sized>(PersistedDb, &'a P);

impl<'a, P: AsRef<Path> + ?Sized> Db<'a, P> {
    fn new(path: &'a P, cfg: &DbConfig) -> Result<Self, DbError> {
        PersistedDb::new(path, cfg).map(|db| Self(db, path))
    }
}

impl<P: AsRef<Path> + ?Sized> Drop for Db<'_, P> {
    fn drop(&mut self) {
        // if you're using absolute paths, you have to clean up after yourself
        if self.1.as_ref().is_relative() {
            remove_dir_all(self.1).expect("should be able to remove db-directory");
        }
    }
}

#[test]
fn test_basic_metrics() {
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
    let db = Db::new("test_revisions_db2", &cfg.truncate(true).build()).unwrap();
    let metrics = db.metrics();
    assert_eq!(metrics.kv_get.hit_count.get(), 0);
    db.kv_get("a").ok();
    assert_eq!(metrics.kv_get.hit_count.get(), 1);
}

#[test]
fn test_revisions() {
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
        );

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
        let db =
            PersistedDb::new("test_revisions_db", &cfg.clone().truncate(true).build()).unwrap();
        let mut dumped = VecDeque::new();
        let mut hashes: VecDeque<TrieHash> = VecDeque::new();
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
                let proposal = db.new_proposal(batch).unwrap();
                proposal.commit().unwrap();
            }
            while dumped.len() > 10 {
                dumped.pop_back();
                hashes.pop_back();
            }
            let root_hash = db.kv_root_hash().unwrap();
            hashes.push_front(root_hash);
            dumped.push_front(kv_dump!(db));
            dumped
                .iter()
                .zip(hashes.iter().cloned())
                .map(|(data, hash)| (data, db.get_revision(&hash).unwrap()))
                .map(|(data, rev)| (data, kv_dump!(rev)))
                .for_each(|(b, a)| {
                    if &a != b {
                        print!("{a}\n{b}");
                        panic!("not the same");
                    }
                });
        }
        drop(db);
        let db = Db::new("test_revisions_db", &cfg.clone().truncate(false).build()).unwrap();
        dumped
            .iter()
            .zip(hashes.iter().cloned())
            .map(|(data, hash)| (data, db.get_revision(&hash).unwrap()))
            .map(|(data, rev)| (data, kv_dump!(rev)))
            .for_each(|(previous_dump, after_reopen_dump)| {
                if &after_reopen_dump != previous_dump {
                    panic!(
                        "not the same: pass {i}:\n{after_reopen_dump}\n--------\n{previous_dump}"
                    );
                }
            });
        println!("i = {i}");
    }
}

#[test]
fn create_db_issue_proof() {
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

    let db = Db::new("test_db_proof", &cfg.truncate(true).build()).unwrap();

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
    let proposal = db.new_proposal(batch).unwrap();
    proposal.commit().unwrap();

    let root_hash = db.kv_root_hash().unwrap();

    // Add second commit
    let mut batch = Vec::new();
    for (k, v) in Vec::from([("x", "two")]).iter() {
        let write = BatchOp::Put {
            key: k.to_string().as_bytes().to_vec(),
            value: v.as_bytes().to_vec(),
        };
        batch.push(write);
    }
    let proposal = db.new_proposal(batch).unwrap();
    proposal.commit().unwrap();

    let rev = db.get_revision(&root_hash).unwrap();
    let key = "doe".as_bytes();
    let root_hash = rev.kv_root_hash();

    match rev.prove::<&[u8]>(key) {
        Ok(proof) => {
            let verification = proof.verify_proof(key, *root_hash.unwrap()).unwrap();
            assert!(verification.is_some());
        }
        Err(e) => {
            panic!("Error: {}", e);
        }
    }

    let missing_key = "dog".as_bytes();
    // The proof for the missing key will return the path to the missing key
    if let Err(e) = rev.prove(missing_key) {
        println!("Error: {}", e);
        // TODO do type assertion on error
    }
}

impl<P: AsRef<Path> + ?Sized> Deref for Db<'_, P> {
    type Target = PersistedDb;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl<P: AsRef<Path> + ?Sized> DerefMut for Db<'_, P> {
    fn deref_mut(&mut self) -> &mut PersistedDb {
        &mut self.0
    }
}

macro_rules! assert_val {
    ($rev: ident, $key:literal, $expected_val:literal) => {
        let actual = $rev.kv_get($key.as_bytes()).unwrap();
        assert_eq!(actual, $expected_val.as_bytes().to_vec());
    };
}

#[test]
fn db_proposal() -> Result<(), DbError> {
    let cfg = DbConfig::builder().wal(WalConfig::builder().max_revisions(10).build());

    let db = Db::new("test_db_proposal", &cfg.clone().truncate(true).build())
        .expect("db initiation should succeed");

    let batch = vec![
        BatchOp::Put {
            key: b"k",
            value: "v".as_bytes().to_vec(),
        },
        BatchOp::Delete { key: b"z" },
    ];

    let proposal = Arc::new(db.new_proposal(batch)?);
    let rev = proposal.get_revision();
    assert_val!(rev, "k", "v");

    let batch_2 = vec![BatchOp::Put {
        key: b"k2",
        value: "v2".as_bytes().to_vec(),
    }];
    let proposal_2 = proposal.clone().propose(batch_2)?;
    let rev = proposal_2.get_revision();
    assert_val!(rev, "k", "v");
    assert_val!(rev, "k2", "v2");

    proposal.commit()?;
    proposal_2.commit()?;

    std::thread::scope(|scope| {
        scope.spawn(|| -> Result<(), DbError> {
            let another_batch = vec![BatchOp::Put {
                key: b"another_k",
                value: "another_v".as_bytes().to_vec(),
            }];
            let another_proposal = proposal.clone().propose(another_batch)?;
            let rev = another_proposal.get_revision();
            assert_val!(rev, "k", "v");
            assert_val!(rev, "another_k", "another_v");
            // The proposal is invalid and cannot be committed
            assert!(another_proposal.commit().is_err());
            Ok(())
        });

        scope.spawn(|| -> Result<(), DbError> {
            let another_batch = vec![BatchOp::Put {
                key: b"another_k_1",
                value: "another_v_1".as_bytes().to_vec(),
            }];
            let another_proposal = proposal.clone().propose(another_batch)?;
            let rev = another_proposal.get_revision();
            assert_val!(rev, "k", "v");
            assert_val!(rev, "another_k_1", "another_v_1");
            Ok(())
        });
    });

    // Recusrive commit

    let batch = vec![BatchOp::Put {
        key: b"k3",
        value: "v3".as_bytes().to_vec(),
    }];
    let proposal = Arc::new(db.new_proposal(batch)?);
    let rev = proposal.get_revision();
    assert_val!(rev, "k", "v");
    assert_val!(rev, "k2", "v2");
    assert_val!(rev, "k3", "v3");

    let batch_2 = vec![BatchOp::Put {
        key: b"k4",
        value: "v4".as_bytes().to_vec(),
    }];
    let proposal_2 = proposal.clone().propose(batch_2)?;
    let rev = proposal_2.get_revision();
    assert_val!(rev, "k", "v");
    assert_val!(rev, "k2", "v2");
    assert_val!(rev, "k3", "v3");
    assert_val!(rev, "k4", "v4");

    proposal_2.commit()?;
    Ok(())
}
