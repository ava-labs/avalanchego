# fwdctl

`fwdctl` is a small CLI designed to make it easy to experiment with firewood locally.

## Building locally

```sh
cargo build --release --bin fwdctl
```

To use

```sh
./target/release/fwdctl -h
```

## Supported commands

* `fwdctl create`: Create a new firewood database.
* `fwdctl get`: Get the code associated with a key in the database.
* `fwdctl insert`: Insert a key/value pair into the generic key/value store.
* `fwdctl delete`: Delete a key/value pair from the database.
* `fwdctl root`: Get the root hash of the key/value trie.
* `fwdctl dump`: Dump the contents of the key/value store.
* `fwdctl launch` (requires `--features launch`): Launch and manage AWS benchmark runs.

## Launch command

`fwdctl launch` provisions and manages EC2 instances for benchmark workflows.

Build with launch support:

```sh
cargo build --release --bin fwdctl --features launch
```

Then inspect command help:

```sh
./target/release/fwdctl launch -h
```

For full launch usage, defaults, and scenario configuration, see [README.launch.md](./README.launch.md).

## Examples

* fwdctl create

```sh
# Check available options when creating a database, including the defaults.
$ fwdctl create -h
# Create a new, blank instance of firewood using the default directory name "firewood".
$ fwdctl create firewood
```

* fwdctl get KEY

```sh
# Get the value associated with a key in the database, if it exists.
fwdctl get KEY
```

* fwdctl insert KEY VALUE

```sh
# Insert a key/value pair into the database.
fwdctl insert KEY VALUE
```

* fwdctl delete KEY

```sh
# Delete a key from the database, along with the associated value.
fwdctl delete KEY
```
