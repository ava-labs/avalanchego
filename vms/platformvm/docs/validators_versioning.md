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

## Validators diffs layout

Validators diffs layout is optimized to support iterations.


## Validators diff usage in rebuilding validators state