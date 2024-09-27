# Firewood Benchmark

Welcome to the Firewood Benchmark! This repository contains the benchmarking code and resources for the Firewood project.

## Table of Contents
- [Introduction](#introduction)
- [Installation](#installation)
- [Usage](#usage)
- [Contributing](#contributing)
- [License](#license)

## Introduction
The Firewood Benchmark is a performance testing suite designed to evaluate the performance of the Firewood project. It includes a set of benchmarks that measure various aspects of the project's performance, such as execution time, memory usage, and scalability.

## Installation
To install the Firewood Benchmark, follow these steps:

1. Clone the repository: `git clone https://github.com/ava-labs/firewood.git`
2. Navigate to the firewood directory: `cd firewood`
3. Build the executable: `cargo build --release`
4. Execute the benchmark: `nohup time cargo run --release bin benchmark -- --test-name create`

As the benchmark is running, statistics for prometheus are availble on port 3000 (by default).

If you want to install grafana and prometheus on an AWS host (using Ubuntu as a base), do the following:

1. Log in to the AWS EC2 console
2. Launch a new instance:
  a. Name: Firewood Benchmark
  b. Click on 'Ubuntu' in the quick start section
  c. Set the instance type to m5d.2xlarge
  d. Set your key pair
  e. Check 'Allow HTTP traffic from the internet' for grafana
  f. Configure storage to 20GiB disk
  g. [optional] Save money by selecting 'spot instance' in advanced
  h. Launch the instance
3. ssh ubuntu@AWS-IP
4. Run the script in setup.sh on the instance as root
5. Log in to grafana on http://AWS-IP
  a. username: admin, password: admin
6. When prompted, change the password (firewood_is_fast)
7. On the left panel, click "Data Sources"
  a. Select "Prometheus"
  b. For the URL, use http://localhost:9090
  c. click "Save and test"
8. On the left panel, click Dashboards
  a. On the right top pulldown, click New->Import
  b. Import the dashboard from the Grafana-dashboard.json file
  c. Set the data source to Prometheus
9. You may also want to install a stock dashboard from [here](https://grafana.com/grafana/dashboards/1860-node-exporter-full/)

## Usage
Since the benchmark is in two phases, you may want to create the database first and then
examine the steady-state performance second. This can easily be accomplished with a few
command line options.

To pre-create the database, use the following command:

```
nohup time cargo run --release --bin benchmark -- --test-name create
```

Then, you can look at nohup.out and see how long the database took to initialize. Then, to run
the second phase, use:

```
nohup time cargo run --release --bin benchmark -- --assume-preloaded-rows=1000000000
```
