# Predicate Utils

This package provides simple helpers to pack/unpack byte slices for a predicate transaction, where a byte slice of size N is encoded in the access list of a transaction.

## Encoding

A byte slice of size N is encoded as:

1. Slice of N bytes
2. Delimiter byte `0xff`
3. Appended 0s to the nearest multiple of 32 bytes
