## Deploying Devnets

_In the world of Avalanche, we refer to short-lived, test Subnets as Devnets._

See [`tokenvm/DEVNETS.md`](https://github.com/ava-labs/hypersdk/blob/main/examples/tokenvm/DEVNETS.md) for subnet/custom VM testing in the dev net.

### Step 1: Install `avalanche-ops`

[avalanche-ops](https://github.com/ava-labs/avalanche-ops) provides a command-line
interface to setup nodes and install Custom VMs on both custom networks and on
Fuji using [AWS CloudFormation](https://aws.amazon.com/cloudformation/).

You can view a full list of pre-built binaries on the [latest release
page](https://github.com/ava-labs/avalanche-ops/releases/tag/latest).

#### Option 1: Install `avalanche-ops` on Mac

```bash
rm -f /tmp/avalancheup-aws
wget "https://github.com/ava-labs/avalanche-ops/releases/download/latest/avalancheup-aws.aarch64-apple-darwin"
mv ./avalancheup-aws.aarch64-apple-darwin /tmp/avalancheup-aws
chmod +x /tmp/avalancheup-aws
/tmp/avalancheup-aws --help
```

#### Option 2: Install `avalanche-ops` on Linux

```bash
rm -f /tmp/avalancheup-aws
wget "https://github.com/ava-labs/avalanche-ops/releases/download/latest/avalancheup-aws.x86_64-linux-gnu"
mv ./avalancheup-aws.x86_64-linux-gnu /tmp/avalancheup-aws
chmod +x /tmp/avalancheup-aws
/tmp/avalancheup-aws --help
```

### Step 2. Configure AWS

Next, install the [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html).
Once you've installed the AWS CLI, run `aws configure` to set the access key to
use while deploying your devnet.
And run the following command to make sure the credential is properly set up:

```bash
aws sts get-caller-identity
```

### Step 3: Plan Local Network Deploy

Now we can spin up a new network of 6 nodes with some defaults:
- `avalanche-ops` supports [Graviton-based processors](https://aws.amazon.com/ec2/graviton/) (ARM64). Use `--arch-type arm64` to run nodes in ARM64 CPUs.
- `avalanche-ops` supports [EC2 Spot instances](https://aws.amazon.com/ec2/spot/) for cost savings. Use `--instance-mode=spot` to run instances in spot mode.
- `avalanche-ops` supports multi-region deployments. Use `--auto-regions 3` to distribute node across 3 regions.

```bash
/tmp/avalancheup-aws default-spec \
--arch-type amd64 \
--rust-os-type ubuntu20.04 \
--anchor-nodes 3 \
--non-anchor-nodes 3 \
--regions us-west-2 \
--instance-mode=on-demand \
--instance-types=c5.4xlarge \
--ip-mode=elastic \
--metrics-fetch-interval-seconds 60 \
--network-name custom \
--keys-to-generate 5
```

The `default-spec` (and `apply`) command only provisions nodes, not Custom VMs.
The Subnet and Custom VM installation are done via `install-subnet-chain` sub-commands that follow.

For instance, to spin up a 100-node cluster:

```bash
--anchor-nodes 10 \
--non-anchor-nodes 90 \
```

To set up a load balancer for all nodes,

```bash
--enable-nlb
```

The default command above will download the `avalanchego` public release binaries from GitHub.
To test your own binaries, use the following flags to upload to S3. These binaries must be built
for the target remote machine platform (e.g., build for `aarch64` and Linux to run them in
Graviton processors):

```bash
--upload-artifacts-aws-volume-provisioner-local-bin ${AWS_VOLUME_PROVISIONER_BIN_PATH} \
--upload-artifacts-aws-ip-provisioner-local-bin ${AWS_IP_PROVISIONER_BIN_PATH} \
--upload-artifacts-avalanche-telemetry-cloudwatch-local-bin ${AVALANCHE_TELEMETRY_CLOUDWATCH_BIN_PATH} \
--upload-artifacts-avalanched-aws-local-bin ${AVALANCHED_AWS_BIN_PATH} \
--upload-artifacts-avalanchego-local-bin ${AVALANCHEGO_BIN_PATH} \
```

It is recommended to specify your own artifacts to avoid flaky github release page downloads.

*SECURITY*: By default, the SSH and HTTP ports are open to public. Once you complete provisioning the nodes, go to EC2 security
group to restrict the inbound rules to your IP.

*NOTE*: In rare cases, you may encounter [aws-sdk-rust#611](https://github.com/awslabs/aws-sdk-rust/issues/611)
where AWS SDK call hangs, which blocks node bootstraps. If a node takes too long to start, connect to that
instance (e..g, use SSM sesson from your AWS console), and restart the agent with the command `sudo systemctl restart avalanched-aws`.

### Step 4: Customize AvalancheGo configuration

If you are attempting to stress test the Devnet, you should disable CPU, Disk,
and Bandwidth rate limiting. You can do this by updating the following lines to
`avalanchego_config` in the spec file:

```yaml
avalanchego_config:
    ...
    proposervm-use-current-height: true
    throttler-inbound-validator-alloc-size: 107374182
    throttler-inbound-node-max-processing-msgs: 100000
    throttler-inbound-bandwidth-refill-rate: 1073741824
    throttler-inbound-bandwidth-max-burst-size: 1073741824
    throttler-inbound-cpu-validator-alloc: 100000
    throttler-inbound-disk-validator-alloc: 10737418240000
    throttler-outbound-validator-alloc-size: 107374182
    snow-mixed-query-num-push-vdr-uint: 10
    consensus-on-accept-gossip-validator-size: 0
    consensus-on-accept-gossip-non-validator-size: 0
    consensus-on-accept-gossip-peer-size: 5
    consensus-accepted-frontier-gossip-peer-size: 5
    network-compression-enabled: false
```

For example, update the following `coreth_chain_config` to enable local transaction:

```yaml
coreth_chain_config:
  ...
  local-txs-enabled: true
```

### Step 5: Apply Local Network Deploy

The `default-spec` command itself does not create any resources, and instead outputs the following `apply` and `delete` commands that you can copy and paste.

```bash
# run the following to create resources
vi /home/ubuntu/aops-custom-****-***.yaml

avalancheup-aws apply \
--spec-file-path /home/ubuntu/aops-custom-****-***.yaml

# run the following to delete resources
/tmp/avalancheup-aws delete \
--delete-cloudwatch-log-group \
--delete-s3-objects \
--delete-ebs-volumes \
--delete-elastic-ips \
--spec-file-path /home/ubuntu/aops-custom-****-***.yaml
```

That is, `apply` creates AWS resources, whereas `delete` destroys after testing is done.

Spinning up a 100-node network takes about 20 minutes from start to finish.

### Step 6: Simulate Network Upgrades

Each agent on the host (`avalanched-aws`) uses the configurations and binary artifacts on the S3 bucket as source of truths,
meaning on restart, it always overwrites the existing configuration and binary files with the ones in S3.

To upgrade the network configuration (e.g., avalanchego config, chain config), (1) simply download the spec file from S3, (2) reupload it to S3,
(3) terminate anchor nodes to let AWS EC2 autoscaling group automatically recreate the nodes while retaining the previous chain state,
and (4) terminate all remaining non-anchor nodes to restart the rest.

To upgrade the software versions (e.g., patched avalanchego), (1) upload the binaries to the same S3 path (e.g., `boostrap/install/avalanchego`),
(2) terminate anchor and non-anchor nodes in order, to automatically download the new binaries.
