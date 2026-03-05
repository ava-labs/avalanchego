# fwdctl launch

`fwdctl launch` runs benchmark workflows on AWS EC2. It also provides commands
to monitor, list, and terminate the instances it manages.

## Build and help

The launch subcommand is behind the `launch` feature flag.

```sh
cargo build --release --bin fwdctl --features launch
./target/release/fwdctl launch -h
./target/release/fwdctl launch deploy -h
```

## AWS prerequisites

Before using launch commands, make sure all of the following are in place.

1. Install AWS CLI.
   See the official docs:
   [AWS CLI install guide](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html).
2. Configure AWS credentials in your shell environment.
   For Okta-based SSO:
   - Sign in through Okta and open the AWS accounts page. It's important to go through Okta homepage to get to access portal.
   - Select the account you want.
   - Click **Access keys** next to the role.
   - Run `aws configure sso` and enter the values from the modal.
   - Optionally rename that profile to `default` in `~/.aws/config`, or export
     `AWS_PROFILE=<profile-name>` in your shell config.
   You can also follow the AWS SSO profile guide:
   [SSO profile setup](https://docs.aws.amazon.com/cli/latest/userguide/sso-configure-profile-token.html#sso-configure-profile-token-auto-sso).
3. Ensure your target region has a security group you plan to use.
   The default security group is in `us-west-2` and allows SSH access.
   To create a similar group from CLI:

   ```sh
   export REGION=<your-region>

   # get default VPC
   DEFAULT_VPC=$(aws ec2 describe-vpcs \
     --region "$REGION" \
     --filters "Name=isDefault,Values=true" \
     --query "Vpcs[0].VpcId" \
     --output text)

   # create security group
   SG_ID=$(aws ec2 create-security-group \
     --region "$REGION" \
     --group-name fwdctl-launch-sg \
     --description "Security group for SSH access of fwdctl/launch instances" \
     --vpc-id "$DEFAULT_VPC" \
     --query GroupId \
     --output text)

   # allow SSH inbound from anywhere
   aws ec2 authorize-security-group-ingress \
     --region "$REGION" \
     --group-id "$SG_ID" \
     --protocol tcp \
     --port 22 \
     --cidr 0.0.0.0/0
   ```

4. Confirm your IAM identity can call the APIs used by launch flows (STS, SSM,
   and EC2). These checks should succeed:

   ```sh
   # should print your caller identity
   aws sts get-caller-identity

   # should return managed instances (or an empty list)
   aws ssm describe-instance-information --region <region>

   # should return EC2 instances (or an empty list)
   aws ec2 describe-instances --region <region>

   # should fail with DryRunOperation if permissions are correct
   aws ec2 run-instances \
     --image-id resolve:ssm:/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64 \
     --instance-type t2.micro \
     --region us-west-2 \
     --dry-run
   ```

5. Confirm the IAM instance profile you pass to deploy has the right permissions.
   The default should work. If you create a new one, attach both:
   `AmazonS3ReadOnlyAccess` and `AmazonSSMManagedInstanceCore`.
   Example:

   ```sh
   # create IAM role
   aws iam create-role \
     --role-name <new-role-name> \
     --assume-role-policy-document '{
       "Version": "2012-10-17",
       "Statement": [
         {
           "Effect": "Allow",
           "Principal": {
             "Service": "ec2.amazonaws.com"
           },
           "Action": "sts:AssumeRole"
         }
       ]
     }'

   # attach S3 read-only policy
   aws iam attach-role-policy \
     --role-name <role> \
     --policy-arn arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess

   # attach SSM policy
   aws iam attach-role-policy \
     --role-name <role> \
     --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore

   # create instance profile
   aws iam create-instance-profile \
     --instance-profile-name <instance-profile-name>

   # add role to profile
   aws iam add-role-to-instance-profile \
     --instance-profile-name <profile> \
     --role-name <role>
   ```

## Quick start

1. Preview the launch plan without creating resources:

   ```sh
   ./target/release/fwdctl launch deploy --dry-run
   ```

   If you are not using the `default` profile, prefix the command:

   ```sh
   AWS_PROFILE=<profile> ./target/release/fwdctl launch deploy --dry-run
   ```

2. Deploy and follow cloud-init progress:

   ```sh
   ./target/release/fwdctl launch deploy \
     --scenario reexecute \
     --nblocks 1m \
     --config firewood \
     --follow follow-with-progress
   ```

3. List instances launched through `fwdctl`:

   ```sh
   ./target/release/fwdctl launch list --running --mine
   ```

4. Reattach monitoring for one instance:

   ```sh
   ./target/release/fwdctl launch monitor i-0123456789abcdef0 --observe
   ```

5. Terminate one or more managed instances:

   ```sh
   ./target/release/fwdctl launch kill i-0123456789abcdef0 -y
   ./target/release/fwdctl launch kill --mine -y
   ```

## Deploy defaults

`launch deploy` uses defaults so you can get started with fewer flags:

| Option | Default |
| --- | --- |
| `--instance-type` | `i4g.large` |
| `--nblocks` | `1m` |
| `--scenario` | `reexecute` |
| `--config` | `firewood` |
| `--metrics-server` | `true` |
| `--region` | `us-west-2` |
| `--sg` | `sg-0ac5ceb1761087d04` |
| `--iam-instance-profile` | `s3-readonly-with-ssm` |
| `--name-prefix` | `fw` |

Useful optional controls:

- `--dry-run` (`plan` or `plan-with-cloud-init`) to preview without creating resources
- `--follow` (`follow` or `follow-with-progress`) to stream stage logs after deploy
- `--firewood-branch`, `--avalanchego-branch`, and `--libevm-branch` to test
  branch combinations
- `--tag` to mark instances with a custom value (`CustomTag`)
- `--variable KEY=VALUE` to override `variables.<key>` in stage templates
  (repeatable, applied last, last value wins)

```sh
# override stage variables (repeatable)
./target/release/fwdctl launch deploy \
  --variable nvme_base=/data/nvme \
  --variable s3_bucket=my-bootstrap-bucket
```

## Stage scenarios and config

By default, `fwdctl launch` uses the embedded stage config from:

- `benchmark/launch/launch-stages.yaml`

You can override this at runtime (without rebuilding) by creating:

- `~/.config/fwdctl/launch-stages.yaml`

Current embedded scenarios:

- `reexecute`
- `replay-log-gen`
- `snapshotter`

## Operational behavior

- Managed instances are tagged with `ManagedBy=fwdctl`
- Only managed instances are targeted by `launch list` and `launch kill`
- `launch list` marks instances launched by your current AWS identity with `*`
- `launch kill` requires explicit confirmation unless `-y`/`--yes` is provided.
