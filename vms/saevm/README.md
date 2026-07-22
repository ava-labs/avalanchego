# saevm

`saevm` is the reference implementation of Continuous Execution of EVM blocks, as described in [ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution).
Continuous Execution was formerly known as Streaming Asynchronous Execution (SAE), the name still used by the packages and throughout this documentation.

## Table of contents

- [Background](#background)
- [Architecture](#architecture)
- [Packages](#packages)
  - Core VM: [`sae`](#sae), [`adaptor`](#adaptor), [`blocks`](#blocks), [`hook`](#hook)
  - Block building: [`worstcase`](#worstcase), [`txgossip`](#txgossip), [`gasprice`](#gasprice)
  - Execution: [`saexec`](#saexec), [`gastime`](#gastime), [`proxytime`](#proxytime)
  - State and storage: [`saedb`](#saedb), [`firewood`](#firewood)
  - Chain integration: [`cchain`](#cchain)
  - Shared utilities: [`types`](#types), [`params`](#params), [`saetest`](#saetest), [`cmputils`](#cmputils)
- [References](#references)

## Background

> **TODO:** Quick ACP-194 recap, why execution is decoupled from consensus, what "streaming" and "asynchronous" actually mean here, and the benefits this gives us over chains and the standard synchornous sturcture. We're the best!

## Architecture

> **TODO:** Big-picture walkthrough (include diagrams where possible) of how everything fits together. Idea that's probably easiest to trace a single block through build -> verify -> accept -> execute -> settle and call out what does what at each step.

## Packages

### `sae`

> **TODO:** The VM itself: block building, consensus glue, P2P, RPC/HTTP APIs, health checks, recovery.

### `adaptor`

> **TODO:** How the VM works w/ consensus.

### `blocks`

> **TODO:** The phases a block goes through (built -> verified -> accepted -> executed -> settled), what triggers each transition, and what gets recorded along the way (worst-case bounds, interim/final gas time, execution results).

See [Invariants](./docs/invariants.md) for the timing guarantees among these phases and their on-disk artefacts.

### `hook`

> **TODO:** The points in a block's lifecycle (validation, execution, build/rebuild) where chain-specific behavior gets injected, e.g. `cchain`.

### `worstcase`

> **TODO:** Build-time worst-case balance/nonce/gas-price tracking. Why the builder can only include transactions that are guaranteed to still be valid at execution time, and how this predictive step differs from what `saexec` actually executes.

### `txgossip`

> **TODO:** The SAE transaction mempool and how it plugs into AvalancheGo's push/pull gossip.

### `gasprice`

> **TODO:** Gas price stats and fee suggestions for transaction inclusion.

### `saexec`

> **TODO:** Where blocks actually get executed. Queue of accepted blocks, running transactions off the parent's post-execution state, checking the worst-case bounds recorded at build time, and storing the results.

### `gastime`

> **TODO:** Gas as a clock tracking excess consumption above target, and how blocks use interim vs. final gas times.

### `proxytime`

> **TODO:** The generic proxy-unit clock that `gastime` uses.

### `saedb`

> **TODO:** Storage and access for SAE data — when we commit the trie, opening state at a root, etc. 

### `firewood`

> **TODO:** How Firewood is wired in as the state backend.

See the [Firewood README](./firewood/README.md).

### `cchain`

> **TODO:** The C-Chain VM built on top of `sae.VM` — hooks, cross-chain transactions, Warp messaging, validator-voted parameters.

See the [C-Chain README](./cchain/README.md), its [configuration reference](./cchain/config.md), and the [Warp README](./cchain/warp/README.md).

### `types`

> **TODO:** Shared types -- not sure how much there is to say here

### `params`

> **TODO:**  `Lambda`, `Tau`, execution-queue limits, etc. This'll be short.

### `saetest`

> **TODO:** Test helpers for SAE.

### `cmputils`

> **TODO:** `cmp` helpers for equality comparisons in tests.

## References

- [ACP-194: Continuous Execution](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution) — the spec this VM implements
- [ACP-176: Dynamic EVM Gas Limit and Price Discovery Updates](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates) — the gas mechanism that `gastime` generalizes
- [ACP-226: Dynamic Minimum Block Times](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/226-dynamic-minimum-block-times)
- [ACP-283: Dynamic Minimum Gas Price](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/283-dynamic-minimum-gas-price/README.md)
- [ACP-118: Warp Signature Request](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/118-warp-signature-request) — used by Warp messaging in `cchain/warp`
- [Firewood](https://github.com/ava-labs/firewood) — the database backing the `firewood` package

