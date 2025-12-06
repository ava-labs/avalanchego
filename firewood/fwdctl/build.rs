// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::process::Command;

fn main() {
    // Get the git commit SHA
    let git_sha = match Command::new("git").args(["rev-parse", "HEAD"]).output() {
        Ok(output) => {
            if output.status.success() {
                String::from_utf8_lossy(&output.stdout).trim().to_string()
            } else {
                let error_msg = String::from_utf8_lossy(&output.stderr);
                format!("git error: {}", error_msg.trim())
            }
        }
        Err(e) => {
            format!("git not found: {e}")
        }
    };

    // Check if ethhash feature is enabled
    let ethhash_feature = if cfg!(feature = "ethhash") {
        "ethhash"
    } else {
        "-ethhash"
    };

    // Make the git SHA and ethhash status available to the main.rs file
    println!("cargo:rustc-env=GIT_COMMIT_SHA={git_sha}");
    println!("cargo:rustc-env=ETHHASH_FEATURE={ethhash_feature}");

    // Re-run this build script if the git HEAD changes
    println!("cargo:rerun-if-changed=.git/HEAD");
    println!("cargo:rerun-if-changed=.git/index");
}
