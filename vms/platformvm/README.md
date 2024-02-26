# Platform VM

## Technical content

Here is a list of technical details about the way `platformvm` works:

- `ChainTime` helps tracking stakers' uptime and rewarding them. You can read [here](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/docs/chain_time.md) details of how P-chain mananges it.
- Validators are versioned to support BLS verification. You can read [here](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/docs/validators_versioning.md) details of how the P-chain stores validator set versions.
- Subnets lifetime is tracked and managed in the P-chain. You can read [here](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/docs/subnets.md) details of what transactions can be used to affect a subnet.
- The P-chain can process different kind of blocks. You can read [here](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/docs/block_formation_logic.md) details of how blocks have changed across forks.
