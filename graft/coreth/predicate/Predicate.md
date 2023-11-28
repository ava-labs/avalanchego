# Predicate

This package contains the predicate data structure and its encoding and helper functions to unpack/pack the data structure.

## Encoding

A byte slice of size N is encoded as:

1. Slice of N bytes
2. Delimiter byte `0xff`
3. Appended 0s to the nearest multiple of 32 bytes
