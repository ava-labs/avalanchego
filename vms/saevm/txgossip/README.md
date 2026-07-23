# `txgossip`

Package `txgossip` provides SAE's mempool. It couples a `txpool.TxPool` with a
`gossip.BloomSet` so that transactions can be exchanged over AvalancheGo's p2p
gossip machinery.

TODO: document the rest of the package.
## Bounding blocks to the p2p message size limit

### Why a rule is needed

An SAE block is gossiped as a single p2p message, which is capped at
`constants.DefaultMaxMessageSize` (2 MiB). The block builder, however, bounds
blocks by gas rather than bytes, and gas is a poor proxy for size. The cheapest
byte is a zero calldata byte at 4 gas (`TxDataZeroGas`). The block gas limit at
Helicon launch is 80M, since ACP-176 sets the per-block limit at 20 times the
target and the initial target is 4M. A block filled with zero calldata could
therefore reach 80M / 4 ≈ 20 MB, roughly ten times the message limit, and
thus not be gossipable.

A block that cannot be gossiped halts the chain: unlike the P/X-chains, SAE
does not remove transactions from the mempool while building, so a node that
builds an oversized block would rebuild the same block from the same pending
transactions rather than making progress. Every block MUST fit in a p2p message 
for the chain to remain live, so block size has to be bounded in bytes and not 
only in gas.

Earlier EVM block builders kept blocks gossipable by skipping over-large
transactions at build time. SAE can do better: it knows each transaction's gas
limit up front, so it gates on the gas-to-byte ratio directly.

### The rule

A transaction is eligible only if its byte share of a block does not exceed
its gas share:

$$\text{accept if } \quad \frac{y}{M} \le \frac{g}{x} \quad \iff \quad y x \le g M$$

where `M` is the block's transaction byte budget (`saeparams.TargetBlockBytes`,
512 KiB below the max message size), `x` is the block gas limit, `g` is the
transaction's gas limit, and `y` is its serialized size. Equivalently, a
transaction must carry at least `x/M` gas per byte, which is ≈ 50.9 at the
Helicon-launch gas limit. [`eligible`](./txgossip.go) evaluates the rule with
exact 128-bit integer math.

The rule ties a transaction's bytes to its gas so that the block's gas limit
also acts as a byte limit. The builder already enforces $\sum_i g_i \le x$, so
if every included transaction satisfies $y_i/M \le g_i/x$, then

$$\sum_i \frac{y_i}{M} \le \sum_i \frac{g_i}{x} \le 1$$

and a full block's worth of transactions fits in `M`, with the margin above
`M` covering every non-transaction byte on the wire. A flat per-transaction
byte cap has no such property: gas is the only limit on how many transactions
a block holds (~3,800 minimum-gas transactions at `x` = 80M), so a flat cap
small enough to bound the block would be around 400 bytes and reject nearly
all traffic. This guarantee holds provided the `x` used in each
per-transaction check matches the `x` bounding the block, a condition the
build-time backstop below protects.

### Impact on real traffic

The 50.9 gas/byte threshold exceeds even the 16 gas charged per nonzero
calldata byte. Reaching it takes execution gas, so the rule rejects
transactions that carry a lot of calldata but compute little with it.

To measure how many transactions could be rejected, every C-Chain transaction 
from May 2026 (82,577,809 in total) was checked against the rule at the 
Helicon-launch parameters (`x` = 80M, `M` = 1.5 MiB). The C-Chain's own gas 
rules play no part in the check; its history simply provides a realistic
population of transactions.

- 25,489 transactions (0.0309%, about 1 in 3,240) would have been rejected.
  The median transaction carries ~362 gas/byte, about 7 times the threshold,
  so the rule almost never bites.
- The rejected set is calldata-dominated traffic, byte-heavy relative to the
  gas it buys, which is exactly what the rule is meant to gate.
- The gas/byte ratio, not absolute size, does all the rejecting: the largest
  transaction all month was ~123 KiB, about 12 times under `M`.

Rejection is also recoverable: resubmitting the same transaction with a
higher gas limit makes it eligible.

<p>
  <img src="heuristic-scatter.png" width="420" alt="Size vs gas with the y = g·M/x rejection boundary; nearly all txs sit well above the line">
</p>

### Why atomic transactions are out of scope

Atomic cross-chain transactions (`ImportTx`/`ExportTx`) are byte-heavy and
gas-light by construction, at roughly 36 gas/byte. The rule would reject
essentially all of them, so they are gated out and handled separately. Any
future use of `eligible` MUST only be reached by EVM transactions.

### Enforcement

The bound is enforced at three layers:

1. Mempool admission ([`txgossip.go`](./txgossip.go)): ineligible transactions
   are rejected on both the gossip and RPC paths, against the live block gas
   limit from `worstcase.SafeMaxBlockSize`. This carries the bulk of the
   protection.
2. Build-time backstop ([`sae/block_builder.go`](../sae/block_builder.go)):
   the builder caps a block's cumulative serialized transaction bytes at the
   same `saeparams.TargetBlockBytes`.
3. Verify-time cap ([`sae/blocks.go`](../sae/blocks.go)): verification rejects
   any block whose serialization exceeds `saeparams.MaxBlockBytes`, 256 KiB
   below the message size. This covers self-built blocks as well as blocks
   received from peers.

The backstop exists because `x` is dynamic under ACP-176. A transaction
admitted under one `x` may be built into a block under a larger `x`, breaking
the admission-time $\sum y \le M$ guarantee, so the absolute byte cap is what
actually guarantees a produced block is gossipable. The flat margins below the
message size (512 KiB for transaction bytes, 256 KiB for the whole block)
reserve headroom for the block header, hook-injected payloads, RLP framing,
the ProposerVM header, and message framing, without itemizing their worst-case
sizes.

### Assumptions that would justify revisiting

- `x` = 80M is the Helicon-launch limit; the live value comes from
  `worstcase.SafeMaxBlockSize`. A material change in the gas target shifts the
  threshold `x/M`. Re-run the rejection-rate analysis before assuming the rule
  stays this benign.
- `M` = 1.5 MiB is `saeparams.TargetBlockBytes`, a literal deliberately not
  derived from `constants.DefaultMaxMessageSize` (2 MiB), so that a change to
  the message size cannot silently move this consensus-relevant bound. A
  compile-time assertion in [`sae/blocks.go`](../sae/blocks.go) fails the
  build if the message size ever drops to `saeparams.MaxBlockBytes` or below.
- The 0.0309% figure was measured on traffic with no access lists. The rule
  uses the true `types.Transaction.Size()`, so it stays correct for any
  traffic composition, but a future access-list- or calldata-heavy workload
  could make the rule bite more often than history suggests.
