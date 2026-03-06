# Setup Scripts

This directory contains the scripts needed to set up the firewood benchmarks, as follows:

```bash
sudo bash build-environment.sh
```

This script sets up the build environment, including installing the firewood build dependencies.

By default, it sets the bytes-per-inode to 2097152 (2MB) when creating the ext4 filesystem. This default works well for workloads that create many small files (such as LevelDB with AvalancheGo).

If you're not using LevelDB (for example, just using Firewood without AvalancheGo), you don't need as many inodes, which gives you more room for the database itself. In this case, you can and should use a larger value with the `--bytes-per-inode` option:

```bash
sudo bash build-environment.sh --bytes-per-inode 6291456
```

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
