# statehistory: flat state history for the Firewood state scheme

Temporary fork until SAE-VM; delete when PR 5624 lands.

## What it is

A pruned Firewood node keeps only the current state. With this package
enabled, every accepted block's state changes (account and storage values,
keyed by the same hashed keys Firewood uses) are also written as flat rows to
a separate pebble database at `<chain data dir>/statehistory`. Historical
`eth_call`, `eth_getBalance`, `eth_getStorageAt` and `eth_getCode` reads at
any past height are served from those rows without re-execution and without
retaining historical trie nodes. A stock archival subnet-evm node grows at
roughly 13 KB per transaction; the flat rows are about 20 times smaller.

Rows for a block are flushed in one synced batch inside `TrieDB.Commit`,
immediately before the Firewood proposal commit, so history is never behind
the durable Firewood state. Crash replay rewrites identical rows.

## Config

Chain config (`config.json` of the chain):

```json
{
  "state-scheme": "firewood",
  "pruning-enabled": true,
  "state-sync-enabled": false,
  "state-history-enabled": true
}
```

Off by default. Without `state-history-enabled` the node behaves exactly like
stock. The flag requires the Firewood scheme with pruning (an archive Firewood
node already retains every revision), rejects state sync, and refuses to start
when the store does not cover `[genesis, lastAccepted]`: the chain must be
synced from genesis with the flag set. Existing databases are not migrated.

Unsupported over history: `eth_getProof` and the trie-iterating debug RPCs
(they fail loud).

## Base commit and rebase

Branch `containerman17/deforest-subnetevm-fork`, based on `origin/master` at
`e9a8b75024` (2026-09-09). The feature is three commits on top of the base:
the package and Firewood hooks, the subnet-evm wiring, and this README. To
move to a new subnet-evm release: `git rebase --onto <new base> e9a8b75024`,
resolve conflicts in the touched files (see `git log --stat` of the three
commits), then run `go test ./graft/evm/firewood/... ./graft/subnet-evm/core/`
and the 60 second fuzz below.

```sh
go test ./graft/evm/firewood/ -run '^$' -fuzz FuzzHistoryMatchesHashDB -fuzztime 60s
```

## Known issue: memory growth

Firewood through the Go FFI grew resident memory over hours in a mainnet run.
The deployed node used a 12 hour restart cron. This fork does not change
Firewood; plan the same restart cron.
