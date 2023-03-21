use std::{sync::Arc, vec};

use firewood::db::DBConfig;
use firewood_connection::Manager;
use serial_test::serial;

#[tokio::test]
#[serial]
async fn test_manager() {
    let cfg = setup_db();

    let manager = Manager::new("/tmp/simple_db", cfg).await.unwrap();

    manager
        .call(|db| {
            db.new_writebatch()
                .kv_insert("foo", "bar".as_bytes().to_vec())
                .unwrap()
                .commit();
        })
        .await;

    let resp = manager.call(|db| db.kv_get(b"foo").unwrap()).await;

    assert_eq!(resp, "bar".as_bytes().to_vec());
    cleanup_db()
}

#[tokio::test]
#[serial]
async fn concurrent_reads_writes() {
    let cfg = setup_db();

    let manager = Manager::new("/tmp/simple_db", cfg).await.unwrap();

    manager
        .call(|db| {
            let items = vec![
                ("d", "verb"),
                ("do", "verb"),
                ("doe", "reindeer"),
                ("e", "coin"),
            ];

            for (k, v) in items.iter() {
                db.new_writebatch()
                    .kv_insert(k, v.as_bytes().to_vec())
                    .unwrap()
                    .commit();
            }
        })
        .await;

    let handle = tokio::spawn(async move {
        manager.call(|db| db.kv_get(b"doe").unwrap()).await;
    })
    .await;

    match handle {
        Ok(_) => cleanup_db(),
        Err(e) => log::error!("{e}"),
    }
}

#[tokio::test(flavor = "multi_thread")]
#[serial]
async fn multi_thread_read_writes() {
    let cfg = setup_db();

    let manager = Arc::new(Manager::new("/tmp/simple_db", cfg).await.unwrap());

    let keys = vec!["a", "b", "c", "d", "e"];
    let m1 = manager.clone();

    tokio::spawn(async move {
        for k in keys {
            m1.call(move |db| {
                db.new_writebatch()
                    .kv_insert(k, "z".as_bytes().to_vec())
                    .unwrap()
                    .commit()
            })
            .await;
        }
    });

    let m2 = manager.clone();
    let keys2 = vec!["a", "b", "c", "d", "e"];

    tokio::spawn(async move {
        for k in keys2 {
            m2.call(move |db| {
                db.new_writebatch()
                    .kv_insert(k, "x".as_bytes().to_vec())
                    .unwrap()
                    .commit()
            })
            .await
        }
    });

    let m3 = manager;
    let keys3 = vec!["a", "b", "c", "d", "e"];

    tokio::spawn(async move {
        for k in keys3 {
            m3.call(move |db| {
                println!("{:?}", db.kv_get(k));
            })
            .await
        }
    });
}

#[cfg(test)]
fn setup_db() -> DBConfig {
    let _ = env_logger::builder()
        .filter_level(log::LevelFilter::Info)
        .is_test(true)
        .try_init();

    DBConfig::builder()
        .wal(firewood::db::WALConfig::builder().max_revisions(10).build())
        .truncate(true)
        .build()
}

// Removes the firewood database on disk
#[cfg(test)]
fn cleanup_db() {
    use std::fs::remove_dir_all;

    if let Err(e) = remove_dir_all("/tmp/simple_db") {
        eprintln!("failed to delete testing dir: {e}");
    }
}
