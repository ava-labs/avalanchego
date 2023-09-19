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

`GetValidatorSet` lets any VM specify a subnet and a height and returns the map of subnet validators which were active at the requested height. In more details, `GetValidatorSet` returns, for each active validator, its `NodeID`, its `Weight` and its `Bls Public Key` if it was registered. All of these information is collected in a struct named `validators.GetValidatorOutput`.

We will see in next section how the P-chain keeps track of this information in time, while the validator set changes

## Validators diffs content

Every new block accepted by the P-chain can potentially alter the validator set of any subnet, including the primary one. New validators may be added; some of them may have reached their end of life and are therefore removed. Moreover a validator can register itself again once its staking time is done, possibly with a `Weight` and a `BLS Public key` different from previous staking period.

Whenever the block at height `h` adds or removes a validator, the P-chain does, among others, the following operations:

1. it updates the current validators set to add the new validator or remove it if expired;
2. it explicitly records the validator set diffs with respect to the validator set at height `h-1`.

These diffs are key to rebuild the validator set at a given past height. We'll see how the diffs are stored and used in next section. Here we illustrate their content.

The validators diffs track changes in a validator's `Weight` and `BLS Public key`.

Both attributes do not currently change through a validator's lifetime; changes happen only at a validator creation and expiration.

We record `Weight` and `BLS Public Key` diffs slightly differently. Specifically:

|                             |                                           Weight Diff                                          |                                       Bls Key Diff                                      |
|:---------------------------:|:----------------------------------------------------------------------------------------------:|:---------------------------------------------------------------------------------------:|
|      Validator creation     | record ``` state.ValidatorWeightDiff{    Decrease: false,    Weight : validator.Weight, }  ``` | record an empty byte slice if `validator.BlsKey` is specified; otherwise record nothing |
|      Validator removal      | record ``` state.ValidatorWeightDiff{    Decrease: true,    Weight : validator.Weight, }  ```  | record `validator.BlsKey` if it is specified; otherwise record nothing                  |
| Delegators addition/removal |                                      No entry is recorded                                      |                                   No entry is recorded                                  |

`Weight` diffs are encoded `state.ValidatorWeightDiff` and are forward-looking: diffs recorded at height `h` store the changes that trasform validators weight at height `h-1` into validators weight at height `h`.
`BLS Public Key` diffs are backward-looking: diffs recorded at height `h` store the changes that transform validator BLS Key at height `h` into validator BLS key at height `h-1`.

## Validators diffs layout

Validators diffs layout is optimized to support iterations.


## Validators diff usage in rebuilding validators state