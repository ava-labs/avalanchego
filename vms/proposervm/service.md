# ProposerVM API

The ProposerVM API allows clients to fetch information about a chain's Snowman++ wrapper information.

## Endpoint

```text
/ext/bc/{blockchainID}/proposervm
```

## Format

This API uses the `JSON-RPC 2.0` RPC format.

## Methods

### `proposervm.getProposedHeight`

Returns this node's current proposer VM height.

**Signature:**

```
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

### `proposervm.getCurrentEpoch`

Returns the current epoch information.

**Signature:**

```
proposervm.getCurrentEpoch() ->
{
  number: int,
  startTime: int,
  pChainHeight: int
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "proposervm.getCurrentEpoch",
    "params": {},
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P/proposervm
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "number": "56",
    "startTime":"1755802182",
    "pChainHeight": "21857141"
  },
  "id": 1
}
```
