use anyhow::Result;
use assert_cmd::Command;
use predicates::prelude::*;

const PRG: &str = "fwdctl";
const VERSION: &str = env!("CARGO_PKG_VERSION");

#[test]
fn prints_version() -> Result<()> {
    let expected_version_output: String = format!("{PRG} {VERSION}");

    // version is defined and succeeds with the desired output
    Command::cargo_bin(PRG)?
        .args(["-V"])
        .assert()
        .success()
        .stdout(predicate::str::contains(expected_version_output));

    Ok(())
}
