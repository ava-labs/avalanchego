# Firewood Benchmark

Welcome to the Firewood Benchmark repository! This repository contains the benchmarking code and resources for the Firewood project.

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
4. Execute the benchmark: `nohup time cargo run --release bin benchmark`

As the benchmark is running, statistics for prometheus are availble on port 3000 (by default).

If you want to install grafana and prometheus on an AWS host (using Ubuntu as a base), do the following:

```

```


## Usage
Since the benchmark is in two phases, you may want to create the database first and then
examine the steady-state performance second. This can easily be accomplished with a few
command line options.

To pre-create the database, use the following command:

```
nohup time cargo run --release --bin benchmark -- --initialize-only
```

Then, you can look at nohup.out and see how long the database took to initialize. Then, to run
the second phase, use:

```
nohup time cargo run --release --bin benchmark -- --assume-preloaded-rows=1000000000
```


## Contributing
We welcome contributions to the Firewood Benchmark repository! If you would like to contribute, please follow our [contribution guidelines](CONTRIBUTING.md).

## License
The Firewood Benchmark is open source software licensed under the [MIT License](LICENSE).
