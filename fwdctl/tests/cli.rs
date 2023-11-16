// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use anyhow::{anyhow, Result};
use assert_cmd::Command;
use predicates::prelude::*;
use serial_test::serial;
use std::{fs::remove_dir_all, path::PathBuf};

const PRG: &str = "fwdctl";
const VERSION: &str = env!("CARGO_PKG_VERSION");

// Removes the firewood database on disk
fn fwdctl_delete_db() -> Result<()> {
    if let Err(e) = remove_dir_all(tmpdb::path()) {
        eprintln!("failed to delete testing dir: {e}");
        return Err(anyhow!(e));
    }

    Ok(())
}

#[test]
#[serial]
fn fwdctl_prints_version() -> Result<()> {
    let expected_version_output: String = format!("{PRG} {VERSION}\n");

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
        .arg(tmpdb::path())
        .assert()
        .success();

    fwdctl_delete_db().map_err(|e| anyhow!(e))?;

    Ok(())
}

#[test]
#[serial]
fn fwdctl_insert_successful() -> Result<()> {
    // Create db
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg(tmpdb::path())
        .assert()
        .success();

    // Insert data
    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db"])
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    fwdctl_delete_db().map_err(|e| anyhow!(e))?;

    Ok(())
}

#[test]
#[serial]
fn fwdctl_get_successful() -> Result<()> {
    // Create db and insert data
    Command::cargo_bin(PRG)?
        .arg("create")
        .args([tmpdb::path()])
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db"])
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get value back out
    Command::cargo_bin(PRG)?
        .arg("get")
        .args(["year"])
        .args(["--db"])
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::contains("2023"));

    fwdctl_delete_db().map_err(|e| anyhow!(e))?;

    Ok(())
}

#[test]
#[serial]
fn fwdctl_delete_successful() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db"])
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Delete key -- prints raw data of deleted value
    Command::cargo_bin(PRG)?
        .arg("delete")
        .args(["year"])
        .args(["--db"])
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::contains("key year deleted successfully"));

    fwdctl_delete_db().map_err(|e| anyhow!(e))?;

    Ok(())
}

#[test]
#[serial]
fn fwdctl_root_hash() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db"])
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get root
    Command::cargo_bin(PRG)?
        .arg("root")
        .args(["--db"])
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::is_empty().not());

    fwdctl_delete_db().map_err(|e| anyhow!(e))?;

    Ok(())
}

#[test]
#[serial]
fn fwdctl_dump() -> Result<()> {
    Command::cargo_bin(PRG)?
        .arg("create")
        .arg(tmpdb::path())
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db"])
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get root
    Command::cargo_bin(PRG)?
        .arg("dump")
        .args([tmpdb::path()])
        .assert()
        .success()
        .stdout(predicate::str::contains("2023"));

    fwdctl_delete_db().map_err(|e| anyhow!(e))?;

    Ok(())
}

// A module to create a temporary database name for use in
// tests. The directory will be one of:
// - cargo's compile-time CARGO_TARGET_TMPDIR, if that exists
// - the value of the TMPDIR environment, if that is set, or
// - fallback to /tmp

// using cargo's CARGO_TARGET_TMPDIR ensures that multiple runs
// of this in different directories will have different databases

mod tmpdb {
    use super::*;

    const FIREWOOD_TEST_DB_NAME: &str = "test_firewood";
    const TARGET_TMP_DIR: Option<&str> = option_env!("CARGO_TARGET_TMPDIR");

    pub fn path() -> PathBuf {
        TARGET_TMP_DIR
            .map(PathBuf::from)
            .or_else(|| std::env::var("TMPDIR").ok().map(PathBuf::from))
            .unwrap_or(std::env::temp_dir())
            .join(FIREWOOD_TEST_DB_NAME)
    }
}
