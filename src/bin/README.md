# fwdctl

`fwdctl` is a small CLI designed to make it easy to experiment with firewood locally. 

> Note: firewood has two separate storage areas in the database. One is a generic key-value store. 
The other is EVM-based account model storage, based on Merkle-Patricia Tries. The generic key-value
store is being written to by the cli currently. Support for commands that use the account based storage
will be supported in a future release of firewood.  

## Building locally
*Note: fwdctl is linux-only*
```
cargo build --release --bin fwdctl
```
To use
```
$ ./target/release/fwdctl -h
```

## Supported commands
* `fwdctl create`: Create a new firewood database.
* `fwdctl get`: Get the code associated with a key in the database.
* `fwdctl insert`: Insert a key/value pair into the generic key/value store.
* `fwdctl delete`: Delete a key/value pair from the database. 
* `fwdctl root`: Get the root hash of the key/value trie

## Examples
* fwdctl create
```
# Check available options when creating a database, including the defaults.
$ fwdctl create -h
# Create a new, blank instance of firewood using the default name "firewood".
$ fwdctl create
# Look inside, there are several folders representing different components of firewood, including the WAL.
$ ls firewood
```
* fwdctl get <KEY>
```
Get the value associated with a key in the database, if it exists.
fwdctl get --key <KEY>
```
* fwdctl insert --key <KEY> --value <VALUE>
```
Insert a key/value pair into the database.
fwdctl insert --key <KEY> --value <VALUE>
```
* fwdctl delete <KEY>
```
Delete a key from the database, along with the associated value.
fwdctl delete --key <KEY>
```

