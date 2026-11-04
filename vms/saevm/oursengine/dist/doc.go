// Package dist is the one copy-chunks verb: copy 1 MiB chunks of a chunk
// file (package chunkfile) from any source to the local sparse file,
// verifying every chunk against the file's hash list.
//
// # Sources
//
// A Source holds chunks: HTTPSource (a node's endpoint or the R2 mirror),
// DirSource (another local directory), later a p2p peer. One interface:
// Fetch(hash, first, n) returns the raw bytes of n consecutive chunks,
// Have(hash, n) the chunks the source announces, MaxChunks the glue cap
// (HTTP 16; a p2p peer 1: avalanchego caps a whole message, framing
// included, at 2 MiB (constants.DefaultMaxMessageSize), so two 1 MiB chunks
// do not fit; its default inbound bandwidth throttler also refills at
// 512 KiB/s per peer, which our node must raise). A p2p
// source plugs in unchanged: Fetch = an AppRequest {hash, first, n}
// answered with the raw bytes, Have = the announce bytes below, and the
// peer's "unknown file" and "not held" answers map to ErrUnknown and
// ErrNotHeld.
//
// HTTPSource speaks HTTP/1.1 only, one TCP connection per request in
// flight (see NewHTTPSource for why not HTTP/2).
//
// # The HTTP language
//
// Every source and mirror speaks it; a mirror is a dumb object store that
// holds each whole file as one object named by its hash.
//
//	GET {base}/{hash}
//
// {hash} is the file hash in 64 lowercase hex digits. The body is the raw
// file, header chunks included: chunk i is bytes [i<<20, min((i+1)<<20,
// size)). The request carries one Range of the form "bytes=a-b",
// "bytes=a-" or "bytes=-k"; a range past the end is clipped to the last
// byte. Clients ask chunk-aligned runs: a = first<<20, b = (first+n)<<20 - 1.
//
//   - 206 Partial Content, "Content-Range: bytes a-e/size", Content-Length
//     e-a+1. The client requires the start it asked for and e equal to the
//     end it asked for or to size-1.
//   - 200 OK, the whole file: no Range header and every chunk held.
//   - 404 Not Found: the hash is unknown here.
//   - 416 Range Not Satisfiable, "Content-Range: bytes */size": the range
//     starts past the end, is malformed or names several ranges, or touches
//     a chunk the server does not hold. A node serves only chunks it holds
//     (presence is trusted: chunkfile settles a crash's half-written chunks
//     at Open) and never fetches on another's behalf (exists = skip). The
//     client verifies every chunk anyway.
//
// Object stores (R2, S3) answer the same statuses for whole objects, so a
// mirror needs no code.
//
//	GET {base}/{hash}/announce
//
// 200 with the Version byte (1) followed by one zstd frame (single segment,
// so the frame header carries the content size) of ceil(N/8) bytes: bit k
// (least significant first) of byte j says chunk 8j+k is held, bits from N
// up are zero. N is the file's chunk count, which the client knows from the
// trusted size. 404: the source does not announce the file (a mirror, which
// holds every chunk of the files it has, or a node without the file, whose
// Fetch then answers 404). The node-wide announce of the design (one bitmap
// over every file) is the per-file bitmaps concatenated in manifest order
// in one frame; it comes with the manifest.
//
// The file hash names the header bytes but not the file size (a size cut at
// a chunk boundary with the same header chunk count verifies), so a fresh
// download takes the size from the manifest, never from a source.
//
// # The Downloader
//
// One Downloader per file. With the walk on (the snapshot, the open tier2)
// Fetchers walk the absent chunks in a seeded random order (nodes started
// together pull different chunks), gluing adjacent absent chunks up to the
// source's cap. Want(i) (a reader that hit chunkfile.AbsentError) jumps the
// queue: FaultWorkers serve only faults, a fault never waits for a walk run
// that holds its chunk, and while faults are queued or in flight the walk
// starts no run and its in-flight runs stop reading, so a fault does not
// queue behind walk bytes at the bottleneck; a run paused longer than
// WalkPause is given back. With OnDemand only faults are fetched (evictable
// files). Sources are picked by fewest requests in flight among those that
// hold the chunk; a source that sends K bad chunks is dropped, K transport
// failures in a row bench it (1 s doubling to 60 s).
package dist
