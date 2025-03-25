# Mempool Gossiping

The PlatformVM has a mempool which tracks unconfirmed transactions that are waiting to be issued into blocks. The mempool is volatile, i.e. it does not persist unconfirmed transactions.

In conjunction with the introduction of [Snowman++](../../proposervm), the mempool was opened to the network, allowing the gossiping of local transactions as well as the hosting of remote ones.

## Mempool Gossiping Workflow

The PlatformVM's mempool performs the following workflow:

- An unconfirmed transaction is provided to `node A`, either through mempool gossiping or direct issuance over an RPC. If this transaction isn't already in the local mempool, the transaction is issued into the mempool.
- When `node A` issues a new transaction into its mempool, it will gossip the transaction ID by sending an `AppGossip` message. The node's engine will randomly select peers (currently defaulting to `6` nodes) to send the `AppGossip` message to.
- When `node B` learns about the existence of a remote transaction ID, it will check if its mempool contains the transaction or if it has been recently dropped. If the transaction ID is not known, `node B` will generate a new `requestID` and respond with an `AppRequest` message with the unknown transaction's ID. `node B` will track the content of the request issued with `requestID` for response verification.
- Upon reception of an `AppRequest` message, `node A` will attempt to fetch the transaction requested in the `AppRequest` message from its mempool. Note that a transaction advertised in an `AppGossip` message may no longer be in the mempool, because they may have been included into a block, rejected, or dropped. If the transaction is retrieved, it is encoded into an `AppResponse` message. The `AppResponse` message will carry the same `requestID` of the originating `AppRequest` message and it will be sent back to `node B`.
- If `node B` receives an `AppResponse` message, it will decode the transaction and verifies that the ID matches the expected content from the original `AppRequest` message. If the content matches, the transaction is validated and issued into the mempool.
- If `nodeB`'s engine decides it isn't likely to receive an `AppResponse` message, the engine will issue an `AppRequestFailure` message. In such a case `node B` will mark the `requestID` as failed and the request for the unknown transaction is aborted.
