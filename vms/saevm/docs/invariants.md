# Invariants

## Goals

This document deals with the in-memory state of a running node, the on-disk persistence of a chain, and how they relate to one another with respect to data equivalence and timing.
The EVM-specific concept of state (e.g. accounts) is orthogonal to the topics in this document.

1. Data equivalence details how in-memory objects can be recovered from disk by APIs or after a node restarts.

2. Timing details the ordering guarantees that exist between, for example, in-memory objects and their equivalents on disk.

## Guiding principles

1. Wherever possible, reuse Ethereum-native concepts i.f.f. there is a clear rationale for equivalence.
   1. Conversely, introducing a new concept is preferred to ambiguous overloading of an existing one.

2. Timing guarantees SHOULD be sufficient to avoid race conditions if relied upon and, if insufficient, MUST be documented as such.

## Data equivalence

### Height mapping

#### Background

The `rawdb` package allows for arbitrary blocks to be stored, only coupling height and hash for canonical blocks; i.e. those in the consensus-agreed chain history.
The canonical block with the greatest height is typically stored as the "head".


Ethereum consensus has delayed finality and `rawdb` therefore caters for storage of the last-finalized block.
While it also has a notion of a "safe" block (with reduced probability of re-org), there is no equivalent support in `rawdb`.

#### Mapping

| SAE      | `rawdb`   |
| -------- | --------- |
| Accepted | Canonical |
| Executed | Head      |
| Settled  | Finalized |

#### Rationale

* Accepted and canonical are effectively identical concepts, save for nuanced differences between the consensus mechanisms.

* From an API perspective, we treat "latest" (i.e. the chain head) as the last-executed block.
Mirroring this on disk allows for simple integration with the upstream API implementations.

* While SAE does _not_ delay finality, it does adopt a notion akin to being safe (although from disk corruption) as of the last-settled block. To maintain (a) support for all API block labels; and (b) monotonicity of said labels; the SAE spec defines both the "safe" and "final" labels as the last-settled block. Although this slightly alters the definitions, it does not overload concepts so is in keeping with the *Guiding principles*.

> [!NOTE]
> These also provide an unambiguous inverse, allowing for recovery from disk.

## Timing

### Ordering guarantees

The goal of these guarantees is to avoid race conditions.
These guarantees are not concerned with low-level races typically protected against with locks and atomics, but with the sort that arise due to the interplay of system components like consensus, the execution stream, and APIs.

> [!NOTE]
> This document is the source of truth and if the code differs there is a bug until proven otherwise.
> If it is impossible to correct the bug then this document MUST be updated to reflect reality, and an audit of potential race conditions SHOULD be performed.

#### Definitions

|             | |
| ----------- | -
| $b_n$      | Block at height $n$
| $A$        | Set of Accepted blocks
| $E$        | Set of Executed blocks
| $S$        | Set of Settled blocks
| $\Sigma_n$ | Set of blocks settled if and when $b_n$ is accepted (MAY be empty)
| $C$        | Arbitrary condition; e.g. $b_n \in E$
| $D(C)$     | Disk artefacts of some $C$
| $M(C)$     | Memory artefacts of some $C$
| $I(C)$     | Internal indicator of some $C$
| $X(C)$     | eXternal indicator of some $C$
| $G \implies P$ | Some condition $G$ guarantees another condition $P$

SAE tracks state as both disk and memory artefacts.
Disk artefacts are typically the same as those stored by regular `geth` nodes.
Examples of memory artefacts include block ancestry, receipts, and post-execution state roots.

An internal indicator is any signal of a change in state that can only be accessed by the SAE implementation.
Such signals are intended to act as gating mechanisms before accessing memory artefacts.
Examples include the last-accepted, -executed, and -settled block pointers, and unblocking of `Block.WaitUntil{Executed,Settled}()` methods without error.

An external indicator is any signal of a change in state that can be accessed outside of the SAE implementation, even if in the same process.
An example is a chain-head subscription, be it in the same binary or over a websocket API.

#### Guarantees

> [!TIP]
> Guarantees should be thought of as "if I witness the guarantor $G$ then I can assume some prerequisite $P$".
> This must not be confused with cause and effect as it is the *temporal inverse*: if $G$ guarantees $P$ then $t_P < t_G$ i.e. $P$ *happens before* $G$ in Golang terminology.

| Guarantor      | Prerequisite        | Notes |
| -------------- | ------------------- | ----- |
| $b_n \in S$    | $b_n \in E$         | Settlement after execution
| $b_n \in E$    | $b_n \in A$         | Execution after acceptance
| $X(C)$         | $I(C)$              | External indicator after internal indicator
| $I(C)$         | $M(C)$              | Internal indicator after memory
| $M(C)$         | $D(C)$              | Memory after disk
| $D(C)$         | $C$                 | Disk after condition
| $f(C_{b_n})$   | $f(C_{b_{n-1}})$    | See (1) below
| $f(b_n \in A)$ | $f'(\sigma \in S) \quad\forall \sigma\in\Sigma_n $ | See (2) below

1. Realisation of some side effect $f(\cdot)$ of some condition $C_{b_n}$ of a block MUST occur after the *same* side effect of the parent block. See Example (3).

2. Realisation of some side effect of acceptance of a block MUST occur after the _equivalent_ side effect of all blocks settled by that acceptance. See Example (4).

> [!NOTE]
> Although by definition $b_n \in A$ i.f.f. $\Sigma_n \subset S$, in practice it may not be possible to realise side effects atomically.
> The chosen ordering of settlement then acceptance is for practical reasons as code with access to $b_n$ can typically access $\Sigma_n$ but not vice versa, so inverting the order would provide zero benefit.

#### Polling vs Broadcast

Note that no guarantees exist _within_ a class of side effects with respect to the _same_ block.
For example, `ChainEvent` and `ChainHeadEvent` are both $X(b_n \in E)$ and MAY therefore be sent in any order for a given block.

However indicators can generally be separated into those that are polled by consumers (e.g. querying a last-executed atomic pointer) and those for which consumers receive a broadcast (e.g. unblocking `blocks.Block.WaitUntilExecuted()`).
The implementation SHOULD realise polling side effects before sending respective broadcasts; i.e. "atomics before channel closes".
At the time of writing, this is only guaranteed for `WaitUntilExecuted()` and `WaitUntilSettled()`, which both block until the respective atomic pointers have already been updated.

> [!NOTE]
> This heuristic does not provide any guarantees of the order of atomic updates nor that the set is itself updated atomically.

#### Examples

1. If `blocks.Block.Executed()` returns `true` then the block's receipts can be read from the database because $M(b_n \in E) \implies D(b_n \in E)$.

2. If the atomic pointer to the last-executed block (an internal indicator) is at height $k \ge n$ then the post-execution state of block $n$ can be opened because $I(b_k \in E) \implies M(b_k \in E) \implies M(b_n \in E)$.

3. All $\sigma \in \Sigma_n$ MUST have their `Block.MarkSettled()` methods called _in order_ as this satisfies $M(b_n \in S) \implies M(b_{n-1} \in S)$.
Importantly, updating the last-settled pointer, $I(\sigma \in S)$, MAY be delayed because this is a different side effect and doing so would not violate any other guarantees.

4. Persisting the canonical block hash for height $n$, i.e. $D(b_n \in A)$, MUST occur after persisting the `rawdb` "finalized" block hash, i.e. $D(\sigma \in S)$.

5. If `blocks.Block.WaitUntilExecuted()` (broadcast) returns with `nil` error then `saexec.Executor.LastExecuted()` (polling) will return a block at the same or greater height.
