# Synthetic Workloads

The `benchmark` binary runs synthetic trie workloads directly against the
Firewood Rust API. These are macro-level database workloads designed to stress different access patterns at scale.

## Table of Contents

- [Workloads](#workloads)
- [Benchmark Specification](#benchmark-specification)
- [Usage](#usage)

## Workloads

Three workloads are available:

1. `tenkrandom` which does transactions of size 10k, 5k updates, 2.5k inserts, and 2.5k deletes
2. `zipf` which uses a zipf distribution across the database to perform updates
3. `single` which only updates a single row (row 1) repeatedly in a tiny transaction

There is also a `create` benchmark which creates the database to begin with. The defaults will create
a 10M row database. If you want a larger one, increase the number of batches.

## Benchmark Specification

This describes each benchmark in detail, and forms a standard for how to test merkle databases. The pseudo-code for each
benchmark is provided.

Note that the `commit` operation is expected to persist the database and compute the root hash for each merkle.

### create

Inserts are done starting from an empty database, 10,000 rows at a time, starting with row id 0 and increasing by 1 for each row.
The key and data consist of the SHA-256 of the row id, in native endian format.

```rust
  for key in (0..N) {
    let key_and_data = sha256(key.to_ne_bytes());
    testdb.insert(key_and_data, key_and_data);
    if key % 10000 == 9999 {
      testdb.commit();
    }
  }
```

Exception: when creating the 1B row database, 100,000 rows at a time are added.

### tenkrandom

This test uses two variables, low and high, which initially start off at 0 and N, where N is the number of rows in the database.
A batch consists of deleting 2,500 rows from low to low+2499, 2,500 new insertions from high to high+2499, and 5,000 updates from (low+high)/2 to (low+high)/2+4999. The key is computed based on the sha256 as when the database is created, but the data is set to the sha256 of the value of 'low', to ensure that the data is updated. Once a batch is completed, low and high are increased by 2,500.

```rust
  let low = 0;
  let high = N;
  loop {
    let hashed_low = sha256(low.to_ne_bytes());
    for key in (low..low+2499) {
      testdb.delete(hashed_key);
      let hashed_key = sha256(key.to_ne_bytes());
    }
    for key in (high..high+2499) {
      let hashed_key = sha256(key.to_ne_bytes());
      testdb.insert(hashed_key, hashed_low);
    }
    for key in ((low+high)/2, (low+high)/2+4999) {
      let hashed_key = sha256(key.to_ne_bytes());
      testdb.upsert(hashed_key, hashed_low);
    }
    testdb.commit();
  }
```

### zipf

A zipf distribution with an exponent of 1.2 on the total number of inserted rows is used to compute which rows to update with a batch of 10,000 rows. Note that this results in duplicates -- the duplicates are passed to the database for resolution.

```rust
   for id in (0..N) {
    let key = zipf(1.2, N);
    let hashed_key = sha256(key.to_ne_bytes());
    let hashed_data = sha256(id.to_ne_bytes());
    testdb.upsert(hashed_key, hashed_data);
    if id % 10000 == 9999 {
      testdb.commit();
    }
   }
  
```

### single

This test repeatedly updates the first row.

```rust
  let hashed_key = sha256(0.to_ne_bytes());
  for id in (0..) {
    let hashed_data = sha256(id.to_ne_bytes());
    testdb.upsert(hashed_key, hashed_data);
    testdb.commit(); 
  }
```

## Usage

Since the benchmark is in two phases, you may want to create the database first and then
examine the steady-state performance second. This can easily be accomplished with a few
command line options.

To create test databases, use the following command:

```sh
    nohup time cargo run --profile maxperf --bin benchmark -- -n 10000 create
```

Then, you can look at nohup.out and see how long the database took to initialize. Then, to run
the second phase, use:

```sh
    nohup time cargo run --profile maxperf --bin benchmark -- -n 10000 zipf
```

If you're looking for detailed logging, there are some command line options to enable it. For example, to enable debug logging for the single benchmark, you can use the following:

```sh
    cargo run --profile release --bin benchmark -- -l debug -n 10000 single
```

## Using opentelemetry

To use the opentelemetry server and record timings, just run a docker image that collects the data using:

```sh
docker run   -p 127.0.0.1:4318:4318   -p 127.0.0.1:55679:55679   otel/opentelemetry-collector-contrib:0.97.0   2>&1
```

Then, pass the `-e` option to the benchmark.
