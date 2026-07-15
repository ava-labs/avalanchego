# `blocklimit` — bounding block size to the P2P message limit

## Why this exists

A SAE block is gossiped as a single P2P message, capped at
`constants.DefaultMaxMessageSize` (2 MiB). But the block builder bounds blocks
by **gas**, not **bytes** — and the two diverge: the cheapest byte is a calldata
zero byte at `TxDataZeroGas = 4` gas/byte, so a block built to the block gas
limit `x` — `80M` at Helicon launch (ACP-176 sets the per-block limit at target
× 20, and the initial target is 4M) — could reach `80M / 4 ≈ 20 MB`, ~10× the
message limit. Such a block can't be gossiped.

This matters beyond a single wasted block. Unlike the P/X-chains, **SAE does not
remove transactions from the mempool while building**, so a node that builds an
oversized block and cannot gossip it would rebuild the *same* block from the
same pending transactions rather than making progress. Bounding block size by
bytes — not just gas — is therefore needed for liveness, not only efficiency.
(Earlier EVM block builders kept blocks gossipable by skipping over-large txs at
build time; SAE can do better because it knows each tx's gas limit up front and
gates on the gas-to-byte ratio directly.)

## The rule

A transaction is eligible only if its **byte share does not exceed its gas
share**:

$$\text{accept if } \quad \frac{y}{M} \le \frac{g}{x} \quad \iff \quad y x \le g M$$

where `M` = max message size, `x` = block gas limit, `g` = tx gas limit, `y` =
tx serialized size — i.e. it must carry at least `x/M ≈ 38.15` gas per byte (at
the `80M` Helicon-launch limit and `M = 2 MiB`). [`Eligible`](blocklimit.go)
evaluates this with exact 128-bit integer math.

The *ratio* form (not a flat byte cap) is what composes: if every included tx
satisfies $y_i/M \le g_i/x$, then

$$\sum_i \frac{y_i}{M} \le \sum_i \frac{g_i}{x} \le 1$$

so the block fits in `M` — **provided the `x` in the per-tx check matches the `x`
bounding the block**, a
condition the build-time backstop below protects.

## Why the threshold is acceptable

`38.15` gas/byte sits *above* even the 16 gas/nonzero-byte intrinsic cost, so it
rejects any calldata-dominated tx with modest execution — raising the worry that
it blocks legitimate traffic. Analysis of **all C-Chain EVM traffic for May 2026
(82,577,809 txs)** shows it does not:

- **3,724 txs (0.0045%, ~1 in 22,000) would be rejected.** The median tx sits at
  ~362 gas/byte, **~9.5× above the threshold**, so the rule almost never bites.
- The rejected set is **legitimate, concentrated infrastructure** (DEX
  aggregators, batch order-cancels, data-availability payloads with zero-padded
  calldata), not spam — and rejection is **soft**: raising the tx's gas limit
  makes it eligible.
- The absolute 2 MiB ceiling is never the binding constraint: the largest tx
  all month was ~126 KB (~16× under `M`). The **gas/byte ratio**, not size, is
  what does the rejecting.

<p>
  <img src="heuristic-scatter.png" width="420" alt="Size vs gas with the y = g·M/x rejection boundary; nearly all txs sit well above the line">
  <img src="gas-per-byte-distribution.png" width="420" alt="Gas-per-byte distribution: EVM vs atomic txs against the 38.1 cutoff">
</p>

The trade-off — blocking a vanishingly small, self-remediable slice of
legitimate traffic to keep blocks gossipable — is worth it.

## Why atomic transactions are out of scope

Atomic cross-chain txs (`ImportTx`/`ExportTx`) are byte-heavy and gas-light by
construction (~36 gas/byte — see the red band in the figure, just left of the
cutoff), so this rule would reject ~100% of them. They are gated out and handled
separately. **Any future use of `Eligible` must only be reached by EVM txs.**

## How it is enforced — two layers

1. **Mempool admission** ([`txgossip`](../txgossip/txgossip.go)) — rejects
   ineligible txs on the gossip and RPC paths against the **live** block gas
   limit (`worstcase.SafeMaxBlockSize`). This carries the bulk of the protection.
2. **Build-time backstop** ([`block_builder.go`](../sae/block_builder.go)) —
   caps total serialized tx bytes at `maxBlockTxBytes = SafeMaxBytes −
   blockByteOverhead`, counting each tx's RLP framing.

Both are needed because `x` is **dynamic** (ACP-176). A tx admitted under one
`x` may be built into a block under a larger `x`, breaking the admission-time
$\sum y \le M$ guarantee — so the absolute byte cap is what actually guarantees the
produced block is gossipable. `blockByteOverhead` (`staking.MaxCertificateLen +
6 KiB`) reserves headroom for the ProposerVM certificate, header, signature, and
framing; the per-tx rule ignores this overhead (uses full `M`) for simplicity,
and the build-time cap is the one place it is reserved.

## Assumptions that would justify revisiting

- **`x = 80M`** is the Helicon-launch limit (live value from
  `worstcase.SafeMaxBlockSize`). A material change in the gas target shifts the
  threshold `x/M`; re-run the rejection-rate analysis before assuming the rule
  stays this benign.
- **`M = 2 MiB`** tracks `constants.DefaultMaxMessageSize` via `SafeMaxBytes`; if
  it changes, re-check `blockByteOverhead` against ProposerVM framing.
- **Traffic composition.** The 0.0045% figure was measured on traffic with no
  access lists. The rule uses the true `types.Transaction.Size()`, so it stays
  *correct* for any composition, but a future access-list- or calldata-heavy
  workload could make it bite more often than history suggests.

## References

- [`blocklimit.go`](./blocklimit.go) — `Eligible`, `SafeMaxBytes`
- [`txgossip/txgossip.go`](../txgossip/txgossip.go) — mempool admission filter
- [`sae/block_builder.go`](../sae/block_builder.go) — `maxBlockTxBytes`,
  `blockByteOverhead` backstop
- [`worstcase/state.go`](../worstcase/state.go) — `SafeMaxBlockSize` (the live `x`)
- [ACP-194 — Streaming Asynchronous Execution](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution)
- [ACP-176 — dynamic EVM gas limit and price discovery](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates)
- [Issue #5554](https://github.com/ava-labs/avalanchego/issues/5554),
  [PR #5570](https://github.com/ava-labs/avalanchego/pull/5570)
