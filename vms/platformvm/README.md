# Platform VM

`PlatformVM` is the virtual machine implementing the [P-chain](https://support.avax.network/en/articles/4058243-what-is-the-platform-chain-p-chain) in `avalanchego`.

As such `PlatformVM` is responsible for:

- Tracking permissioned and [elastic](https://medium.com/avalancheavax/banff-elastic-subnets-44042f41e34c) subnets, including the Primary Network subnet, which is defined in P-chain genesis.
- Tracking every chains associated with each subnets.
- Tracking every active staker, validators and delegators, associated with each elastic subnet and the Primary Network.
- Measuring uptimes of each staker associated with the Primary Network and with elastic subnets, in order to decide whether they should be rewarded or not at the end of their lifetime.
- Hosting on-chain voting on elastic subnets rewarding, via Proposal Blocks.
- Exposing the validators set of any elastic subnets at a past height, in order to support [Warp protocol](https://medium.com/avalancheavax/avalanche-warp-messaging-awm-launches-with-the-first-native-subnet-to-subnet-message-on-avalanche-c0ceec32144a).

## Technical content

Here is a list of technical details about the way `PlatformVM` works:

- `ChainTime` helps tracking stakers' uptime and rewarding them. You can read [here](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/docs/chain_time.md) details of how P-chain mananges it.
- Validators are versioned to support the [Warp protocol](https://medium.com/avalancheavax/avalanche-warp-messaging-awm-launches-with-the-first-native-subnet-to-subnet-message-on-avalanche-c0ceec32144a). You can read [here](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/docs/validators_versioning.md) details of how the P-chain stores validator set versions.
- Subnets lifetime is tracked and managed in the P-chain. You can read [here](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/docs/subnets.md) details of what transactions can be used to affect a subnet.
- The P-chain can process different kind of blocks. You can read [here](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/docs/block_formation_logic.md) details of how blocks have changed across forks.
