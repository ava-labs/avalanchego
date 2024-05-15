# Validators versioning

One of the main responsibilities of the P-chain is to register and expose the validator set of any Subnet at every height.

This information helps Subnets to bootstrap securely, downloading information from active validators only; moreover it supports validated cross-chain communication via Warp.

In this brief document we dive into the technicalities of how `platformVM` tracks and versions the validator set of any Subnet.

## The tracked content

The entry point to retrieve validator information at a given height is the `GetValidatorSet` method in the `validators` package. Here is its signature:

```golang
GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*GetValidatorOutput, error)
```

`GetValidatorSet` lets any VM specify a Subnet and a height and returns the data of all Subnet validators active at the requested height, and only those.

Validator data are collected in a struct named `validators.GetValidatorOutput` which holds for each active validator, its `NodeID`, its `Weight` and its `BLS Public Key` if it was registered.

Note that a validator `Weight` is not just its stake; it's the aggregate value of the validator's own stake and all of its delegators' stake. A validator's `Weight` gauges how relevant its preference should be in consensus or Warp operations.

We will see in the next section how the P-chain keeps track of this information over time as the validator set changes.

## Validator diffs content

Every new block accepted by the P-chain can potentially alter the validator set of any Subnet, including the primary one. New validators may be added; some of them may have reached their end of life and are therefore removed. Moreover a validator can register itself again once its staking time is done, possibly with a `Weight` and a `BLS Public key` different from the previous staking period.

Whenever the block at height `H` adds or removes a validator, the P-chain does, among others, the following operations:

1. it updates the current validator set to add the new validator or remove it if expired;
2. it explicitly records the validator set diffs with respect to the validator set at height `H-1`.

These diffs are key to rebuilding the validator set at a given past height. In this section we illustrate their content. In next ones, We'll see how the diffs are stored and used.

The validators diffs track changes in a validator's `Weight` and `BLS Public key`. Along with the `NodeID` this is the data exposed by the `GetValidatorSet` method.

Note that `Weight` and `BLS Public key` behave differently throughout the validator's lifetime:

1. `BLS Public key` cannot change through a validator's lifetime. It can only change when a validator is added/re-added and removed.
2. `Weight` can change throughout a validator's lifetime by the creation and removal of its delegators as well as by validator's own creation and removal.

Here is a scheme of what `Weight` and `BLS Public key` diff content we record upon relevant scenarios:

|                    | Weight Diff (forward looking)                                                                           | BLS Key Diff (backward looking)                                                       |
|--------------------|---------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------|
| Validator creation | record ```golang state.ValidatorWeightDiff{       Decrease: false,     Weight: validator.Weight, }``` | record an empty byte slice if validator.BlsKey is specified; otherwise record nothing |
| Delegator creation | record ```golang state.ValidatorWeightDiff{      Decrease: false,     Weight: validator.Weight, }```  | No entry is recorded                                                                  |
| Delegator removal  | record ```golang state.ValidatorWeightDiff{      Decrease: true,     Weight: validator.Weight, }```  | No entry is recorded                                                                  |
| Validator removal  | record ```golang state.ValidatorWeightDiff{      Decrease: true,     Weight: validator.Weight, }```  | record validator.BlsKey if it is specified; otherwise record nothing                  |

Note that `Weight` diffs are encoded `state.ValidatorWeightDiff` and are *forward-looking*: a diff recorded at height `H` stores the change that transforms validator weight at height `H-1` into validator weight at height `H`.

In contrast, `BLS Public Key` diffs are *backward-looking*: a diff recorded at height `H` stores the change that transforms validator `BLS Public Key` at height `H` into validator `BLS Public key` at height `H-1`.

Finally, if no changes are made to the validator set no diff entry is recorded. This implies that a validator `Weight` or `BLS Public Key` diff may not be stored for every height `H`.

## Validator diffs layout

Validator diffs layout is optimized to support iteration. Validator sets are rebuilt by accumulating `Weight` and `BLS Public Key` diffs from the top-most height down to the requested height. So validator diffs are stored so that it's fast to iterate them in this order.

`Weight` diffs are stored as a contiguous block of key-value pairs as follows:

| Key                                | Value                                |
|------------------------------------|--------------------------------------|
| SubnetID + Reverse_Height + NodeID | serialized state.ValidatorWeightDiff |

Note that:

1. `Weight` diffs related to a Subnet are stored contiguously.
2. Diff height is serialized as `Reverse_Height`. It is stored with big endian format and has its bits flipped too. Big endianness ensures that heights are stored in order, bit flipping ensures that the top-most height is always the first.
3. `NodeID` is part of the key and `state.ValidatorWeightDiff` is part of the value.

`BLS Public` diffs are stored as follows:

| Key                                | Value                         |
|------------------------------------|-------------------------------|
| SubnetID + Reverse_Height + NodeID | validator.BlsKey bytes or nil |

Note that:

1. `BLS Public Key` diffs have the same keys as `Weight` diffs. This implies that the same ordering is guaranteed.
2. Value is either validator `BLS Public Key` bytes or an empty byte slice, as illustrated in the previous section.

## Validators diff usage in rebuilding validators state

Now let's see how diffs are used to rebuild the validator set at a given height. The procedure varies slightly between Primary Network and Subnet validator, so we'll describe them separately.
We assume that the reader knows that, as of the Cortina fork, every Subnet validator must also be a Primary Network validator.

### Primary network validator set rebuild

If the P-Chain's current height is `T` and we want to retrieve the Primary Network validators at height `H < T`. We proceed as follows:

1. We retrieve the Primary Network validator set at current height `T`. This is the base state on top of which diffs will be applied.
2. We apply weight diffs first. Specifically:
   - `Weight` diff iteration starts from the top-most height smaller or equal to `T`. Remember that entry heights do not need to be contiguous, so the iteration starts from the highest height smaller or equal to `T`, in case `T` does not have a diff entry.
   - Since `Weight` diffs are forward-looking, each diff is applied in reverse. A validator's weight is decreased if `state.ValidatorWeightDiff.Decrease` is `false` and it is increased if it is `true`.
   - We take care of adding or removing a validator from the base set based on its weight. Whenever a validator weight, following diff application, becomes zero, we drop it; conversely whenever we encounter a diff increasing weight for a currently-non-existing validator, we add the validator to the base set.
   - The iteration stops at the first height smaller or equal to `H+1`. Note that a `Weight` diff stored at height `K` holds the content to turn validator state at height `K-1` into validator state at height `K`. So to get validator state at height `K` we must apply diff content at height `K+1`.
3. Once all `Weight` diffs have been applied, the resulting validator set will contain all Primary Network validators active at height `H` and only those. We still need to compute the correct `BLS Public Keys` registered at height `H` for these validators, as each validator may have restaked between height `H` and `T`. They may have a different (or no) `BLS Public Key` at either height. We solve this by applying `BLS Public Key` diffs to the validator set:
   - Once again we iterate `BLS Public Key` diffs from the top-most height smaller or equal to `T` till the first height smaller or equal to `H+1`.
   - Since `BLS Public Key` diffs are *backward-looking*, we simply nil the BLS key when diff is nil and we restore the BLS Key when it is specified in the diff.

### Subnet validator set rebuild

Let's see first the reason why Subnet validators needs to have handled differently. As of `Cortina` fork, we allow `BLS Public Key` registration only for Primary network validators. A given `NodeID` may be both a Primary Network validator and a Subnet validator, but it'll register its `BLS Public Key` only when it registers as Primary Network validator. Despite this, we want to provide a validator `BLS Public Key` when `validators.GetValidatorOutput` is called. So we need to fetch it from the Primary Network validator set.

Say P-chain current height is `T` and we want to retrieve Primary network validators at height `H < T`. We proceed as follows:

1. We retrieve both Subnet and Primary Network validator set at current height `T`,
2. We apply `Weight` diff on top of the Subnet validator set, exactly as described in the previous section,
3. Before applying `BLS Public Key` diffs, we retrieve `BLS Public Key` from the current Primary Network validator set for each of the current Subnet validators. This ensures the `BLS Public Key`s are duly initialized before applying the diffs,
4. Finally we apply the `BLS Public Key` diffs exactly as described in the previous section.
