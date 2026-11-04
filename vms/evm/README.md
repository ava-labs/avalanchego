# saevm

`saevm` is the reference implementation of Continuous Execution of EVM blocks, as described in [ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution).
Continuous Execution was originally called Streaming Asynchronous Execution (SAE), and the packages and this document still use that name.

The C-Chain is built on `saevm`; see the [C-Chain README](./cchain/README.md).

## Table of contents

- [Background](#background)
- [Architecture](#architecture)
  - [The VM](#the-vm)
  - [Block lifecycle](#block-lifecycle)
  - [Block building](#block-building)
  - [Execution](#execution)
  - [Gas as a clock](#gas-as-a-clock)
  - [State and storage](#state-and-storage)

## Background

> **TODO:** Recap ACP-194: why execution is decoupled from consensus, what "streaming" and "asynchronous" mean here, and what this buys over a chain that executes blocks synchronously.

## Architecture

> **TODO:** Big-picture walkthrough of how the pieces fit together, with diagrams where possible. Tracing a single block through build -> verify -> accept -> execute -> settle, noting which component acts at each step, is probably the clearest approach.

The subsections below are organized by concept, not by package. Package-level documentation lives with each package.

### The VM

> **TODO:** The VM itself (`sae`): block building, P2P, RPC/HTTP APIs, health checks, recovery, and the adaptor to consensus (`adaptor`). Cover both the SAE lifecycle hooks (`hook`) and the libevm hooks; they are similar and tightly coupled, so make clear how they differ and when each is used.

### Block lifecycle

> **TODO:** The phases a block goes through (built -> verified -> accepted -> executed -> settled), what triggers each transition, and what is recorded along the way (worst-case bounds, interim and final gas times, execution results).

See [Invariants](./docs/invariants.md) for the timing guarantees between these phases and their on-disk artefacts.

### Block building

> **TODO:** Worst-case balance, nonce, and gas-price tracking at build time (`worstcase`): why the builder may only include transactions guaranteed to remain valid at execution time, and how what is built differs from what is executed. Also the mempool, and gas-price stats and fee suggestions (`gasprice`).

### Execution

> **TODO:** Where blocks are executed (`saexec`): the queue of accepted blocks, running transactions on the parent's post-execution state, checking the worst-case bounds recorded at build time, and storing the results.

### Gas as a clock

> **TODO:** How gas tracks consumption above target as a clock, and how blocks use interim and final gas times (`gastime`, built on the generic proxy-unit clock in `proxytime`).

### State and storage

> **TODO:** Storing and accessing SAE data (`saedb`): when the trie is committed, opening state at a root, and how Firewood is wired in as the state backend.

See the [Firewood README](./firewood/README.md).
