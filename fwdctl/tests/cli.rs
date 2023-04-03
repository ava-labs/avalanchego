use anyhow::{anyhow, Result};
use assert_cmd::Command;
use predicates::prelude::*;
use serial_test::serial;
use std::fs::remove_dir_all;

const PRG: &str = "fwdctl";
const VERSION: &str = env!("CARGO_PKG_VERSION");
const FIREWOOD_TEST_DB_NAME: &str = "test_firewood";

// Removes the firewood database on disk
fn fwdctl_delete_db() -> Result<()> {
    if let Err(e) = remove_dir_all(FIREWOOD_TEST_DB_NAME) {
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
        .arg(FIREWOOD_TEST_DB_NAME)
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
        .arg(FIREWOOD_TEST_DB_NAME)
        .assert()
        .success();

    // Insert data
    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db", FIREWOOD_TEST_DB_NAME])
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
        .args([FIREWOOD_TEST_DB_NAME])
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db", FIREWOOD_TEST_DB_NAME])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get value back out
    Command::cargo_bin(PRG)?
        .arg("get")
        .args(["year"])
        .args(["--db", FIREWOOD_TEST_DB_NAME])
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
        .arg(FIREWOOD_TEST_DB_NAME)
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db", FIREWOOD_TEST_DB_NAME])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Delete key -- prints raw data of deleted value
    Command::cargo_bin(PRG)?
        .arg("delete")
        .args(["year"])
        .args(["--db", FIREWOOD_TEST_DB_NAME])
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
        .arg(FIREWOOD_TEST_DB_NAME)
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db", FIREWOOD_TEST_DB_NAME])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get root
    Command::cargo_bin(PRG)?
        .arg("root")
        .args(["--db", FIREWOOD_TEST_DB_NAME])
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
        .arg(FIREWOOD_TEST_DB_NAME)
        .assert()
        .success();

    Command::cargo_bin(PRG)?
        .arg("insert")
        .args(["year"])
        .args(["2023"])
        .args(["--db", FIREWOOD_TEST_DB_NAME])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    // Get root
    Command::cargo_bin(PRG)?
        .arg("dump")
        .args([FIREWOOD_TEST_DB_NAME])
        .assert()
        .success()
        .stdout(predicate::str::is_empty().not());

    fwdctl_delete_db().map_err(|e| anyhow!(e))?;

    Ok(())
}
