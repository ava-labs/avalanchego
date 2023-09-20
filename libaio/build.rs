// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::env;

fn main() {
    let out_dir = env::var("OUT_DIR").unwrap();
    std::process::Command::new("make")
        .args(&[format!("{}/libaio.a", out_dir)])
        .env("OUT_DIR", &out_dir)
        .current_dir("./libaio")
        .status()
        .expect("failed to make libaio");
    eprintln!("{}", out_dir);
    // the current source version of libaio is 0.3.112
    println!("cargo:rerun-if-changed={}/libaio.a", out_dir);
    println!("cargo:rustc-link-search=native={}", out_dir);
}
