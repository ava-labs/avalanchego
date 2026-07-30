# Serving `GetAncestors`

A node joining the network downloads the accepted chain from its peers.
It requests a block by hash and the peer replies with that block followed by as many of its ancestors as fit, newest first, bounded by a block count, a byte size, and a time limit.
The requester repeats this walking backwards, so a serving node answers these requests for the entire chain, for every joining peer.
The latency of one request therefore gates how fast the network can grow.

## What is on disk

The chain database stores an accepted block as three records, all keyed primarily by height.

1. The block's header, RLP encoded, keyed by height and hash.
2. The block's body, RLP encoded, keyed by height and hash.
   The body holds every block field other than the header, transactions and uncles, plus any VM specific extra fields.
3. The accepted hash at that height, keyed by height alone.

A separate index maps each accepted hash back to its height.
Acceptance writes all of these before it completes, and the genesis block is written at startup, so disk is a complete record of accepted history.

Disk is not, however, an exclusively canonical record.
Current versions only write accepted blocks, but older versions also wrote non canonical blocks, so a database with history from them can hold several headers and bodies at one height.
The canonical hash record is the arbiter of which one is accepted.

The wire format of a block is a single RLP list holding the header followed by exactly the fields of the body, in body order.

## The design

Each design decision below is shaped by one observation.

### Unaccepted blocks are not served

Ancestry by height is only meaningful for accepted blocks, and only accepted blocks appear in the hash to height index.
One index read therefore both resolves the requested height and proves acceptance.
Any other hash is answered as not found.

### Ancestors are read by height range, not by hash

The requested block and its ancestors occupy consecutive heights, and every record is keyed by height, so all of their records sit adjacent on disk.
One iterator pass over the header keyspace yields the accepted hashes and headers in height order, and one concurrent pass over the body keyspace yields the bodies.
Looking blocks up individually would instead cost three random reads per block, each paying the database's full lookup overhead, where a range pass pays it once.

A height may hold non canonical siblings, see the storage section.
Each header and body encountered carries its block's hash in its key, so candidates are checked against the height's canonical hash, and on a mismatch the canonical block's record is fetched with a targeted read.
Siblings are rare enough that the targeted reads cost nothing in practice, while the check keeps them out of every response.

### Reading and serving run in opposite directions

This was the central tension of the design.
Database iterators historically only advanced from lower keys to higher, so a range pass visited blocks oldest first.
The response runs the other way, newest first, and the byte cap discards the oldest end of the range, so a forward pass cannot stop at the cap and must read and buffer the whole range before assembly can begin.

The tension was dissolved by teaching the database layer to iterate backward.
The two iterators walk from the requested height downward, each in its own goroutine, streaming per height records to the assembler so that their reads overlap each other and the splicing.
The assembler pairs the streams by height, splices, appends, and stops the moment the response is complete, so only returned blocks are ever read.
The design space without backward iteration, is documented in [get_ancestors_without_backward_iteration.md](get_ancestors_without_backward_iteration.md).

### Streamed records are batched into fixed arrays

Sending each height's records through a channel individually costs a synchronisation per height, measured at a sixth of the whole request.
Records are therefore streamed in fixed size arrays passed by value, which amortise the synchronisation without allocating anything per batch.

Batching deepens read ahead.
The scanners keep reading until channel backpressure stops them, so on a byte capped request they overshoot the cut by up to the batch size times the channel depth plus the batch being filled, per stream, and every overshot height costs a handful of discarded allocations.
Batches of 64 with a channel depth of 1 measured fastest, and shrinking the batch bought back so few allocations that latency regressed past the unbatched design before the allocations reached it.

### Wire bytes are spliced, not rebuilt

Because the wire format is the header followed by the body's fields, the stored header bytes and the stored body's payload can be concatenated and wrapped in a fresh RLP list header.
That is pure byte copying, roughly a hundred times cheaper than the alternative of decoding both records into objects and re-encoding them.

The caveat is the VM's extra block fields.
Their encoding is controlled by pluggable hooks, and the hooks define the block's field list and the body's field list independently, so header then body order is a convention rather than a guarantee.
Every hook set this repository registers follows the convention, so the tests pinning it under each registered hook set are the only guard against a misaligned future hook set corrupting responses.

### Scan results are referenced, not copied

The database iterators in this repository return slices that remain valid indefinitely, so the response is assembled from them directly.
This is a property of our database layer, deliberately relied upon and documented, and it is worth about a tenth of each request.

## Assembling the response

The requested block is always included, even if it alone exceeds the byte cap.
Ancestors are appended newest first until the block count is met, the next block would exceed the byte cap, or a height is missing.
A missing height cannot occur within accepted history, so it proves the remainder is not resolvable and cleanly ends the response.

## Measured effect

Measured on an Apple M2 Max over a 2000 block chain with 10 transactions per block, where the byte cap truncates responses to 1054 blocks.

| implementation                      | ms per request |
| ----------------------------------- | -------------: |
| one block at a time                 |           53.7 |
| parallel per block lookups          |           14.4 |
| forward pass over the whole range   |           1.0  |
| descending sub ranges, forward only |           0.87 |
| backward walk, synchronous          |           0.82 |
| backward walk, streamed and batched |           0.67 |

The gap against the forward pass grows with the fraction of the range the byte cap discards, half in this benchmark.

The remaining time is almost entirely the disk scans themselves.
Of the roughly 8400 allocations per request, three quarters are the database iterators' defensive copies of keys and values, and most of the rest are the per block splice outputs.
