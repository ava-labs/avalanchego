# Subnet-EVM SAE VM (`subnetevm`)

`subnetevm` is the SAE-based Subnet-EVM L1 VM. It uses the generic execution,
settlement, and Warp machinery in [`vms/saevm`](../), while reusing
[`graft/subnet-evm`](../../../graft/subnet-evm) for chain configuration, header
extensions, precompiles, and upgrade semantics. It runs as an out-of-process
`rpcchainvm` plugin.

Build the plugin with:

```sh
task build-subnet-evm-sae
```

Run its package tests and Warp end-to-end suite with:

```sh
go test ./vms/saevm/subnetevm/...
task test-e2e-warp-sae
```

## Boundaries

The VM follows the SAE C-Chain wrapper where the mechanics are common, but it
does not inherit C-Chain-only behavior. Atomic transactions, shared memory,
native-asset legacy plumbing, ExtData fields, and the `/avax` namespace are not
part of this VM.

Subnet-EVM-specific code is concentrated here:

- [`genesis.go`](genesis.go) parses Subnet-EVM genesis and upgrade bytes.
- [`hooks.go`](hooks.go) connects Subnet-EVM rules and precompiles to SAE.
- [`gas_config.go`](gas_config.go) encodes ACP-224 gas configuration in headers.
- [`api`](api) exposes `eth_getActiveRulesAt` and the validators service.
- [`warp`](warp) adds Subnet-EVM Warp predicates and uptime verification.
- [`plugin`](plugin) is the standalone VM entrypoint.

Scalar ACP-176 and ACP-226 math belongs to
[`vms/evm/dynamic`](../../evm/dynamic), not either chain wrapper. Composite gas
state remains in [`vms/evm/acp176`](../../evm/acp176).

## Consensus invariants

SAE separates building, execution, and settlement. Code that affects block
construction must therefore use a state view that every verifier can
reproduce:

- reward routing and worst-case transaction admission use settled state;
- inbound mempool admission uses last-executed state;
- gas pricing uses the ACP-224 configuration carried by the parent header.

Subnet-EVM's existing header-extra type carries the SAE settlement marker,
ACP-176 target state, ACP-226 minimum-delay exponent, and ACP-224 gas
configuration. These optional tail fields are populated only from Helicon
onward so pre-Helicon RLP encodings remain unchanged. Each settlement or gas
configuration group must be either complete or absent.

When `gaspricemanager` is active, the builder reads its configuration from
settled state and stamps it into the header. Historical replay and verification
can then derive pricing from the parent header without reopening historical
state. Genesis configuration is the fallback before the first stamped header.
An active manager with zero-valued storage is an error.

`txallowlist` is enforced at transaction admission because rejection affects
block validity. `deployerallowlist` remains in the EVM execution path so nested
contract creation has the same rules as top-level creation.

Network upgrade overrides are applied before chain-config validation and
Ethereum-fork alignment. Restart compatibility is checked against the
last-accepted block, with genesis used only for an empty database.

## Compatibility rules

The legacy `feeManager` precompile is retired at Helicon in both the legacy and
SAE parsers. Configurations at or after Helicon are rejected or removed when
immutable legacy genesis requires it. If the precompile is active before
Helicon, a synthetic disable upgrade wipes its state at the transition. Legacy
`FeeConfig` is defaulted and validated for pre-Helicon compatibility; ACP-176
and ACP-224 own pricing after Helicon.

Reward routing follows libevm semantics after London: coinbase receives the
effective tip and the base fee is burned. Legacy Subnet-EVM credited the full
effective gas price, so this is an intentional behavior change.

The SAE operator configuration is intentionally smaller than the legacy
plugin's configuration. Standalone per-chain databases, legacy state sync, and
deprecated RPC methods such as `eth_feeConfig` are not provided.

A legacy-to-SAE transition with already-active `gaspricemanager` storage is not
supported yet. Legacy headers do not contain the ACP-224 configuration needed
to seed the first SAE header; transition support must materialize that state
explicitly before this case can be enabled.
