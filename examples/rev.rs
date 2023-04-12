use firewood::db::{DBConfig, WALConfig, DB};

fn main() {
    let cfg = DBConfig::builder().wal(WALConfig::builder().max_revisions(10).build());
    {
        let db = DB::new("rev_db", &cfg.clone().truncate(true).build()).unwrap();
        let items = vec![
            ("do", "verb"),
            ("doe", "reindeer"),
            ("dog", "puppy"),
            ("doge", "coin"),
            ("horse", "stallion"),
            ("ddd", "ok"),
        ];
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
        println!(
            "{}",
            hex::encode(*db.get_revision(2, None).unwrap().kv_root_hash().unwrap())
        );
    }
    {
        let db = DB::new("rev_db", &cfg.truncate(false).build()).unwrap();
        {
            let rev = db.get_revision(1, None).unwrap();
            print!("{}", hex::encode(*rev.kv_root_hash().unwrap()));
            rev.kv_dump(&mut std::io::stdout()).unwrap();
        }
        {
            let rev = db.get_revision(2, None).unwrap();
            print!("{}", hex::encode(*rev.kv_root_hash().unwrap()));
            rev.kv_dump(&mut std::io::stdout()).unwrap();
        }
    }
}
