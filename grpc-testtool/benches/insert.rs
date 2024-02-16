// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use criterion::{criterion_group, criterion_main, BatchSize, BenchmarkId, Criterion};
use rand::{distributions::Alphanumeric, Rng, SeedableRng};
use std::{
    borrow::BorrowMut as _, cell::RefCell, env, fs::remove_dir_all, net::TcpStream,
    os::unix::process::CommandExt, path::PathBuf, thread::sleep, time::Duration,
};

use rpc::rpcdb::{self, PutRequest, WriteBatchRequest};
pub use rpc::service::Database as DatabaseService;

use rpcdb::database_client::DatabaseClient;

use std::process::Command;

/// The directory where the database will be created
const TESTDIR: &str = "/tmp/benchdb";
/// The port to use for testing
const TESTPORT: u16 = 5000;
/// The URI to connect to; this better match the TESTPORT
const TESTURI: &str = "http://localhost:5000";
/// Retry timeouts (in seconds); we want this long for processes
/// to start and exit
const RETRY_TIMEOUT_SEC: u32 = 5;

/// Merkledb configuration options; these are ignored by the rust side
/// These were chosen to be as close to the defaults for firewood as
/// possible. NodeCacheSize is a guess.
const MERKLEDB_OPTIONAL_CONFIGURATIONS: &str = r#"
    {
    "BranchFactor":16,
    "ProcessName":"bench",
    "HistoryLength":100,
    "NodeCacheSize":1000
    }
"#;

/// Clean up anything that might be left from prior runs
fn stop_everything() -> Result<(), std::io::Error> {
    // kill all process servers
    Command::new("killall").arg("process-server").output()?;

    // wait for them to die
    retry("process-server wouldn't die", || {
        process_server_pids().is_empty()
    });

    // remove test directory, ignoring any errors
    let _ = remove_dir_all(TESTDIR);

    Ok(())
}
fn reset_everything() -> Result<(), std::io::Error> {
    stop_everything()?;
    // find the process server
    let process_server = process_server_path().expect("Can't find process-server on path");
    eprintln!("Using process-server {}", process_server.display());

    // spawn a new one; use a separate thread to avoid zombies
    std::thread::spawn(|| {
        Command::new(process_server)
            .arg("--grpc-port")
            .arg(TESTPORT.to_string())
            .arg("--db-dir")
            .arg(TESTDIR)
            .arg("--config")
            .arg(MERKLEDB_OPTIONAL_CONFIGURATIONS)
            .process_group(0)
            .spawn()
            .expect("unable to start process-server")
            .wait()
    });

    // wait for it to accept connections
    retry("couldn't connect to process-server", || {
        TcpStream::connect(format!("localhost:{TESTPORT}")).is_ok()
    });

    Ok(())
}

/// Poll a function until it returns true
///
/// Pass in a message and a closure to execute. Panics if it times out.
///
/// # Arguments
///
/// * `msg` - The message to render if it panics
/// * `t` - The test closure
fn retry<TEST: Fn() -> bool>(msg: &str, t: TEST) {
    const TEST_INTERVAL_MS: u32 = 50;
    for _ in 0..=(RETRY_TIMEOUT_SEC * 1000 / TEST_INTERVAL_MS) {
        sleep(Duration::from_millis(TEST_INTERVAL_MS as u64));
        if t() {
            return;
        }
    }
    panic!("{msg} timed out after {RETRY_TIMEOUT_SEC} second(s)");
}

/// Return a list of process IDs for any running process-server processes
fn process_server_pids() -> Vec<u32> {
    // Basically we do `ps -eo pid=,comm=` which removes the header from ps
    // and gives us just the pid and the command, then we look for process-server
    // TODO: we match "123456 process-server-something-else", which isn't ideal
    let cmd = Command::new("ps")
        .arg("-eo")
        .arg("pid=,comm=")
        .output()
        .expect("Can't run ps");
    String::from_utf8_lossy(&cmd.stdout)
        .lines()
        .filter_map(|line| {
            line.trim_start().find(" process-server").map(|pos| {
                str::parse(line.trim_start().get(0..pos).unwrap_or_default()).unwrap_or_default()
            })
        })
        .collect()
}

/// Finds the first process-server on the path, or in some predefined locations
///
/// If the process-server isn't on the path, look for it in a target directory
/// As a last resort, we check the parent in case you're running from the
/// grpc-testtool directory, or in the current directory
fn process_server_path() -> Option<PathBuf> {
    const OTHER_PLACES_TO_LOOK: &str = ":target/release:../target/release:.";

    env::var_os("PATH").and_then(|mut paths| {
        paths.push(OTHER_PLACES_TO_LOOK);
        env::split_paths(&paths)
            .filter_map(|dir| {
                let full_path = dir.join("process-server");
                if full_path.is_file() {
                    Some(full_path)
                } else {
                    None
                }
            })
            .next()
    })
}

/// The actual insert benchmark
fn insert<const BATCHSIZE: usize, const KEYLEN: usize, const DATALEN: usize>(
    criterion: &mut Criterion,
) {
    // we save the tokio runtime because the client is only valid from within the
    // same runtime
    let runtime = tokio::runtime::Runtime::new().expect("tokio startup");

    // clean up anything that was running before, and make sure we have an empty directory
    // to run the tests in
    reset_everything().expect("unable to reset everything");

    // We want a consistent seed, but we need different values for each batch, so we
    // reseed each time we compute more data from the next seed value upwards
    let seed = RefCell::new(0);

    let client = runtime
        .block_on(DatabaseClient::connect(TESTURI))
        .expect("connection succeeded");

    criterion.bench_with_input(
        BenchmarkId::new("insert", BATCHSIZE),
        &BATCHSIZE,
        |b, &s| {
            b.to_async(&runtime).iter_batched(
                || {
                    // seed a new random number generator to generate data
                    // each time we call this, we increase the seed by 1
                    // this gives us different but consistently different random data
                    let seed = {
                        let mut inner = seed.borrow_mut();
                        *inner += 1;
                        *inner
                    };
                    let mut rng = rand::rngs::StdRng::seed_from_u64(seed);

                    // generate the put request, which is BATCHSIZE PutRequest objects with random keys/values
                    let put_requests: Vec<PutRequest> = (0..s)
                        .map(|_| {
                            (
                                rng.borrow_mut()
                                    .sample_iter(&Alphanumeric)
                                    .take(KEYLEN)
                                    .collect::<Vec<u8>>(),
                                rng.borrow_mut()
                                    .sample_iter(&Alphanumeric)
                                    .take(DATALEN)
                                    .collect::<Vec<u8>>(),
                            )
                        })
                        .map(|(key, value)| PutRequest { key, value })
                        .collect();

                    // wrap it into a tonic::Request to pass to the execution routine
                    let req = tonic::Request::new(WriteBatchRequest {
                        puts: put_requests,
                        deletes: vec![],
                    });

                    // hand back the client and the request contents to the benchmark executor
                    (client.clone(), req)
                },
                // this part is actually timed
                |(mut client, req)| async move {
                    client
                        .write_batch(req)
                        .await
                        .expect("batch insert succeeds");
                },
                // I have no idea what this does, but the docs seem to say you almost always want this
                BatchSize::SmallInput,
            )
        },
    );

    stop_everything().expect("unable to stop the process server");
}

criterion_group! {
    name = benches;
    config = Criterion::default().sample_size(20);
    targets = insert::<1, 32, 32>, insert::<20, 32, 32>, insert::<10000, 32, 32>
}

criterion_main!(benches);
