# fwdctl

`fwdctl` is a small CLI designed to make it easy to experiment with firewood locally. 

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
* `fwdctl get`: Get the code associated with a key in the database

## Examples
* fwdctl create
```
# Check available options when creating a database, including the defaults
$ fwdctl create -h
# Create a new, blank instance of firewood with a custom name
$ fwdctl create --name=my-db
# Look inside, there are several folders representing different components of firewood, including the WAL
$ ls my-db
```
* fwdctl get <KEY>


