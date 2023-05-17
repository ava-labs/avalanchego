// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[cfg(not(feature = "eth"))]
fn main() {
    println!("To run this example, you must enable to eth feature.");
    println!("For more information on features, see:");
    println!("https://doc.rust-lang.org/cargo/reference/features.html");
}

#[cfg(feature = "eth")]
use clap::{command, Arg, ArgMatches};
#[cfg(feature = "eth")]
use firewood::db::{Db, DbConfig, DbError, WalConfig};

/// cargo run --example dump benchmark_db/
#[cfg(feature = "eth")]
fn main() {
    let matches = command!()
        .arg(
            Arg::new("INPUT")
                .help("db path name")
                .required(false)
                .index(1),
        )
        .get_matches();
    let path = get_db_path(matches);
    let db = Db::new(
        path.unwrap().as_str(),
        &DbConfig::builder().truncate(false).build(),
    )
    .unwrap();
    let mut stdout = std::io::stdout();

    println!("== Account Model ==");
    db.dump(&mut stdout).unwrap();

    println!("== Generic KV ==");
    db.kv_dump(&mut stdout).unwrap();
}

/// Returns the provided INPUT db path if one is provided.
/// Otherwise, instantiate a DB called simple_db and return the path.
#[cfg(feature = "eth")]
fn get_db_path(matches: ArgMatches) -> Result<String, DbError> {
    if let Some(m) = matches.get_one::<String>("INPUT") {
        return Ok(m.to_string());
    }

    // Build and provide a new db path
    let cfg = DbConfig::builder().wal(WalConfig::builder().max_revisions(10).build());
    let db = Db::new("simple_db", &cfg.truncate(true).build()).unwrap();
    db.new_writebatch()
        .set_balance(b"ted", 10.into())
        .unwrap()
        .set_code(b"ted", b"smart contract byte code here!")
        .unwrap()
        .set_nonce(b"ted", 10086)
        .unwrap()
        .set_state(b"ted", b"x", b"1".to_vec())
        .unwrap()
        .set_state(b"ted", b"y", b"2".to_vec())
        .unwrap()
        .commit();
    Ok("simple_db".to_string())
}
