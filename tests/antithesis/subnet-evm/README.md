# Subnet-EVM Antithesis Test Setup

This directory contains the antithesis test setup for subnet-evm, consolidated
at the root level to match the pattern used by avalanchego and xsvm test setups.

## Overview

This package supports testing with
[Antithesis](https://antithesis.com/docs/),
a SaaS offering that enables deployment of distributed systems (such
as Avalanche) to a deterministic and simulated environment that
enables discovery and reproduction of anomalous behavior.

See avalanchego's
[documentation](https://github.com/ava-labs/avalanchego/blob/master/tests/antithesis/README.md)
for more details.

## Build Images

From repository root:
```bash
task build-antithesis-images-subnet-evm
```

Or using environment variable:
```bash
TEST_SETUP=subnet-evm ./scripts/build_antithesis_images.sh
```

## Test Images

```bash
task test-build-antithesis-images-subnet-evm
```

## Related Files

- **Scripts**: `scripts/build_antithesis_subnet_evm_workload.sh`
- **Test Script**: `scripts/tests.build_antithesis_images_subnet_evm.sh`
- **Workflows**: `.github/workflows/publish_antithesis_images.yml`

## Legacy Location

The original test setup in `graft/subnet-evm/tests/antithesis/` is preserved
for backward compatibility and as a reference. Both locations are functional.
