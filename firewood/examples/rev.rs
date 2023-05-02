// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::{
    db::{DBConfig, Revision, WALConfig, DB},
    proof::Proof,
};

/// cargo run --example rev
fn main() {
    let cfg = DBConfig::builder().wal(WALConfig::builder().max_revisions(10).build());
    {
        let db = DB::new("rev_db", &cfg.clone().truncate(true).build())
            .expect("db initiation should succeed");
        let items = vec![("dof", "verb"), ("doe", "reindeer"), ("dog", "puppy")];
        for (k, v) in items.iter() {
            db.new_writebatch()
                .kv_insert(k, v.as_bytes().to_vec())
                .unwrap()
                .commit();

            let root_hash = db
                .kv_root_hash()
                .expect("root-hash for current state should exist");
            println!("{root_hash:?}");
        }
        db.kv_dump(&mut std::io::stdout()).unwrap();
        let revision = db.get_revision(0, None).expect("revision-0 should exist");
        let revision_root_hash = revision
            .kv_root_hash()
            .expect("root-hash for revision-0 should exist");
        println!("{revision_root_hash:?}");

        let current_root_hash = db
            .kv_root_hash()
            .expect("root-hash for current state should exist");
        // The following is true as long as there are no dirty-writes.
        assert_eq!(revision_root_hash, current_root_hash);

        let revision = db.get_revision(2, None).expect("revision-2 should exist");
        let revision_root_hash = revision
            .kv_root_hash()
            .expect("root-hash for revision-2 should exist");
        println!("{revision_root_hash:?}");

        // Get a revision while a batch is active.
        let revision_root_hash = db
            .get_revision(1, None)
            .expect("revision-1 should exist")
            .kv_root_hash()
            .expect("root-hash for revision-1 should exist");
        println!("{revision_root_hash:?}");

        let write = db.new_writebatch().kv_insert("k", vec![b'v']).unwrap();

        let actual_revision_root_hash = db
            .get_revision(1, None)
            .expect("revision-1 should exist")
            .kv_root_hash()
            .expect("root-hash for revision-1 should exist");
        assert_eq!(revision_root_hash, actual_revision_root_hash);

        // Read the uncommitted value while the batch is still active.
        let val = db.kv_get("k").unwrap();
        assert_eq!("v".as_bytes().to_vec(), val);

        write.commit();
        let new_revision_root_hash = db
            .get_revision(1, None)
            .expect("revision-1 should exist")
            .kv_root_hash()
            .expect("root-hash for revision-1 should exist");
        assert_ne!(revision_root_hash, new_revision_root_hash);

        let val = db.kv_get("k").unwrap();
        assert_eq!("v".as_bytes().to_vec(), val);
    }
    {
        let db =
            DB::new("rev_db", &cfg.truncate(false).build()).expect("db initiation should succeed");
        {
            let revision = db.get_revision(0, None).expect("revision-0 should exist");
            let revision_root_hash = revision
                .kv_root_hash()
                .expect("root-hash for revision-0 should exist");
            println!("{revision_root_hash:?}");

            let current_root_hash = db
                .kv_root_hash()
                .expect("root-hash for current state should exist");
            // The following is true as long as the current state is fresh after replaying from WALs.
            assert_eq!(revision_root_hash, current_root_hash);

            let revision = db.get_revision(1, None).expect("revision-1 should exist");
            revision.kv_dump(&mut std::io::stdout()).unwrap();

            let mut items_rev = vec![("dof", "verb"), ("doe", "reindeer")];
            items_rev.sort();
            let (keys, vals) = items_rev.clone().into_iter().unzip();

            let proof = build_proof(&revision, &items_rev);
            revision
                .verify_range_proof(
                    proof,
                    items_rev[0].0,
                    items_rev[items_rev.len() - 1].0,
                    keys,
                    vals,
                )
                .unwrap();
        }
        {
            let revision = db.get_revision(2, None).expect("revision-2 should exist");
            let revision_root_hash = revision
                .kv_root_hash()
                .expect("root-hash for revision-2 should exist");
            println!("{revision_root_hash:?}");
            revision.kv_dump(&mut std::io::stdout()).unwrap();

            let mut items_rev = vec![("dof", "verb")];
            items_rev.sort();
            let (keys, vals) = items_rev.clone().into_iter().unzip();

            let proof = build_proof(&revision, &items_rev);
            revision
                .verify_range_proof(
                    proof,
                    items_rev[0].0,
                    items_rev[items_rev.len() - 1].0,
                    keys,
                    vals,
                )
                .unwrap();
        }
    }
}

fn build_proof(revision: &Revision, items: &[(&str, &str)]) -> Proof {
    let mut proof = revision.prove(items[0].0).unwrap();
    let end = revision.prove(items.last().unwrap().0).unwrap();
    proof.concat_proofs(end);
    proof
}
