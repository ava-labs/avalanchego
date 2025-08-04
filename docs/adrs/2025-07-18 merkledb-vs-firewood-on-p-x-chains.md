# Should the P- and X-chains use Firewood or merkleDB?

## Context and Problem Statement

The state of the P- and X- chains is currently indexed by hash.  This precludes use of dynamic sync, which needs to verify pieces of state it receives at many block heights.   Dynamic sync is sweet, and we want it, because it speeds up bootstrapping and more importantly, allows us to shrink our storage footprint.   So, we also want some sort of merkle/patricia storage thing.

We have two such things: merkledb and firewood.  

## Decision Outcome

**We will use Firewood on P- and X- chains.**

## Date

2025-07-18

## Goals

* Store verifiable chain state in form compatible with dynamic sync
* Correctness
* Reliability and Resilience
* Minimal on-going maintenance costs

## Non-goals

* Improved Throughput - needs of P- and X-chains are met by current solutions.
* Improved Latency - idem.
* Consistent recovery - we recover via sync from other nodes if needed

## Arguments for merkledb

* Has been baking longer
* ~~Razvan has spent effort making it production-ready~~ (sunk-cost fallacy)
* Better resiliency through software diversity
* Firewood is not proven
* ~~Firewood performance on EBS not yet known~~ (Speed is not a goal)

## Arguments for Firewood

* Already a rewrite and under development, easier to modify
* Minimize on-going maintenance (already committed to use on C-chain)
* ~~Faster~~ (Speed is not a goal)

## Rationale

We discounted software diversity's accrual to resilience, because both P- and C- chains are critical to the Avalanche network, and there is no situation where we could tolerate either being down.

merkleDb seems likely to continue to need attention, and despite its greater age it does not appear to have been more thoroughly tested.  At this point, Firewood has ingested data resulting from verification of all blocks since genesis, more than once.  

## Participants

Instigator: Joshua Kim

Participants: Stephen Buttholph, Ron Kuris, Juan Leon

<!-- This is an optional element. Feel free to remove. -->
### Consequences

_To be completed when known, in future_
