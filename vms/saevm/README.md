# saevm

`saevm` is the reference implementation of Streaming Asynchronous Execution (SAE) of EVM blocks, as described in [ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution).
It is under active development and there are currently no guarantees about the stability of its Go APIs.

## Documentation

- [Invariants](./docs/invariants.md) - in-memory/on-disk state equivalence and timing guarantees
- [Block size limit](./txgossip/README.md) - why blocks are bounded to the P2P message size, and the gas-to-byte eligibility rule that enforces it