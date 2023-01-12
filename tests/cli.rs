use anyhow::Result;
use assert_cmd::Command;
use predicates::prelude::*;

const PRG: &str = "fwdctl";
const VERSION: &str = env!("CARGO_PKG_VERSION");
const FIREWOOD: &str = "firewood";

#[test]
fn fwdctl_prints_version() -> Result<()> {
    let expected_version_output: String = format!("{FIREWOOD} {VERSION}\n");

    // version is defined and succeeds with the desired output
    Command::cargo_bin(PRG)?
        .args(["-V"])
        .assert()
        .success()
        .stdout(predicate::str::contains(expected_version_output));

    Ok(())
}

#[test]
fn fwdctl_creates_database() -> Result<()> {
    // version is defined and succeeds with the desired output
    Command::cargo_bin(PRG)?.arg("create").assert().success();

    Ok(())
}

#[test]
#[ignore] // TODO
fn fwdctl_insert_successful() -> Result<()> {
    // Create db
    fwdctl_creates_database()?;

    // Insert data
    Command::cargo_bin(PRG)?
        .args(["insert", "--", "--key year", "--value hello"])
        .assert()
        .success()
        .stdout(predicate::str::contains("year"));

    Ok(())
}
