# Genesis

The Genesis package converts formatted JSON files into the genesis of the Primary Network. For the simplest example, see the [Local Genesis](./genesis_local.json) JSON file.

The genesis JSON file contains the following properties:

- `networkID`: A unique identifier for the blockchain, must be a number in the range [0, 2^32).
- `allocations`: The list of initial addresses, their initial balances and the unlock schedule for each.
- `startTime`: The time of the beginning of the blockchain, it must be a Unix
  timestamp and it can't be a time in the future.
- `initialStakeDuration`: The stake duration, in seconds, of the validators that exist at network genesis.
- `initialStakeDurationOffset`: The offset, in seconds, between the end times
  of the validators that exist at genesis.
- `initialStakedFunds`: A list of addresses that own the funds staked at genesis
  (each address must be present in `allocations` as well)
- `initialStakers`: The validators that exist at genesis. Each element contains
  the `rewardAddress`, NodeID and the `delegationFee` of the validator.
- `cChainGenesis`: The genesis info to be passed to the C-Chain.
- `message`: A message to include in the genesis. Not required.

## Allocations and Genesis Stakers

Each allocation contains the following fields:

- `ethAddr`: Annotation of corresponding Ethereum address holding an ERC-20 token
- `avaxAddr`: X/P Chain address to receive the allocation
- `initialAmount`: Initial unlocked amount minted to the `avaxAddr` on the X-Chain
- `unlockSchedule`: List of locked, stakeable UTXOs minted to `avaxAddr` on the P-Chain

Note: if an `avaxAddr` from allocations is included in `initialStakers`, the genesis includes
all the UTXOs specified in the `unlockSchedule` as part of the locked stake of the corresponding
genesis validator. Otherwise, the locked UTXO is created directly on the P-Chain.

## Primary Network Validator Uptime Requirements

The Primary Network uses an uptime requirement configuration to determine whether validators should be rewarded based on their observed uptime during their validation period.

### Configuration

The uptime requirement consists of:
- A default percentage (e.g., 80%) that applies to all validators by default
- An optional schedule of requirement updates, each specifying a timestamp and a new requirement percentage

When a validator's staking period begins, the applicable uptime requirement is determined by finding the most recent update in the schedule whose timestamp is at or before the validator's start time. If no such update exists, the default percentage is used. This means validators that start staking after a scheduled increase will be subject to the higher requirement.

The schedule must have strictly increasing timestamps - each update must occur after all previous updates. All requirement percentages must be in the range [0, 1].

#### Example Configuration

```go
UptimeRequirementConfig: UptimeRequirementConfig{
    DefaultRequiredUptimePercentage: 0.8, // 80%
    RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
        {
            Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
            Requirement: 0.9, // 90%
        },
    },
}
```

This configuration sets the default requirement at 80% and schedules an increase to 90% effective February 12, 2026 at 00:00:00 UTC. Validators whose staking period begins before this date will be subject to the 80% requirement, while those starting on or after this date will need to maintain 90% uptime to be rewarded.

### Consensus and Preference

The uptime requirement determines each validator's **initial preference** for whether to reward a fellow validator when their staking period ends. If the validator's observed uptime meets or exceeds the applicable requirement, the validator initially prefers to issue the reward (commit). Otherwise, it initially prefers to deny the reward (abort).

However, the **final decision on whether to reward a validator is determined through Snowman consensus**. All validators vote on whether to commit (reward) or abort (deny reward) the validator's staking transaction. The uptime requirement only influences each validator's initial preference - the network reaches agreement through the standard consensus process, ensuring that the majority decision prevails regardless of individual preferences.
