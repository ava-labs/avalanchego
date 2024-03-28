# Platform VM

`PlatformVM` is the virtual machine implementing the P-chain in `avalanchego`.

As such `PlatformVM` is responsible for:

- Tracking permissioned and elastic Subnets, including the Primary Network, which is defined in P-chain genesis.
- Tracking every chain associated with each Subnet.
- Tracking every active staker, validators and delegators, associated with each elastic Subnet and the Primary Network.
- Measuring uptimes of each staker associated with the Primary Network and with elastic Subnets, in order to decide whether they should be rewarded or not at the end of their lifetime.
- Hosting on-chain voting on elastic Subnets rewarding, via Proposal Blocks.
- Exposing the validators set of any elastic Subnets at a past height, in order to support Warp protocol.

## Technical content

Here is a list of technical details about the way `PlatformVM` works:

- `ChainTime` helps tracking stakers' uptime and rewarding them. You can read [here](./docs/chain_time.md) details of how P-chain mananges it.
- Validators are versioned to support the Warp protocol. You can read [here](./docs/validators_versioning.md) details of how the P-chain stores validator set versions.
- Subnets lifetime is tracked and managed in the P-chain. You can read [here](./docs/subnets.md) details of what transactions can be used to affect a Subnet.
- The P-chain can process different kind of blocks. You can read [here](./docs/block_formation_logic.md) details of how blocks have changed across forks.

Note that you can find details about P-chain transactions list and details [here](https://docs.avax.network/reference/avalanchego/p-chain/txn-format).

Note that transactions dissemination has been consolidated into an sdk package. You can find the relevant code [here](../../network/p2p/).
