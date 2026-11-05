# customtypes

The C-Chain's extra header, body and account payloads, registered with libevm. `HeaderExtra` holds the header fields Avalanche adds to the go-ethereum header. Its RLP encoding defines every C-Chain block hash, so historical encodings MUST NOT change.

## Adding a header field

New fields are always appended at the end of the header's RLP list, as an optional element that older headers omit.

### 1. Add the field to `HeaderExtra`

```go
FooExponent *dynamic.FooExponent `json:"fooExponent"`
```

- The field must be a `*big.Int` or a pointer to an unsigned integer type. A nil value means the upgrade that introduces the field is not active.
- The `json` tag is the field's JSON key. The keys are part of the RPC API and MUST NOT change.

That's all that JSON decoding, `PostRPCMarshal` and `PostCopy` need. They walk `HeaderExtra`'s tagged fields by reflection, writing a `*big.Int` as `hexutil.Big` and any other pointer as `hexutil.Uint64`.

### 2. Append it to `rlpFields`

Add a pointer to the field at the end of `Optional` in `HeaderExtra.rlpFields`. This list is the header wire format. It is written out by hand, not derived from the struct, so reordering `HeaderExtra`'s fields can't change block hashes. The field's pointer type needs a case in `writeRLPField` and `readRLPField`; both panic on a type they don't handle.

### 3. Update the tests

- `headerWithNonZeroFields`: set the field. `TestHeaderWithNonZeroFields` fails until you do, and the round-trip tests then cover it.
- `TestHeaderRLP`: the golden hex encodes the all-fields header, so it changes. Replace it with the encoding the failing test prints, after checking that only the bytes for the new field were appended.
- `forkHeaders`: add an entry for the upgrade, setting the field and returning its expected RLP value. The existing entries MUST still pass unchanged: they pin the encoding of every historical fork.
- `TestHeaderRPCMarshal`: add the field's key and value to the all-fields golden.

If you skip step 1's tag, the JSON round-trip tests fail. If you skip step 2, the RLP round-trip tests fail.

Building and verifying the field's value happens outside this package, in `vms/saevm/cchain`.
