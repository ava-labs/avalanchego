# saevm

`saevm` is the reference implementation of Continuous Execution of EVM blocks, as described in [ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution).
Continuous Execution was formerly known as Streaming Asynchronous Execution (SAE), the name still used by the packages and throughout this documentation.

## Table of contents

- [Background](#background)
- [Architecture](#architecture)
  - [The VM](#the-vm)
  - [Block lifecycle](#block-lifecycle)
  - [Block building](#block-building)
  - [Execution](#execution)
  - [Gas as a clock](#gas-as-a-clock)
  - [State and storage](#state-and-storage)
  - [The C-Chain](#the-c-chain)
- [References](#references)

## Background

> **TODO:** Quick ACP-194 recap, why execution is decoupled from consensus, what "streaming" and "asynchronous" actually mean here, and the benefits this gives us over chains with the standard synchronous structure. We're the best!

## Architecture

> **TODO:** Big-picture walkthrough (include diagrams where possible) of how everything fits together. Idea that's probably easiest to trace a single block through build -> verify -> accept -> execute -> settle and call out what does what at each step.

This section is organized by concept rather than by package — detailed package-level documentation lives with the packages themselves.

### The VM

> **TODO:** The VM itself (`sae`): block building, consensus glue, P2P, RPC/HTTP APIs, health checks, recovery, and how it works w/ consensus (`adaptor`). Cover both the SAE lifecycle hooks (`hook`) AND the libevm hooks — they're similar and tightly coupled, so make the differences and uses clear.

### Block lifecycle

> **TODO:** The phases a block goes through (built -> verified -> accepted -> executed -> settled), what triggers each transition, and what gets recorded along the way (worst-case bounds, interim/final gas time, execution results).

See [Invariants](./docs/invariants.md) for the timing guarantees among these phases and their on-disk artefacts.

### Block building

> **TODO:** Build-time worst-case balance/nonce/gas-price tracking (`worstcase`). Why the builder can only include transactions that are guaranteed to still be valid at execution time, and how this  step differs from what actually gets executed. Also the SAE transaction mempool and gas price stats and fee suggestions for transaction inclusion (`gasprice`).

### Execution

> **TODO:** Where blocks actually get executed (`saexec`). Queue of accepted blocks, running transactions off the parent's post-execution state, checking the worst-case bounds recorded at build time, and storing the results.

### Gas as a clock

> **TODO:** Gas as a clock tracking excess consumption above target, and how blocks use interim vs. final gas times (`gastime`, built on the generic proxy-unit clock in `proxytime`).

### State and storage

> **TODO:** Storage and access for SAE data (`saedb`) — when we commit the trie, opening state at a root, and how Firewood is wired in as the state backend.

See the [Firewood README](./firewood/README.md).

### The C-Chain

> **TODO:** The C-Chain VM built on top of `sae.VM` (`cchain`) — hooks, cross-chain transactions, Warp messaging, validator-voted parameters.

See the [C-Chain README](./cchain/README.md), its [configuration reference](./cchain/config.md), and the [Warp README](./cchain/warp/README.md).

## References

- [ACP-194: Continuous Execution](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution) — the spec this VM implements
- [ACP-176: Dynamic EVM Gas Limit and Price Discovery Updates](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates) — the gas mechanism that `gastime` generalizes
- [ACP-226: Dynamic Minimum Block Times](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/226-dynamic-minimum-block-times)
- [ACP-283: Dynamic Minimum Gas Price](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/283-dynamic-minimum-gas-price/README.md)
- [ACP-118: Warp Signature Request](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/118-warp-signature-request) — used by Warp messaging in `cchain/warp`
- [Firewood](https://github.com/ava-labs/firewood) — the database backing the `firewood` package
