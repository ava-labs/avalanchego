# Setup Scripts

This directory contains the scripts needed to set up the firewood benchmarks, as follows:

```bash
sudo bash build-environment.sh
```

This script sets up the build environment, including installing the firewood build dependencies.

```bash
sudo bash install-grafana.sh
```

This script sets up grafana to listen on port 3000 for firewood. It also sets up listening
for coreth as well, on port 6060, with the special metrics path coreth expects.

```bash
bash build-firewood.sh
```

This script checks out and builds firewood. It assumes you have already set up the build environment earlier.

The final script, `run-benchmarks.sh`, is a set of commands that can be copied/pasted to run individual
benchmarks of different sizes.
