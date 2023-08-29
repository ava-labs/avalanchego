fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::compile_protos("proto/sync/sync.proto")?;
    tonic_build::compile_protos("proto/rpcdb/rpcdb.proto")?;

    Ok(())
}
