# Firewood Benchmark

Welcome to the Firewood Benchmark! This repository contains the benchmarking code and resources for the Firewood project.

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
3. `single` which only updates a single row (row 1) repeatedly in a tiny transactoin

There is also a `create` benchmark which creates the database to begin with. The defaults will create
a 10M row database. If you want a larger one, increase the number of batches.

## Installation

To install the Firewood Benchmark, follow these steps:

1. Clone the repository: `git clone https://github.com/ava-labs/firewood.git`
2. Navigate to the firewood directory: `cd firewood`
3. Build the executable: `cargo build --release`
4. Create the benchmark database: `nohup time cargo run --profile maxperf --bin benchmark -- create`. For a larger database, add `--number-of-batches=10000` for a 100M row database (each batch by default is 10K rows) before the subcommand 'create'.
5. \[Optional] Save the benchmark database in rev_db
6. Run the benchmark you want: `nohup time cargo run --profile maxperf --bin benchmark -- NAME` (selecting NAME from the list above). If you're not using the default database size, make sure you specify the number of batches here as well.

As the benchmark is running, statistics for prometheus are availble on port 3000 (by default).

If you want to install grafana and prometheus on an AWS host (using Ubuntu as a base), do the following:

1. Log in to the AWS EC2 console
2. Launch a new instance:
  a. Name: Firewood Benchmark
  b. Click on 'Ubuntu' in the quick start section
  c. Set the instance type to m5d.2xlarge
  d. Set your key pair
  e. Open SSH, HTTP, and port 3000 so prometheus can be probed remotely (we use a 'firewood-bench' security group)
  f. Configure storage to 20GiB disk
  g. \[optional] Save money by selecting 'spot instance' in advanced
  h. Launch the instance
3. ssh ubuntu@AWS-IP
4. Run the script in setup.sh on the instance as root
5. Log in to grafana on <http://YOUR-AWS-IP>
  a. username: admin, password: admin
6. When prompted, change the password (firewood_is_fast)
7. On the left panel, click "Data Sources"
  a. Select "Prometheus"
  b. For the URL, use <http://localhost:9090>
  c. click "Save and test"
8. On the left panel, click Dashboards
  a. On the right top pulldown, click New->Import
  b. Import the dashboard from the Grafana-dashboard.json file
  c. Set the data source to Prometheus
9. \[optional] Install a stock dashboard from [here](https://grafana.com/grafana/dashboards/1860-node-exporter-full/)

## Usage

Since the benchmark is in two phases, you may want to create the database first and then
examine the steady-state performance second. This can easily be accomplished with a few
command line options.

To pre-create the database, use the following command:

```sh
nohup time cargo run --profile maxperf --bin benchmark -- create
```

Then, you can look at nohup.out and see how long the database took to initialize. Then, to run
the second phase, use:

```sh
nohup time cargo run --profile maxperf --bin benchmark -- -zipf
```

If you're looking for detailed logging, there are some command line options to enable it. For example, to enable debug logging for the single benchmark, you can use the following:

```sh
cargo run --profile release --bin benchmark -- -l debug single
```
