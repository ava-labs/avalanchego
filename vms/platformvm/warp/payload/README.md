# Payload

An Avalanche Unsigned Warp Message already includes a `networkID`, `sourceChainID`, and `payload` field. The `payload` field can be parsed into one of the types included in this package to be further handled by the VM.

## Hash

Hash:
```
+-----------------+----------+-----------+
|         codecID :   uint16 |   2 bytes |
+-----------------+----------+-----------+
|          typeID :   uint32 |   4 bytes |
+-----------------+----------+-----------+
|            hash : [32]byte |  32 bytes |
+-----------------+----------+-----------+
                             |  38 bytes |
                             +-----------+
```

- `codecID` is the codec version used to serialize the payload and is hardcoded to `0x0000`
- `typeID` is the payload type identifier and is `0x00000000` for `Hash`
- `hash` is a hash from the `sourceChainID`. The format of the expected preimage is chain specific. Some examples for valid hash values are:
  - root of a merkle tree
  - accepted block hash on the source chain
  - accepted transaction hash on the source chain

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
- `typeID` is the payload type identifier and is `0x00000001` for `AddressedCall`
- `sourceAddress` is the address that sent this message from the source chain
- `payload` is an arbitrary byte array payload
