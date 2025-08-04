

# Predicate

This package contains the predicate data structure and its encoding and helper functions to unpack/pack the data structure.

## Encoding

A byte slice of size N is encoded as:

1. Slice of N bytes
2. Delimiter byte `0xff`
3. Appended 0s to the nearest multiple of 32 bytes


## Results

`predicate_results.go` defines how to encode `PredicateResults` within the block header's `Extra` data field.

For more information on the motivation for encoding the results of predicate verification within a block, see [here](../../../vms/platformvm/warp/README.md#processing-historical-avalanche-interchain-messages).

### Serialization

Note: PredicateResults are encoded using the AvalancheGo codec, which serializes a map by serializing the length of the map as a uint32 and then serializes each key-value pair sequentially.

PredicateResults:
```
+---------------------+----------------------------------+-------------------+
|             codecID :                           uint16 |           2 bytes |
+---------------------+----------------------------------+-------------------+
|             results :  map[[32]byte]TxPredicateResults | 4 + size(results) |
+---------------------+----------------------------------+-------------------+
                                                         | 6 + size(results) |
                                                         +-------------------+
```

- `codecID` is the codec version used to serialize the payload and is hardcoded to `0x0000`
- `results` is a map of transaction hashes to the corresponding `TxPredicateResults`

TxPredicateResults
```
+--------------------+---------------------+------------------------------------+
| txPredicateResults : map[[20]byte][]byte | 4 + size(txPredicateResults) bytes |
+--------------------+---------------------+------------------------------------+
                                           | 4 + size(txPredicateResults) bytes |
                                           +------------------------------------+
```

- `txPredicateResults` is a map of precompile addresses to the corresponding byte array returned by the predicate

#### Examples

##### Empty Predicate Results Map

```
// codecID
0x00, 0x00,
// results length
0x00, 0x00, 0x00, 0x00
```

##### Predicate Map with a Single Transaction Result

```
// codecID
0x00, 0x00,
// Results length
0x00, 0x00, 0x00, 0x01,
// txHash (key in results map)
0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 
// TxPredicateResults (value in results map)
// TxPredicateResults length
0x00, 0x00, 0x00, 0x01,
// precompile address (key in TxPredicateResults map)
0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00,
// Byte array results (value in TxPredicateResults map)
// Length of bytes result
0x00, 0x00, 0x00, 0x03,
// bytes
0x01, 0x02, 0x03
```

##### Predicate Map with Two Transaction Results

```
// codecID
0x00, 0x00,
// Results length
0x00, 0x00, 0x00, 0x02,
// txHash (key in results map)
0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 
// TxPredicateResults (value in results map)
// TxPredicateResults length
0x00, 0x00, 0x00, 0x01,
// precompile address (key in TxPredicateResults map)
0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00,
// Byte array results (value in TxPredicateResults map)
// Length of bytes result
0x00, 0x00, 0x00, 0x03,
// bytes
0x01, 0x02, 0x03
// txHash2 (key in results map)
0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 
// TxPredicateResults (value in results map)
// TxPredicateResults length
0x00, 0x00, 0x00, 0x01,
// precompile address (key in TxPredicateResults map)
0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00,
// Byte array results (value in TxPredicateResults map)
// Length of bytes result
0x00, 0x00, 0x00, 0x03,
// bytes
0x01, 0x02, 0x03
```

#### Maximum Size

Results has a maximum size of 1MB enforced by the codec. The actual size depends on how much data the Precompile predicates may put into the results, the gas cost they charge, and the block gas limit.

The Results maximum size should comfortably exceed the maximum value that could happen in practice, so that a correct block builder will not attempt to build a block and fail to marshal the predicate results using the codec.

We make this easy to reason about by assigning a minimum gas cost to the `PredicateGas` function of precompiles. In the case of Warp, the minimum gas cost is set to 200k gas, which can lead to at most 32 additional bytes being included in Results.

The additional bytes come from the transaction hash (32 bytes), length of tx predicate results (4 bytes), the precompile address (20 bytes), length of the bytes result (4 bytes), and the additional byte in the results bitset (1 byte). This results in 200k gas contributing a maximum of 61 additional bytes to Result.

For a block with a maximum gas limit of 100M, the block can include up to 500 validated predicates based contributing to the size of Result. At 61 bytes / validated predicate, this yields ~30KB, which is well short of the 1MB cap.
