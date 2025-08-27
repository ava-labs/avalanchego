# ProposerVM API

The ProposerVM API allows clients to fetch information about a Snowman++ chain's ProposerVM.

## Endpoint

```text
/ext/bc/{blockchainID}/proposervm
```

## Format

This API uses the `json 2.0` RPC format.

## Methods

### `proposervm.getProposedHeight`

Returns this node's current proposer VM height.

**Signature:**

```go
proposervm.getProposedHeight() ->
{
  height: int,
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "proposervm.getProposedHeight",
    "params": {},
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P/proposervm
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "height": "56"
  },
  "id": 1
}
```
