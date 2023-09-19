# Validators versioning

One of the main responsability of the P-chain is to register and expose the validators set of any subnet at every height.

This information helps subnets to bootstrap securily, downloading information from active validators only; moreover it supports validated cross-chain communication via Warp. 

In this brief document we dive into the technicalities of how `platformVM` tracks and versions the validators set of any subnet.

We assume the reader is familiar with the difference between validators and delegators. `platformVM` tracks both type of stakers but in the following we'll focus on validators only.

## The tracked content

The entry point to retrieve validators information at given height is the `GetValidatorSet` method in `validators` package. Here is its signature:
``` golang
GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*GetValidatorOutput, error)
```

`GetValidatorSet` lets any VM specify a subnet and a height and returns the data of all subnet validators active at the requested height, and only those.

Validators data are collected in a struct named `validators.GetValidatorOutput`  which holds for each active validator, its `NodeID`, its `Weight` and its `Bls Public Key` if it was registered.

We will see in next section how the P-chain keeps track of this information in time, while the validator set changes.

## Validators diffs content

Every new block accepted by the P-chain can potentially alter the validator set of any subnet, including the primary one. New validators may be added; some of them may have reached their end of life and are therefore removed. Moreover a validator can register itself again once its staking time is done, possibly with a `Weight` and a `BLS Public key` different from previous staking period.

Whenever the block at height `H` adds or removes a validator, the P-chain does, among others, the following operations:

1. it updates the current validators set to add the new validator or remove it if expired;
2. it explicitly records the validator set diffs with respect to the validator set at height `H-1`.

These diffs are key to rebuild the validator set at a given past height. In this section we illustrate their content. In next ones, We'll see how the diffs are stored and used.

The validators diffs track changes in a validator's `Weight` and `BLS Public key`. Along with the `NodeID` these are the data exposed by the `GetValidatorSet` method.

Note that both `Weight` and `BLS Public key` cannot change through a validator's lifetime. So we only need to track changes at a validator creation and removal, because changes only happen at those times.

Here is what is the `Weight` and `BLS Public key` diff content at creation and removal 

|                             |                                           Weight Diff                                          |                                       Bls Key Diff                                      |
|:---------------------------:|:----------------------------------------------------------------------------------------------:|:---------------------------------------------------------------------------------------:|
|      Validator creation     | record ``` state.ValidatorWeightDiff{    Decrease: false,    Weight : validator.Weight, }  ``` | record an empty byte slice if `validator.BlsKey` is specified; otherwise record nothing |
|      Validator removal      | record ``` state.ValidatorWeightDiff{    Decrease: true,    Weight : validator.Weight, }  ```  | record `validator.BlsKey` if it is specified; otherwise record nothing                  |
| Delegators addition/removal |                                      No entry is recorded                                      |                                   No entry is recorded                                  |

Note that `Weight` diffs are encoded `state.ValidatorWeightDiff` and are *forward-looking*. a diff recorded at height `H` stores the change that trasforms validator weight at height `H-1` into validator weight at height `H`.

On the contrary, `BLS Public Key` diffs are *backward-looking*: a diff recorded at height `H` stores the change that transforms validator `BLS Public Key` at height `H` into validator `BLS Public key` at height `H-1`.

Finally, if no changes happen to the validator set, no diff entry is recorded. This happens when the accepted block changes only delegators or does not alter the stakers set at all. So technically we should not expect a validator `Weight` or `BLS Public Key` diff stored at every height `H`.

## Validators diffs layout

Validators diffs layout is optimized to support iterations. Validator sets are rebuilt by cumulating `Weight` and `BLS Public Key` diffs from the top-most down to the requested height. So validator diffs are store so that it's fast to iterate them in this order.

`Weight` diffs are stored as a contiguous block of key, values as follows:

| Key                                | Value                                |
|------------------------------------|--------------------------------------|
| SubnetID + Reverse_Height + NodeID | serialized state.ValidatorWeightDiff |

Note that:

1. `Weight` diffs related to the a subnet are store contiguously.
2. Diff height is stored as `Reverse_Height`. It is stored with big endian format and has its bits flipped too. This ensurses that heighs are store in order (by big endianess) with the top most height always being the first (by bit flipping)
3. `NodeID` is part of the key and `state.ValidatorWeightDiff` is part of the value stored

`BLS Public` diffs are store as follows:

| Key                                | Value                         |
|------------------------------------|-------------------------------|
| SubnetID + Reverse_Height + NodeID | validator.BlsKey bytes or nil |

Note that:

1. `BLS Public Key` diffs have same keys as `Weight` diffs. So same ordering is guaranteed
2. Value is either validator `BLS Public Key` bytes or an empty byte slice, as illustrated in previous section.

## Validators diff usage in rebuilding validators state

Let's see now how diffs are use to rebuild the validator set at a given height. The procedure varies slightly for Primary Network and subnet validator, so we'll describe them separately.
We assume that the reader know that, as of Cortina fork, every subnet validator must also be a primary network validator.

### Primary network validator set rebuild

Say P-chain current height is `T` and we want to retrieve Primary network validators at height `H < T`. We proceed as follows:

1. We retrieve the Primary Network validator set at current height `T`. This is the base state on top of which diffs will be applied.
2. We apply weight diffs first. Specifically:
   -  We iterate `Weight` diff from the top most height smaller or equal to `T`. Remember that entries heights does not need to be contiguous, so we start from the highest height smaller or equal to `T`, in case `T` has not diff entry.
   -  Since `Weight` diffs are forward-looking, we apply each diff in reverse. We decrease a validator's weight if `state.ValidatorWeightDiff.Decrease` is `false` and we increse it if it is `true`.
   - We take care of adding or removing a validator from the base set based on its weight. Whenever a validator weight, following diff application, becomes zero, we drop it; conversely whenever we encounter a diff increasing weight for a currently-non-existing validator, we add the validator in the base set.
   -  We stop applying diffs at the first height smaller or equal to `H+1`. Note that a `Weight` diff stored at height `K` holds the content to turn validator state at height `K-1` into validator state at height `K`. So to get validator state at height `K` we must apply diff content at height `K+1`.
3. Once all `Weight` diffs have been applied the resulting validator set will contain all Primary Network validators active at height `H` and only those. However their `BLS Public Keys` may be wrong, missing or different from those registered at height `H`. We solve this by applying `BLS Public Key` diffs to the vallidator set:
   - Once again we iterate `BLS Public Key` diffs from the top most height smaller or equal to `T` till the first height smaller or equal to `H+1`.
   - Since `BLS Public Key` diff are *backward-looking*, we simply nil the BLS key when diff is nil and we restore the BLS Key when it is specified in the diff. 