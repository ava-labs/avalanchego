use aiofut::AioBuilder;
use futures::{executor::LocalPool, future::FutureExt, task::LocalSpawnExt};
use std::{os::unix::io::AsRawFd, path::PathBuf};

fn tmp_dir() -> PathBuf {
    option_env!("CARGO_TARGET_TMPDIR")
        .map(PathBuf::from)
        .unwrap_or(std::env::temp_dir())
}

#[test]
fn simple1() {
    let aiomgr = AioBuilder::default().max_events(100).build().unwrap();
    let file = std::fs::OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(true)
        .open(tmp_dir().join("test"))
        .unwrap();
    let fd = file.as_raw_fd();
    let ws = vec![(0, "hello"), (5, "world"), (2, "xxxx")]
        .into_iter()
        .map(|(off, s)| aiomgr.write(fd, off, s.as_bytes().into(), None))
        .collect::<Vec<_>>();
    let mut pool = LocalPool::new();
    let spawner = pool.spawner();
    for w in ws.into_iter() {
        let h = spawner.spawn_local_with_handle(w).unwrap().map(|r| {
            println!("wrote {} bytes", r.0.unwrap());
        });
        spawner.spawn_local(h).unwrap();
    }
    pool.run();
}

#[test]
fn simple2() {
    let aiomgr = AioBuilder::default().build().unwrap();
    let file = std::fs::OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(true)
        .open(tmp_dir().join("test2"))
        .unwrap();
    let fd = file.as_raw_fd();
    let ws = (0..4000)
        .map(|i| {
            let off = i * 128;
            let s = char::from((97 + i % 26) as u8)
                .to_string()
                .repeat((i + 1) as usize);
            aiomgr.write(fd, off as u64, s.as_bytes().into(), None)
        })
        .collect::<Vec<_>>();
    let mut pool = LocalPool::new();
    let spawner = pool.spawner();
    for w in ws.into_iter() {
        let h = spawner.spawn_local_with_handle(w).unwrap().map(|r| {
            println!("wrote {} bytes", r.0.unwrap());
        });
        spawner.spawn_local(h).unwrap();
    }
    pool.run();
}
