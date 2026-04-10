Place the committed armored package-signing public key here as
`avalanchego-signing-public.asc` once the AWS KMS-backed signing identity
has been provisioned.

Until that key is committed, the workflows export a runtime copy of the
public key generated from the configured signer and use that for
validation and artifacts.
