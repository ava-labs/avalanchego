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

> **TODO:** The VM itself (`sae`): block building, P2P, RPC/HTTP APIs, health checks, recovery, and the adaptor to consensus (`adaptor`).

#### Hooks

Chains customize SAE through two sets of hooks:

- **SAE hooks** (`hook`) run at fixed points in a block's lifecycle.
- **libevm hooks** (`params.RulesHooks` and `vm.Hooks`) run inside the EVM.
  - Almost all of them run only during execution.
  - `AccessListGas` also runs while building, because intrinsic gas is checked then.

Every hook runs at one of five points, and reads only what that point provides.

- **Setup**
  - Runs once, when the VM starts.
  - Hooks: `ExecutionResultsDB`.
  - Reads its data directory.
- **Consensus**
  - Runs while building a block, and again while verifying it by rebuilding.
  - Hooks: `BuildHeader`, `StampHeader`, `PotentialEndOfBlockOps`, `BuildBlock`, `BlockRebuilderFrom`, `AdmitTransaction` and libevm's `AccessListGas`.
  - Reads the parent header and the P-Chain context.
  - `StampHeader` and `AdmitTransaction` also read the settled view: the settled block's header, rules and read-only post-state.
- **Pure**
  - Runs at any time.
  - Hooks: `BlockTime`, `GasConfigAfter`, `SettledBy`, `EndOfBlockOps` and `VerifyBlockSyntax`.
  - Reads only the header or block it is given.
- **Execution**
  - Runs during canonical execution, and during historical re-execution for RPC and tracing.
  - Hooks: `StartExecutingBlock`, `FinishExecutingBlock` and the other libevm hooks.
  - Reads the parent's post-state and the block.
- **Post-execution**
  - Runs only during canonical execution, before the state is committed.
  - Hooks: `AfterExecutingBlock`.
  - Reads the block and its receipts.

Consensus accepts a block before it executes, so implementers MUST follow these rules.

1. **Execution never rejects a transaction.**
   - A transaction error during execution stops every node.
   - Reject transactions in `AdmitTransaction` instead.
   - The chain's libevm `CanExecuteTransaction` MUST allow every transaction.
   - On the C-Chain, Granite reverts delegate calls into stateful precompiles before they can invalidate a transaction. The synchronous VM never accepted a block with an invalidated transaction.
2. **Start and finish hooks never fail.**
   - An error from `StartExecutingBlock` or `FinishExecutingBlock` stops execution.
   - Put every check that can fail in config parsing or block verification.
3. **Execution hooks share no per-block state.**
   - One hooks instance serves canonical execution and historical re-execution at the same time.
   - Re-execution that stops mid-block runs `StartExecutingBlock` without `FinishExecutingBlock`, so neither may rely on the other.
4. **Admission lags module changes.**
   - `AdmitTransaction` sees modules as of the settled block.
   - A module activation or role change reaches admission only once its block settles, at least τ later.
5. **Header values derived from state are stamped.**
   - `StampHeader` MUST write anything that `GasConfigAfter`, `BlockTime`, `SettledBy` or execution reads from module state.
   - It runs before worst-case accounting copies the header.
   - After `BuildHeader`, no hook may change the parent hash, number, time or block time.
   - After `StampHeader`, no hook may change stamped values or the values SAE sets.
   - `hookstest.Conformance` checks this in tests.
6. **Only an account's own transactions debit it.**
   - Nothing else may lower an externally owned account's balance, change its nonce or give it code.
   - Worst-case accounting and the replay of unsettled blocks rely on this to bound balances and nonces.

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
