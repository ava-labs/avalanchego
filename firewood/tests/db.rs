use firewood::{
    db::{Db as PersistedDb, DbConfig, DbError, WalConfig},
    merkle::{Node, TrieHash},
    storage::StoreRevMut,
};
use firewood_shale::compact::CompactSpace;
use std::{
    collections::VecDeque,
    fs::remove_dir_all,
    ops::{Deref, DerefMut},
    path::Path,
};

// TODO: use a trait
macro_rules! kv_dump {
    ($e: ident) => {{
        let mut s = Vec::new();
        $e.kv_dump(&mut s).unwrap();
        String::from_utf8(s).unwrap()
    }};
}

type Store = CompactSpace<Node, StoreRevMut>;

struct Db<'a, P: AsRef<Path> + ?Sized>(PersistedDb<Store>, &'a P);

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
                let mut wb = db.new_writebatch();
                let m = rng.borrow_mut().gen_range(1..20);
                for _ in 0..m {
                    let key = keygen();
                    let val: Vec<u8> = (0..8).map(|_| rng.borrow_mut().gen()).collect();
                    wb = wb.kv_insert(key, val.to_vec()).unwrap();
                }
                wb.commit();
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
                .map(|(data, hash)| (data, db.get_revision(&hash, None).unwrap()))
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
            .map(|(data, hash)| (data, db.get_revision(&hash, None).unwrap()))
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

    let mut wb = db.new_writebatch();

    let items = vec![
        ("d", "verb"),
        ("do", "verb"),
        ("doe", "reindeer"),
        ("e", "coin"),
    ];

    for (k, v) in items {
        wb = wb.kv_insert(k.as_bytes(), v.as_bytes().to_vec()).unwrap();
    }
    wb.commit();
    let root_hash = db.kv_root_hash().unwrap();

    // Add second commit due to API restrictions
    let mut wb = db.new_writebatch();
    for (k, v) in Vec::from([("x", "two")]).iter() {
        wb = wb
            .kv_insert(k.to_string().as_bytes(), v.as_bytes().to_vec())
            .unwrap();
    }
    wb.commit();

    let rev = db.get_revision(&root_hash, None).unwrap();
    let key = "doe".as_bytes();
    let root_hash = rev.kv_root_hash();

    match rev.prove(key) {
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
    type Target = PersistedDb<Store>;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl<P: AsRef<Path> + ?Sized> DerefMut for Db<'_, P> {
    fn deref_mut(&mut self) -> &mut PersistedDb<Store> {
        &mut self.0
    }
}
