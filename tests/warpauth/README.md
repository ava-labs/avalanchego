# warpauth: P-chain remote command from EVM addresses

Prototype of one new P-chain authorization path: an owner is satisfied by a
warp message from the C-chain instead of a secp256k1 signature. With it, any
EVM address (MetaMask EOA, Safe, any contract) can create subnets, stake,
and run L1 validators with ordinary C-chain transactions and no P-chain key.

## The rule

A P-chain `OutputOwners` address is 20 bytes, so an EVM address is already a
valid owner. `secp256k1fx.WarpCredential{Message}` is a new credential type,
accepted wherever a `secp256k1fx.Credential` is today (`VerifyTransfer`,
`VerifyPermission`). The message is an `AddressedCall` from the C-chain whose
payload is `owner (20 bytes) || unsigned P-chain tx bytes`, and it is valid
when:

- BLS quorum of the primary network signed it and its source chain is the
  C-chain (`txs/executor/warp_verifier.go`, same check as ACP-77 txs),
- the source address is one of the trusted helper contracts
  (`config.DefaultWarpHelperAddresses`, override with the P-chain chain
  config key `warp-helper-addresses`),
- the tx bytes equal the tx being verified, and every owner slot the input
  names is `owner` (`vms/secp256k1fx/warp_credential.go`).

No nonce or expiry: the credential is bound to one exact tx and that tx
consumes its UTXOs, so it cannot replay. Warp credentials are rejected before
Granite (`txs/executor/standard_tx_executor.go`).

## The flow

1. The dapp reads the owner's UTXOs (`platform.getUTXOs`, address encoded as
   bech32) and picks inputs. The P-chain never selects inputs.
2. The user sends one C-chain tx to `PChain.sol`, e.g.
   `createSubnet(utxos, change, owners)`. The contract encodes the P-chain tx
   in the Avalanche codec, prefixes `msg.sender` as the owner and calls
   `sendWarpMessage`. `msg.sender` at the precompile is the contract, so the
   P-chain trusts the contract's address to name the owner (the ACP-77
   validator-manager pattern). All 17 user tx types are covered.
3. Anyone runs a relayer (`go run ./tests/warpauth/relayer`): it watches the
   contract's `SendWarpMessage` logs, aggregates signatures with
   `warp_getMessageAggregateSignature`, rebuilds the tx from the payload
   (`Wrap`), attaches the message as every credential and calls
   `platform.issueTx`. The relayer holds no keys and no funds; the fee is
   burned from the tx's own inputs. Racing relayers are harmless: the node
   rejects the second copy.

## Deployment

`PChain.sol` is deployed with Nick's method: a presigned tx with a made-up
signature, so nobody holds the deployer key and the address commits to the
exact initcode. `go run ./tests/warpauth/nick -network mainnet|fuji` prints
the deployer to fund (0.4 AVAX), the contract address and the raw tx.
`TestDefaultHelperAddressesMatchContract` pins the hardcoded addresses to
the contract bytes.

## Tests

- `go test ./tests/warpauth/`: every contract function's output is
  byte-identical to `txs.Codec` (runs the bytecode in an in-memory EVM with
  a mock warp precompile); `Wrap` credential counts; Nick addresses.
- `go test ./vms/secp256k1fx/ ./vms/platformvm/ -run Warp`: fx semantics
  and a full VM path (wrong sender rejected, right sender commits).
- e2e on tmpnet (needs the xsvm plugin):
  `./scripts/build.sh && ./scripts/build_xsvm.sh && AVAGO_PLUGIN_DIR=$PWD/build/plugins ./bin/ginkgo -v --focus "Warp Credential" ./tests/e2e -- --avalanchego-path=$PWD/build/avalanchego`.
  It deploys the contract, runs the relayer, creates and transfers a subnet,
  delegates 25 AVAX and watches stake and reward return to the 0x address.

## Open points for review

- Activation is on Granite only because Helicon switches the C-chain to SAE,
  which has no `warp_*` RPC yet; the ACP picks its own activation.
- BLS quorum is checked at block build, not at mempool admission (same as
  `RegisterL1ValidatorTx`).
- Inputs are single-owner AVAX UTXOs (all a new user ever has); multisig
  UTXOs and genesis stakeable-lock outputs are out of scope.
- Moving AVAX between the C-chain and the P-chain without a secp signature
  (export to a derived address, credential-less import) is a separate step.
