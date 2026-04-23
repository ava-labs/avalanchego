# Multi-party stake — P-chain tx mechanics

This directory is the pure-CLI pipeline for the same flow the server exposes over HTTP. Six steps; no network/UI layer. Every step operates on hex blobs on disk.

Roles:
- **Coordinator** runs `prepare`, `prepare-split`, and both `submit*`. Never touches a private key.
- **Each staker** runs `sign` and `sign-split` on their own machine with their key. Emits a partial-signature JSON file.

---

## Step 1 — `prepare.go` → unsigned `AddPermissionlessValidatorTx`

**Inputs:** `InputConfig` (validator NodeID + BLS + endTime + delegationFee + `[]StakerConfig{addr, amount, feePayer}`), P-chain URI.

**Logic (`runPrepare` L56):**
1. Parse NodeID, BLS pubkey (48 B), BLS PoP (96 B). Validate exactly one `feePayer=true`.
2. `primary.FetchPState(ctx, uri, allStakerAddrs)` → shared chain client, `pCTX` (ComplexityWeights, GasPrice, AVAX assetID), and per-address UTXO store.
3. `builder.EstimateAddPermissionlessValidatorTxComplexity(pop, valOwner, delOwner)` → baseline complexity for the validator-only parts of the tx.
4. For each **non-fee-payer** staker `i`:
   `builder.Spend(backend, pCTX, addrSet={addr_i}, toBurn={}, toStake={AVAX: amount_i}, fee=0, complexity=0, changeOwner=nil, opts=WithCustomAddresses({addr_i}))`
   → `SpendResult{Inputs, ChangeOutputs, StakeOutputs}` — all referencing only that staker's UTXOs, change back to them, stake-out owned by them.
5. `builder.ContributionComplexity(nonFeePayerResults...)` + baseCx = full complexity.
6. `builder.Spend` for the **fee payer** with `toStake={AVAX: amount_feePayer}` and `complexity=full` → picks extra UTXOs to cover the fee.
7. `builder.NewAddPermissionlessValidatorTxFromParts(pCTX, vdr, pop, valOwner, delOwner, shares, results, memo=nil)` — concatenates per-staker inputs/change/stake-outs; sorts all three; sorts `rewardsOwner.Addrs`; emits `*txs.AddPermissionlessValidatorTx`.

**Output (`PrepareOutput`):**
- `unsignedTx` — JSON of the tx for humans
- `unsignedTxHex` — canonical bytes that signers will hash
- `utxos []hex` — every UTXO consumed by an input (signers reconstruct their backend from this)
- `signers []{address}` — whose sigs are required, by input order

**Invariants enforced here:**
- Each non-fee-payer's inputs are *only* their own UTXOs (`WithCustomAddresses({addr_i})`).
- Each stake-out is `threshold=1` to the original owner → **principal never commingles**.
- RewardsOwner is `threshold=N` over all participants (sorted) → **no unilateral spend of reward**.

---

## Step 2 — `sign.go` → `PartialSignature`

**Inputs:** one staker's `PrepareOutput` + their CB58 private key.

**Logic (`runSign` L49):**
1. `txs.Codec.Unmarshal(unsignedBytes)` → structured `txs.Tx`.
2. `buildUTXOBackend(utxoHexes)` populates a `minimalSignerBackend` that implements `signer.Backend.GetUTXO` (no `GetOwner` — nil fx owners).
3. `psigner.New(secp256k1fx.NewKeychain(privKey), backend).Sign(ctx, tx)` — SDK partial signer:
   - For each input: look up UTXO → read `OutputOwners.Addrs` → map `SigIndices[j]` to address.
   - If our keychain has that address, sign the unsigned-tx hash and place sig at `creds[i].Sigs[j]`.
   - If not, leave zeros (`if signer == nil { continue }`) — preserves other parties' slots.
4. Emit `PartialSignature{address, credentials: [][]sigHex}` — N credentials matching tx input count, most slots empty.

This is why the party works: one canonical unsigned tx, N independent signing passes, each partial fills a disjoint set of slots.

---

## Step 3 — `submit.go` → merge + `IssueTx` (validator tx)

**Inputs:** unsigned tx hex + N partial-sig JSON files.

**Logic (`runSubmitTx` L44):**
1. Pre-allocate `creds[i] = &secp256k1fx.Credential{Sigs: make([][65]byte, numSigIndicesFromInput(input))}` for each input.
2. Merge: for each `PartialSignature` → for each credential slot → if partial has a non-empty sig and our slot is still zero, copy it in.
3. `txs.Codec.Marshal(signedTx)` → `signedBytes`. `tx.SetBytes(unsigned, signed)` computes `TxID = sha256(signedBytes)`.
4. `platformvm.NewClient(uri).IssueTx(ctx, signedBytes)` → accepted txID.

Chain-side verification will:
- Call `utxoDB.GetUTXO(input.InputID())` for each input → `ErrNotFound` fails here
- Recover each sig slot → match against `OutputOwners.Addrs[SigIndices[j]]`
- Flow-check `sum(inputs) − sum(outputs) == computedFee`

---

## Step 4 — `prepare_split.go` → unsigned reward-split `BaseTx`

**Inputs:** accepted validator tx bytes + collected partial sigs (to deterministically recompute `TxID` locally) + known `rewardAmount` (from `GetCurrentValidators[i].PotentialReward`).

**Logic (`runPrepareSplit` L53):**
1. Unmarshal the validator tx → extract `Outs`, `StakeOuts`, `ValidatorRewardsOwner` (N-of-N, sorted).
2. **Derive the reward UTXO's position deterministically:**
   `rewardOutputIndex = uint32(len(Outs) + len(StakeOuts))`
   Matches `vms/platformvm/txs/executor/proposal_tx_executor.go` `rewardValidatorTx` (L459): reward UTXO is written at `{TxID: stakingTxID, OutputIndex: len(outputs)+len(stake)}` on the commit path.
3. Rebuild the *signed* validator tx locally with the merged sigs → compute its `TxID` (== what chain accepted, since marshal is deterministic).
4. Build one `TransferableInput`:
   ```
   UTXOID: {TxID: validatorTxID, OutputIndex: rewardOutputIndex}
   Asset:  AVAX
   In:     secp256k1fx.TransferInput{Amt: rewardAmount, SigIndices: [0..N-1]}
   ```
5. Derive per-staker stake ratio by walking `StakeOuts`:
   `stakerAmounts[owner] += out.Amt` → normalized against `totalStake`.
6. **Two-pass dynamic-fee fix:**
   - Pass A: build outputs totaling `rewardAmount` (proportional by stake), assemble `BaseTx`, `fee.TxComplexity(tx) → ToGas(pCTX.ComplexityWeights) → Cost(pCTX.GasPrice) = txFee`.
   - Pass B: rebuild outputs totaling `rewardAmount − txFee`, each `= (rewardAmount − txFee) × stakerAmounts[addr] / totalStake`; last staker absorbs integer-division dust.
7. Emits `SplitPrepareOutput` with unsigned hex, a synthesized reward UTXO hex (built from the rewardsOwner so signers' backends can resolve the input), and signers list.

**Key property:** outputs reference the *same* three addresses as the rewardsOwner but each with `threshold=1` → reward lands as three spendable UTXOs, one per staker, in the exact ratio of their stake.

---

## Step 5 — `sign_split.go` → partial sig on split

Identical mechanics to step 2, operating on the split tx + the synthesized reward UTXO. Since `SigIndices=[0,1,2]` and owners is N-of-N sorted, each staker's partial fills exactly one slot in the single credential.

---

## Step 6 — `submit_split.go` → merge + `IssueTx` (split)

Same merge pattern as step 3. Chain's flow check requires `input(=rewardAmount) − sum(outputs) == txFee` — by construction the pre-computed fee matches.

---

## Byte-level cheatsheet

| Artifact | Codec type ID | Defined at |
|---|---|---|
| `secp256k1fx.TransferInput` | 5 | `vms/secp256k1fx/transfer_input.go` |
| `secp256k1fx.TransferOutput` | 7 | `vms/secp256k1fx/transfer_output.go` |
| `secp256k1fx.Credential` | 9 | same package |
| `AddPermissionlessValidatorTx` | 32 | `vms/platformvm/txs/add_permissionless_validator_tx.go` |
| `BaseTx` (post-Durango) | 34 | `vms/platformvm/txs/base_tx.go` |

UTXO serialization (on-disk / `getRewardUTXOs` hex):
`codec(2) ‖ TxID(32) ‖ OutputIndex(4) ‖ AssetID(32) ‖ OutTypeID(4) ‖ {TransferOutput: Amt(8), Locktime(8), Threshold(4), nAddrs(4), Addrs(20·n)}`

---

## Sort rules (builder enforces, chain verifies)

- **Inputs:** sorted by `avax.CompareInputs` — TxID byte-ascending, then OutputIndex ascending.
- **Outputs & StakeOuts:** `avax.SortTransferableOutputs(outs, txs.Codec)` — lexicographic on marshaled bytes.
- **OutputOwners.Addrs:** `utils.Sort(addrs)` — ShortID byte-order.
- **SigIndices:** reference positions in the *sorted* Addrs list. Each signer's partial fills the slot matching their address's sorted position.

---

## Key constraints (why the chain accepts the tx)

| Rule | Source |
|---|---|
| Total staked weight = `sum(StakeOuts.Amt)` | `staker_tx_verification.go` weight checks |
| `MinValidatorStake ≤ weight ≤ MaxValidatorStake` | same |
| `DelegationShares ≥ MinDelegationFee` (default 20000 = 2%) | `staker_tx_verification.go:127` |
| `duration = End − blockTimestamp ≥ MinStakeDuration` | `staker_tx_verification.go:547` (post-Durango uses block time, not `tx.Start`) |
| Memo length = 0 post-Durango | `avax.VerifyMemoFieldLength` |
| `sum(inputs) − sum(outputs) ≥ fee.TxComplexity→Gas→Cost` | `FlowChecker.VerifySpend` + `feeCalculator.CalculateFee` |
| Each input's UTXO resolvable via `utxoDB.GetUTXO(input.InputID())` | `utxo/verifier.go:107` |
| Credential `Sigs[j]` recovers to `OutputOwners.Addrs[SigIndices[j]]` | `VerifySpendUTXOs` → fx verify |

---

## Where the heavy lifting lives in avalanchego

| File | What |
|---|---|
| `wallet/chain/p/signer/signer.go` | `Signer` interface + partial-sig contract |
| `wallet/chain/p/signer/visitor.go` | `sign()` — iterates inputs, fills/skips slots by keychain availability |
| `wallet/chain/p/builder/multi_party.go:85` | `NewAddPermissionlessValidatorTxFromParts` — merges per-staker SpendResults |
| `wallet/chain/p/builder/builder.go` | `Spend()`, `EstimateAddPermissionlessValidatorTxComplexity`, `ContributionComplexity` |
| `wallet/chain/p/context.go:21` | `NewContextFromURI` — fetches `ComplexityWeights` + `GasPrice` |
| `wallet/subnet/primary/api.go:147` | `FetchPState` — client + pCTX + UTXOs |
| `vms/platformvm/txs/executor/proposal_tx_executor.go:417-518` | `rewardValidatorTx` — reward UTXO creation + OutputIndex formula |
| `vms/platformvm/txs/executor/staker_tx_verification.go` | per-input verify, weight/duration/fee checks |
| `vms/platformvm/utxo/verifier.go:94` | `VerifySpend` / `VerifySpendUTXOs` — the flow check + sig check |
| `vms/platformvm/txs/fee/complexity.go:233` | `TxComplexity(txs ...UnsignedTx)` |
| `vms/components/avax/utxo_id.go:46` | `InputID() = TxID.Prefix(uint64(OutputIndex))` — the UTXO DB key |

---

## Known gotchas (each is a bug-trap I hit in this codebase)

1. **Reward UTXO index drift.** `len(outputs)` is *not* number of stakers — each UTXO consumed on input produces its own change output, so `len(outputs)` varies run-to-run. Always compute `rewardOutputIndex = uint32(len(tx.Outs) + len(tx.StakeOuts))` from the actual tx, never hardcode.
2. **Post-Durango memo must be 0 bytes.** `VerifyMemoFieldLength` rejects any non-empty memo. You can't differentiate two identical retry attempts via memo.
3. **Dropped-tx cache by `TxID`.** If you submit a split tx before the reward UTXO exists, chain caches `Dropped` keyed by txID. Retries with identical bytes return cached Dropped forever. **Fix:** before any first-submit, call `GetCurrentValidators([nodeID])`; only submit once the NodeID is gone from the active set. The server's `trySubmitSplitTx` implements this gate.
4. **Fee must be non-zero post-ACP-103.** The split tx originally had `input.Amt = rewardAmount` with outputs summing to `rewardAmount` → fee=0 → chain rejects. Always two-pass: build to measure complexity, subtract computed fee from outputs, rebuild.
5. **HRP comes from networkID, not from hardcoded "fuji" or "local".** For `network-id=88888` or any non-reserved ID, `constants.GetHRP` returns `"custom"`. Derive addresses with the actual networkID.
