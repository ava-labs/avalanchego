# Serving `GetAncestors` without backward iteration

The design in [get_ancestors.md](get_ancestors.md) walks the database backward from the requested height, which requires backward iteration support from the database layer.
That support is an interface change touching every database implementation, remote and layered databases cannot honour it at all, and it is by far the hardest part of the design to land.
This document describes how to build the request without it.

The request shape, the storage model, and the response assembly rules are those of the main document.
Everything below replaces only its "opposite directions" section.

## The constraint

Forward only iterators visit a height range oldest first, while the response is assembled newest first and the byte cap discards the oldest end of the range.
Which blocks the cap discards is only known once everything newer has been sized.
A single forward pass therefore cannot stop early, it must read and buffer the entire requested range before assembly can begin.

## The design, descending sub ranges

Serving order can still descend by choosing where each pass begins.
The requested range is processed in sub ranges of consecutive heights, newest sub range first, each read oldest first by a fresh pair of forward iterators, one over headers and canonical hashes, one over bodies, running concurrently.
A buffered sub range is assembled newest first while the next sub range down is already being read, so assembly hides behind disk time.

The sub range size is the tuning knob.
Reads past the cap's cut are bounded by one sub range, so smaller sub ranges waste less, while each sub range pays iterator setup, teardown, and a re-seek, so larger sub ranges amortise better.
Sub ranges of 128 heights measured best, and doubling them was already slower.

Relative to the backward walk this design still pays per sub range iterator churn and up to one sub range of wasted reads, but it preserves the property that matters, reading stops near the cap instead of covering the whole range.

## The simpler alternative, and why not

One forward pass over the whole range with a single pair of iterators avoids all iterator churn and any notion of sub ranges.
It was measured fifteen percent slower on a request where the cap discards half the range, its buffering holds the entire range in memory at once, and its response MAY be empty when the time limit expires mid pass, because it reaches the requested block last.
Its only advantages are simplicity and a slight edge when the cap does not bite.
That trade is acceptable for a rarely taken fallback, which is how it is currently used, but an implementation serving all traffic without backward iteration should use the sub ranges.

## Measured effect

Measured on an Apple M2 Max over a 2000 block chain with 10 transactions per block, where the byte cap truncates responses to 1054 blocks.

| implementation                        | ms per request |
| ------------------------------------- | -------------: |
| forward pass over the whole range     |           1.0  |
| descending sub ranges, this design    |           0.87 |
| backward walk, for comparison         |           0.67 |

The forward designs' gap to the backward walk grows with the fraction of the range the byte cap discards.
