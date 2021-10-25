# Release Notes

## v1.6.4

This version is backwards compatible to [v1.6.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0). It is optional, but encouraged.

### Networking

- Updated peerlist gossiping to use the same mechanism as other gossip calls.

### Metrics

- Updated `avalanche_{ChainID}_handler_gossip_{count,sum}` to `avalanche_{ChainID}_handler_gossip_request_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_accepted_{count,sum}` to `avalanche_{ChainID}_lat_accepted_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_accepted_frontier_{count,sum}` to `avalanche_{ChainID}_lat_accepted_frontier_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_ancestors_{count,sum}` to `avalanche_{ChainID}_lat_multi_put_{count,sum}`.
- Combined `avalanche_{ChainID}_lat_pull_query_{count,sum}` and `avalanche_{ChainID}_lat_push_query_{count,sum}` to `avalanche_{ChainID}_lat_chits_{count,sum}`.
- Added `avalanche_{ChainID}_app_response_{count,sum}`.

### Indexer

- Added method `GetContainerByID` to client implementation.

### Subnet & Custom VMs

- Ensured that all possible chains are run in `--staking-enabled=false` networks.

---

## v1.6.3

This version is backwards compatible to [v1.6.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0). It is optional, but encouraged.

### Config Options

- Updated the default value of `--inbound-connection-throttling-max-conns-per-sec` to `256`.
- Updated the default value of `--meter-vms-enabled` to `true`.
- Updated the default value of `--staking-disabled-weight` to `100`.

### Metrics

- Changed the behavior of `avalanche_network_buffer_throttler_inbound_awaiting_acquire` to only increment if the message is actually blocking.
- Changed the behavior of `avalanche_network_byte_throttler_inbound_awaiting_acquire` to only increment if the message is actually blocking.
- Added `Block/Tx` metrics on `meterVM`s.
  - Added `avalanche_{ChainID}_vm_metervm_build_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_parse_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_get_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_verify_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_verify_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_accept_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_reject_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_parse_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_get_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_verify_tx_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_verify_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_accept_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_reject_{count,sum}`.

### Coreth

- Applied callTracer fault handling fix.
- Initialized multicoin functions in the runtime environment.

### ProposerVM

- Updated block `Delay` in `--staking-enabled=false` networks to be `0`.
