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

## Examples

* fwdctl create

```sh
# Check available options when creating a database, including the defaults.
$ fwdctl create -h
# Create a new, blank instance of firewood using the default name "firewood.db".
$ fwdctl create firewood.db
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
