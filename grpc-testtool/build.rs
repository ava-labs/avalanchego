// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // we want to import these proto files
    let import_protos = ["sync", "rpcdb", "process-server"];

    let protos: Box<[PathBuf]> = import_protos
        .into_iter()
        .map(|proto| PathBuf::from(format!("proto/{proto}/{proto}.proto")))
        .collect();

    // go through each proto and build it, also let cargo know we rerun this if the file changes
    for proto in protos.iter() {
        tonic_build::compile_protos(proto)?;

        // this improves recompile times; we only rerun tonic if any of these files change
        println!("cargo:rerun-if-changed={}", proto.display());
    }

    Ok(())
}
