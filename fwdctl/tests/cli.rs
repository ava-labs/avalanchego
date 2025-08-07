// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use anyhow::{Result, anyhow};
use assert_cmd::Command;
use predicates::prelude::*;
use serial_test::serial;
use std::fs::{self, remove_file};
use std::path::{Path, PathBuf};

const PRG: &str = "fwdctl";
const VERSION: &str = env!("CARGO_PKG_VERSION");

// Removes the firewood database on disk
fn fwdctl_delete_db() -> Result<()> {
    if let Err(e) = remove_file(tmpdb::path()) {
        eprintln!("failed to delete testing dir: {e}");
        return Err(anyhow!(e));
    }

    Ok(())
}

#[test]
#[serial]
fn fwdctl_prints_version() -> Result<()> {
    let expected_version_output: String = format!("{PRG} {VERSION}");

    // version is defined and succeeds with the desired output
    Command::cargo_bin(PRG)?
        .args(["-V"])
        .assert()
        .success()
        .stdout(predicate::str::contains(expected_version_output));

    Ok(())
}

#[test]
#[serial]
fn fwdctl_creates_database() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_insert_successful() -> Result<()> {
    // Create db
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    // Insert data
    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_get_successful() -> Result<()> {
    // Create db and insert data
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get value back out
    Command::cargo_bin(PRG)?
        .arg("get")
        .args(["year"])
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::contains("2023"));

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_delete_successful() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Delete key -- prints raw data of deleted value
    Command::cargo_bin(PRG)?
        .arg("delete")
        .args(["year"])
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::contains("key year deleted successfully"));

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_root_hash() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get root
    Command::cargo_bin(PRG)?
        .arg("root")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::is_empty().not());

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_dump() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get root
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success()
        .stdout(predicate::str::contains("2023"));

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_dump_with_start_stop_and_max() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["a"])
        .args(["1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("a"));

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["b"])
        .args(["2"])
        .assert()
        .success()
        .stdout(predicate::str::contains("b"));

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["c"])
        .args(["3"])
        .assert()
        .success()
        .stdout(predicate::str::contains("c"));

    // Test stop in the middle
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--stop-key"])
        .arg("b")
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "Next key is c, resume with \"--start-key=c\"",
        ));

    // Test stop in the end
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--stop-key"])
        .arg("c")
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "There is no next key. Data dump completed.",
        ));

    // Test start in the middle
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--start-key"])
        .arg("b")
        .assert()
        .success()
        .stdout(predicate::str::starts_with("\'b"));

    // Test start and stop
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--start-key"])
        .arg("b")
        .args(["--stop-key"])
        .arg("b")
        .assert()
        .success()
        .stdout(predicate::str::starts_with("\'b"))
        .stdout(predicate::str::contains(
            "Next key is c, resume with \"--start-key=c\"",
        ));

    // Test start and stop
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--start-key"])
        .arg("b")
        .args(["--max-key-count"])
        .arg("1")
        .assert()
        .success()
        .stdout(predicate::str::starts_with("\'b"))
        .stdout(predicate::str::contains(
            "Next key is c, resume with \"--start-key=c\"",
        ));

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_dump_with_csv_and_json() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["a"])
        .args(["1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("a"));

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["b"])
        .args(["2"])
        .assert()
        .success()
        .stdout(predicate::str::contains("b"));

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["c"])
        .args(["3"])
        .assert()
        .success()
        .stdout(predicate::str::contains("c"));

    // Test output csv
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--output-format"])
        .arg("csv")
        .assert()
        .success()
        .stdout(predicate::str::contains("Dumping to dump.csv"));

    let contents = fs::read_to_string("dump.csv").expect("Should read dump.csv file");
    assert_eq!(contents, "a,1\nb,2\nc,3\n");
    fs::remove_file("dump.csv").expect("Should remove dump.csv file");

    // Test output json
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--output-format"])
        .arg("json")
        .assert()
        .success()
        .stdout(predicate::str::contains("Dumping to dump.json"));

    let contents = fs::read_to_string("dump.json").expect("Should read dump.json file");
    assert_eq!(
        contents,
        "{\n  \"a\": \"1\",\n  \"b\": \"2\",\n  \"c\": \"3\"\n}\n"
    );
    fs::remove_file("dump.json").expect("Should remove dump.json file");

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_dump_with_file_name() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["a"])
        .args(["1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("a"));

    // Test without output format
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--output-file-name"])
        .arg("test")
        .assert()
        .failure()
        .stderr(predicate::str::contains("--output-format"));

    // Test output csv
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--output-format"])
        .arg("csv")
        .args(["--output-file-name"])
        .arg("test")
        .assert()
        .success()
        .stdout(predicate::str::contains("Dumping to test.csv"));

    let contents = fs::read_to_string("test.csv").expect("Should read test.csv file");
    assert_eq!(contents, "a,1\n");
    fs::remove_file("test.csv").expect("Should remove test.csv file");

    // Test output json
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--output-format"])
        .arg("json")
        .args(["--output-file-name"])
        .arg("test")
        .assert()
        .success()
        .stdout(predicate::str::contains("Dumping to test.json"));

    let contents = fs::read_to_string("test.json").expect("Should read test.json file");
    assert_eq!(contents, "{\n  \"a\": \"1\"\n}\n");
    fs::remove_file("test.json").expect("Should remove test.json file");

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_dump_with_hex() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["a"])
        .args(["1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("a"));

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["b"])
        .args(["2"])
        .assert()
        .success()
        .stdout(predicate::str::contains("b"));

    Command::cargo_bin(PRG)?
        .arg("insert")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["c"])
        .args(["3"])
        .assert()
        .success()
        .stdout(predicate::str::contains("c"));

    // Test without output format
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--start-key"])
        .arg("a")
        .args(["--start-key-hex"])
        .arg("61")
        .assert()
        .failure()
        .stderr(predicate::str::contains("--start-key"))
        .stderr(predicate::str::contains("--start-key-hex"));

    // Test start with hex value
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--start-key-hex"])
        .arg("62")
        .assert()
        .success()
        .stdout(predicate::str::starts_with("\'b"));

    // Test stop with hex value
    Command::cargo_bin(PRG)?
        .arg("dump")
        .arg("--db")
        .arg(tmpdb::path())
        .args(["--stop-key-hex"])
        .arg("62")
        .assert()
        .success()
        .stdout(predicate::str::starts_with("\'a"))
        .stdout(predicate::str::contains("Next key is c"))
        .stdout(predicate::str::contains("--start-key=c"))
        .stdout(predicate::str::contains("--start-key-hex=63"));

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_check_empty_db() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("check")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    fwdctl_delete_db()
}

#[test]
#[serial]
fn fwdctl_check_db_with_data() -> Result<()> {
    use rand::distr::Alphanumeric;
    use rand::rngs::StdRng;
    use rand::{Rng, SeedableRng, rng};

    let seed = std::env::var("FIREWOOD_TEST_SEED")
        .ok()
        .map_or_else(
            || None,
            |s| Some(str::parse(&s).expect("couldn't parse FIREWOOD_TEST_SEED; must be a u64")),
        )
        .unwrap_or_else(|| rng().random());

    eprintln!("Seed {seed}: to rerun with this data, export FIREWOOD_TEST_SEED={seed}");
    let rng = StdRng::seed_from_u64(seed);
    let mut sample_iter = rng.sample_iter(Alphanumeric).map(char::from);

    Command::cargo_bin(PRG)?
        .arg("create")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    // TODO: bulk loading data instead of inserting one by one
    for _ in 0..4 {
        let key = sample_iter.by_ref().take(64).collect::<String>();
        let value = sample_iter.by_ref().take(10).collect::<String>();
        Command::cargo_bin(PRG)?
            .arg("insert")
            .arg("--db")
            .arg(tmpdb::path())
            .args([key])
            .args([value])
            .assert()
            .success();
    }

    Command::cargo_bin(PRG)?
        .arg("check")
        .arg("--db")
        .arg(tmpdb::path())
        .assert()
        .success();

    fwdctl_delete_db()
}

// A module to create a temporary database name for use in
// tests. The directory will be one of:
// - cargo's compile-time CARGO_TARGET_TMPDIR, if that exists
// - the value of the TMPDIR environment, if that is set, or
// - fallback to /tmp

// using cargo's CARGO_TARGET_TMPDIR ensures that multiple runs
// of this in different directories will have different databases

mod tmpdb {
    use std::sync::OnceLock;

    use super::*;

    const FIREWOOD_TEST_DB_NAME: &str = "test_firewood";
    const TARGET_TMP_DIR: Option<&str> = option_env!("CARGO_TARGET_TMPDIR");

    pub fn path() -> &'static Path {
        static PATH: OnceLock<PathBuf> = OnceLock::new();
        PATH.get_or_init(|| {
            TARGET_TMP_DIR
                .map(PathBuf::from)
                .or_else(|| std::env::var("TMPDIR").ok().map(PathBuf::from))
                .unwrap_or(std::env::temp_dir())
                .join(FIREWOOD_TEST_DB_NAME)
        })
        .as_path()
    }
}
