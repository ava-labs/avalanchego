// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![allow(clippy::unwrap_used)]

use predicates::prelude::*;
use std::fs;

const PRG: &str = "fwdctl";
const VERSION: &str = env!("CARGO_PKG_VERSION");

macro_rules! cargo_bin_cmd {
    () => {
        ::assert_cmd::cargo::cargo_bin_cmd!("fwdctl")
    };
}

#[test]
fn fwdctl_prints_version() {
    let expected_version_output: String = format!("{PRG} {VERSION}");

    // version is defined and succeeds with the desired output
    cargo_bin_cmd!()
        .args(["-V"])
        .assert()
        .success()
        .stdout(predicate::str::contains(expected_version_output));
}

#[test]
fn fwdctl_creates_database() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();
}

#[test]
fn fwdctl_insert_successful() {
    let tmpdir = tempfile::tempdir().unwrap();

    // Create db
    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    // Insert data
    cargo_bin_cmd!()
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));
}

#[test]
fn fwdctl_get_successful() {
    let tmpdir = tempfile::tempdir().unwrap();

    // Create db and insert data
    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get value back out
    cargo_bin_cmd!()
        .arg("get")
        .args(["year"])
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("2023"));
}

#[test]
fn fwdctl_delete_successful() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Delete key -- prints raw data of deleted value
    cargo_bin_cmd!()
        .arg("delete")
        .args(["year"])
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("key year deleted successfully"));
}

#[test]
fn fwdctl_root_hash() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get root
    cargo_bin_cmd!()
        .arg("root")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::is_empty().not());
}

#[test]
fn fwdctl_dump() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get root
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("2023"));
}

#[test]
fn test_slow_fwdctl_dump_with_start_stop_and_max() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["a"])
        .args(["1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("a"));

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["b"])
        .args(["2"])
        .assert()
        .success()
        .stdout(predicate::str::contains("b"));

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["c"])
        .args(["3"])
        .assert()
        .success()
        .stdout(predicate::str::contains("c"));

    // Test stop in the middle
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["--stop-key"])
        .arg("b")
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "Next key is c, resume with \"--start-key=c\"",
        ));

    // Test stop in the end
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["--stop-key"])
        .arg("c")
        .assert()
        .success()
        .stdout(predicate::str::contains(
            "There is no next key. Data dump completed.",
        ));

    // Test start in the middle
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["--start-key"])
        .arg("b")
        .assert()
        .success()
        .stdout(predicate::str::starts_with("\'b"));

    // Test start and stop
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
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
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
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
}

#[test]
fn test_slow_fwdctl_dump_with_csv_and_json() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["a"])
        .args(["1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("a"));

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["b"])
        .args(["2"])
        .assert()
        .success()
        .stdout(predicate::str::contains("b"));

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["c"])
        .args(["3"])
        .assert()
        .success()
        .stdout(predicate::str::contains("c"));

    // Test output csv
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["--output-format"])
        .arg("csv")
        .assert()
        .success()
        .stdout(predicate::str::contains("Dumping to dump.csv"));

    let contents = fs::read_to_string("dump.csv").expect("Should read dump.csv file");
    assert_eq!(contents, "a,1\nb,2\nc,3\n");
    fs::remove_file("dump.csv").expect("Should remove dump.csv file");

    // Test output json
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
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
}

#[test]
fn fwdctl_dump_with_file_name() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["a"])
        .args(["1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("a"));

    // Test without output format
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["--output-file-name"])
        .arg("test")
        .assert()
        .failure()
        .stderr(predicate::str::contains("--output-format"));

    // Test output csv
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
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
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
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
}

#[test]
fn test_slow_fwdctl_dump_with_hex() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["a"])
        .args(["1"])
        .assert()
        .success()
        .stdout(predicate::str::contains("a"));

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["b"])
        .args(["2"])
        .assert()
        .success()
        .stdout(predicate::str::contains("b"));

    cargo_bin_cmd!()
        .arg("insert")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["c"])
        .args(["3"])
        .assert()
        .success()
        .stdout(predicate::str::contains("c"));

    // Test without output format
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["--start-key"])
        .arg("a")
        .args(["--start-key-hex"])
        .arg("61")
        .assert()
        .failure()
        .stderr(predicate::str::contains("--start-key"))
        .stderr(predicate::str::contains("--start-key-hex"));

    // Test start with hex value
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["--start-key-hex"])
        .arg("62")
        .assert()
        .success()
        .stdout(predicate::str::starts_with("\'b"));

    // Test stop with hex value
    cargo_bin_cmd!()
        .arg("dump")
        .arg("--db")
        .arg(tmpdir.path())
        .args(["--stop-key-hex"])
        .arg("62")
        .assert()
        .success()
        .stdout(predicate::str::starts_with("\'a"))
        .stdout(predicate::str::contains("Next key is c"))
        .stdout(predicate::str::contains("--start-key=c"))
        .stdout(predicate::str::contains("--start-key-hex=63"));
}

#[test]
fn fwdctl_check_empty_db() {
    let tmpdir = tempfile::tempdir().unwrap();

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    cargo_bin_cmd!()
        .arg("check")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();
}

#[test]
fn test_slow_fwdctl_check_db_with_data() {
    use rand::{RngExt, distr::Alphanumeric};

    let tmpdir = tempfile::tempdir().unwrap();

    let rng = firewood_storage::SeededRng::from_env_or_random();
    let mut sample_iter = rng.sample_iter(Alphanumeric).map(char::from);

    cargo_bin_cmd!()
        .arg("create")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();

    // TODO: bulk loading data instead of inserting one by one
    for _ in 0..4 {
        let key = sample_iter.by_ref().take(64).collect::<String>();
        let value = sample_iter.by_ref().take(10).collect::<String>();
        cargo_bin_cmd!()
            .arg("insert")
            .arg("--db")
            .arg(tmpdir.path())
            .args([key])
            .args([value])
            .assert()
            .success();
    }

    cargo_bin_cmd!()
        .arg("check")
        .arg("--db")
        .arg(tmpdir.path())
        .assert()
        .success();
}
