// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::compile_protos("proto/sync/sync.proto")?;
    tonic_build::compile_protos("proto/rpcdb/rpcdb.proto")?;
    tonic_build::compile_protos("proto/process-server/process-server.proto")?;

    Ok(())
}
