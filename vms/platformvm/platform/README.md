# Platform

The `platform` package defines the canonical types of the P-Chain protocol and
the invariants that are intrinsic to those types. Behavior on these types have
the same meaning regardless of the context in which they are used.

This package defines P-Chain protocol concepts such as:

- Transactions
- Blocks
- Staking primitives (e.g validators and delegators).

A type or behavior belongs in this package when it is part of the P-Chain
protocol itself and is meaningful independent of a particular consumer.
Context-specific behavior belongs in the context that owns it. For example,
state persistence and indexing belong in the state layer, and state transitions
belong in the execution layer.

Consumers should adapt the canonical types defined here into context-specific
representations, but should not redefine their protocol-level meaning or invariants.
