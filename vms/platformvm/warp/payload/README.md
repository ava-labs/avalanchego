# Payload

An Avalanche Unsigned Warp Message already includes a `networkID`, `sourceChainID`, and `payload` field. The `payload` field is parsed into one of the types included in this package to be further handled by the VM.

## AddressedCall

AddressedCall:
```
+---------------------+--------+----------------------------------+
|             codecID : uint16 |                          2 bytes |
+---------------------+--------+----------------------------------+
|              typeID : uint32 |                          4 bytes |
+---------------------+--------+----------------------------------+
|       sourceAddress : []byte |                 4 + len(address) |
+---------------------+--------+----------------------------------+
|             payload : []byte |                 4 + len(payload) |
+---------------------+--------+----------------------------------+
                               | 14 + len(payload) + len(address) |
                               +----------------------------------+
```

- `codecID` is the codec version used to serialize the payload and is hardcoded to `0x0000`
- `typeID` is the payload type identifier and is `0x00000000` for `AddressedCall`
- `sourceAddress` is the address that sent this message from the source chain
- `payload` is an arbitrary byte array payload

## BlockHash

BlockHash:
```
+-----------------+----------+-----------+
|         codecID :   uint16 |   2 bytes |
+-----------------+----------+-----------+
|          typeID :   uint32 |   4 bytes |
+-----------------+----------+-----------+
|       blockHash : [32]byte |  32 bytes |
+-----------------+----------+-----------+
                             |  38 bytes |
                             +-----------+
```

- `codecID` is the codec version used to serialize the payload and is hardcoded to `0x0000`
- `typeID` is the payload type identifier and is `0x00000001` for `BlockHash`
- `blockHash` is a blockHash from the `sourceChainID`. A signed block hash payload indicates that the signer has accepted the block on the source chain.
