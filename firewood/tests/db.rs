use firewood::db::{DBConfig, WALConfig, DB};
use std::{collections::VecDeque, fs::remove_dir_all, path::Path};

macro_rules! kv_dump {
    ($e: ident) => {{
        let mut s = Vec::new();
        $e.kv_dump(&mut s).unwrap();
        String::from_utf8(s).unwrap()
    }};
}

#[test]
fn test_revisions() {
    use rand::{rngs::StdRng, Rng, SeedableRng};
    let cfg = DBConfig::builder()
        .meta_ncached_pages(1024)
        .meta_ncached_files(128)
        .payload_ncached_pages(1024)
        .payload_ncached_files(128)
        .payload_file_nbit(16)
        .payload_regn_nbit(16)
        .wal(
            WALConfig::builder()
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
        let db = DB::new("test_revisions_db", &cfg.clone().truncate(true).build()).unwrap();
        let mut dumped = VecDeque::new();
        for _ in 0..100 {
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
            }
            dumped.push_front(kv_dump!(db));
            for (i, _) in dumped.iter().enumerate().skip(1) {
                let rev = db.get_revision(i, None).unwrap();
                let a = &kv_dump!(rev);
                let b = &dumped[i];
                if a != b {
                    print!("{a}\n{b}");
                    panic!("not the same");
                }
            }
        }
        drop(db);
        let db = DB::new("test_revisions_db", &cfg.clone().truncate(false).build()).unwrap();
        for (j, _) in dumped.iter().enumerate().skip(1) {
            let rev = db.get_revision(j, None).unwrap();
            let a = &kv_dump!(rev);
            let b = &dumped[j];
            if a != b {
                print!("{a}\n{b}");
                panic!("not the same");
            }
        }
        println!("i = {i}");
    }
}

#[test]
fn create_db_issue_proof() {
    let cfg = DBConfig::builder()
        .meta_ncached_pages(1024)
        .meta_ncached_files(128)
        .payload_ncached_pages(1024)
        .payload_ncached_files(128)
        .payload_file_nbit(16)
        .payload_regn_nbit(16)
        .wal(
            WALConfig::builder()
                .file_nbit(15)
                .block_nbit(8)
                .max_revisions(10)
                .build(),
        );

    let db = DB::new("test_db_proof", &cfg.truncate(true).build()).unwrap();

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

    // Add second commit due to API restrictions
    let mut wb = db.new_writebatch();
    for (k, v) in Vec::from([("x", "two")]).iter() {
        wb = wb
            .kv_insert(k.to_string().as_bytes(), v.as_bytes().to_vec())
            .unwrap();
    }
    wb.commit();

    let rev = db.get_revision(1, None).unwrap();
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

    fwdctl_delete_db("test_db_proof");
}

// Removes the firewood database on disk
fn fwdctl_delete_db<P: AsRef<Path>>(path: P) {
    if let Err(e) = remove_dir_all(path) {
        eprintln!("failed to delete testing dir: {e}");
    }
}
