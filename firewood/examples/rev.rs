// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::db::{DBConfig, WALConfig, DB};

/// cargo run --example rev
fn main() {
    let cfg = DBConfig::builder().wal(WALConfig::builder().max_revisions(10).build());
    {
        let db = DB::new("rev_db", &cfg.clone().truncate(true).build()).unwrap();
        let items = vec![("dof", "verb"), ("doe", "reindeer"), ("dog", "puppy")];
        for (k, v) in items.iter() {
            db.new_writebatch()
                .kv_insert(k, v.as_bytes().to_vec())
                .unwrap()
                .commit();
            println!("{}", hex::encode(*db.kv_root_hash().unwrap()));
        }
        db.kv_dump(&mut std::io::stdout()).unwrap();
        println!(
            "{}",
            hex::encode(*db.get_revision(1, None).unwrap().kv_root_hash().unwrap())
        );
        let root_hash = *db.get_revision(1, None).unwrap().kv_root_hash().unwrap();
        println!(
            "{}",
            hex::encode(*db.get_revision(2, None).unwrap().kv_root_hash().unwrap())
        );
        let write = db.new_writebatch().kv_insert("k", vec![b'v']).unwrap();

        // Get a revision while a batch is active.
        println!(
            "{}",
            hex::encode(*db.get_revision(1, None).unwrap().kv_root_hash().unwrap())
        );
        assert_eq!(
            root_hash,
            *db.get_revision(1, None).unwrap().kv_root_hash().unwrap()
        );

        // Read the uncommitted value while the batch is still active.
        let val = db.kv_get("k").unwrap();
        assert_eq!("v".as_bytes().to_vec(), val);

        write.commit();
        println!(
            "{}",
            hex::encode(*db.get_revision(1, None).unwrap().kv_root_hash().unwrap())
        );
        let val = db.kv_get("k").unwrap();
        assert_eq!("v".as_bytes().to_vec(), val);
    }
    {
        let db = DB::new("rev_db", &cfg.truncate(false).build()).unwrap();
        {
            let rev = db.get_revision(1, None).unwrap();
            println!("{}", hex::encode(*rev.kv_root_hash().unwrap()));
            rev.kv_dump(&mut std::io::stdout()).unwrap();

            let mut items_rev_1 = vec![("dof", "verb"), ("doe", "reindeer")];
            items_rev_1.sort();
            let (keys, vals) = items_rev_1.clone().into_iter().unzip();

            let mut proof = rev.prove(items_rev_1[0].0).unwrap();
            let end_proof = rev.prove(items_rev_1[items_rev_1.len() - 1].0).unwrap();
            proof.concat_proofs(end_proof);

            rev.verify_range_proof(
                proof,
                items_rev_1[0].0,
                items_rev_1[items_rev_1.len() - 1].0,
                keys,
                vals,
            )
            .unwrap();
        }
        {
            let rev = db.get_revision(2, None).unwrap();
            print!("{}", hex::encode(*rev.kv_root_hash().unwrap()));
            rev.kv_dump(&mut std::io::stdout()).unwrap();

            let mut items_rev_2 = vec![("dof", "verb")];
            items_rev_2.sort();
            let (keys, vals) = items_rev_2.clone().into_iter().unzip();

            let mut proof = rev.prove(items_rev_2[0].0).unwrap();
            let end_proof = rev.prove(items_rev_2[items_rev_2.len() - 1].0).unwrap();
            proof.concat_proofs(end_proof);

            rev.verify_range_proof(
                proof,
                items_rev_2[0].0,
                items_rev_2[items_rev_2.len() - 1].0,
                keys,
                vals,
            )
            .unwrap();
        }
    }
}
