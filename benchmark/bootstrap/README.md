# Bootstrap Testing Script

This directory contains tools for automated Firewood blockchain database benchmarking on AWS. The `aws-launch.sh` script creates EC2 instances, sets up the complete testing environment, and executes C-chain (Avalanche) block bootstrapping tests.

## Prerequisites

Before running the script, you'll need:

- AWS CLI installed and configured on your machine
- Authenticated AWS session: `aws sso login`
  - Your session should be configured to use the `Experimental` account.

## What It Does

The `aws-launch.sh` script automatically:

1. Launches an EC2 instance with the specified instance type and configuration
2. Sets up the environment with all necessary dependencies (Git, Rust, Go, build tools, Grafana)
3. Creates user accounts with SSH access for the team
4. Clones and builds:
   - Firewood (from specified branch or default)
   - AvalancheGo (from specified branch or default)
   - Coreth (from specified branch or default)
   - LibEVM (from specified branch or default)
5. Downloads pre-existing blockchain data from S3 (1M, 10M, or 50M blocks)
6. Executes the bootstrapping benchmark to test Firewood's performance

## Usage

```bash
./aws-launch.sh [OPTIONS]
```

For a complete list of options, run:

```bash
./aws-launch.sh --help
```

## Examples

### Run a large benchmark with spot pricing

```bash
./aws-launch.sh --instance-type i4i.xlarge --nblocks 10m --spot
```

### Test multiple component branches together

```bash
./aws-launch.sh --firewood-branch my-firewood-branch --avalanchego-branch develop --coreth-branch foo --libevm-branch bar
```

### Preview a configuration without launching

```bash
./aws-launch.sh --dry-run --firewood-branch my-branch --nblocks 1m
```

## Monitoring Results

After launching, the script outputs an instance ID. You can:

1. **SSH to the instance** - Only authorized team members (rkuris, austin, aaron, brandon, amin, bernard, rodrigo) can SSH using their configured GPG hardware keys. Note: Your GPG agent must be properly configured for SSH support on your local machine.

   ```bash
   ssh <username>@<instance-public-ip>
   ```

2. **Monitor benchmark progress**:

   ```bash
   tail -f /var/log/bootstrap.log
   ```

3. **Check build logs**:

   ```bash
   tail -f /mnt/nvme/ubuntu/firewood/build.log
   tail -f /mnt/nvme/ubuntu/avalanchego/build.log
   ```
