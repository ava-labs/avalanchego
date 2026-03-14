# Firewood Benchmark

Welcome to the Firewood Benchmark! This repository contains the benchmarking
code and resources for the Firewood project.

## Table of Contents

- [Introduction](#introduction)
- [Benchmark Description](#benchmark-description)
- [Installation](#installation)
- [Usage](#usage)

## Introduction

The Firewood Benchmark is a performance testing suite designed to evaluate the performance of the Firewood project. It includes a set of benchmarks that measure various aspects of the project's performance, such as execution time, memory usage, and scalability.

## Benchmark Description

There are currently three different benchmarks, as follows:

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

## Installation

To install the Firewood Benchmark, follow these steps:

1. Install the build prerequisites: `sudo bash setup-scripts/build-environment.sh`
2. \[Optional\] Install grafana: `sudo bash setup-scripts/install-grafana.sh`
3. Build firewood: `bash setup-scripts/build-firewood.sh`
4. Create the benchmark database: `nohup time cargo run --profile maxperf --bin benchmark -- create`. For a larger database, add `--number-of-batches=10000` before the subcommand 'create' for a 100M row database (each batch by default is 10K rows). Additional options are documented in setup-scripts/run-benchmarks.sh

If you want to install grafana and prometheus on an AWS host (using Ubuntu as a base), do the following:

1. Log in to the AWS EC2 console
2. Launch a new instance:
   1. Name: Firewood Benchmark
   2. Click on 'Ubuntu' in the quick start section
   3. Set the instance type to m5d.2xlarge
   4. Set your key pair
   5. Open SSH, HTTP, and port 3000 so prometheus can be probed remotely (we use a 'firewood-bench' security group)
   6. Configure storage to 20GiB disk
   7. \[optional] Save money by selecting 'spot instance' in advanced
   8. Launch the instance
3. ssh ubuntu@AWS-IP
4. Run the scripts as described above, including the grafana installation.
5. Log in to Grafana at <http://YOUR-AWS-IP>
   a. Username: `admin`, password: `firewood_is_fast`
6. A Prometheus data source, the Firewood dashboard, and a [system statistics dashboard](https://grafana.com/grafana/dashboards/1860-node-exporter-full/) are preconfigured and ready to use.

### Updating the dashboard

If you want to update the dashboard and commit it, do not enable "Export for sharing externally" when exporting. These dashboards are provisioned and reference the default Prometheus data source, with the option to pick from available data sources.

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
