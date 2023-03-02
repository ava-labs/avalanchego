use aiofut::AIOBuilder;
use futures::executor::LocalPool;
use futures::future::FutureExt;
use futures::task::LocalSpawnExt;
use std::os::unix::io::AsRawFd;

#[test]
fn simple1() {
    let aiomgr = AIOBuilder::default().max_events(100).build().unwrap();
    let file = std::fs::OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(true)
        .open("test")
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
    let aiomgr = AIOBuilder::default().build().unwrap();
    let file = std::fs::OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(true)
        .open("test2")
        .unwrap();
    let fd = file.as_raw_fd();
    let ws = (0..4000)
        .into_iter()
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
