# Peer Package

The peer package handles networking for the VM.

## Network

The `Network` interface implements the networking portion of the required VM interface. The VM utilizes the `Network` interface to:

- Set an App Gossip handler for incoming VM gossip messages
- Set an App Request handler for incoming VM request messages
- Send App Requests to peers in the network and specify a response handler to be called upon receiving a response or failure notification
- Send App Gossip messages to the network

## Client

The client utilizes the `Network` interface to send requests to peers on the network and utilizes the `waitingHandler` to wait until a response or failure is received from the AvalancheGo networking layer.

This allows the user of `Client` to treat it as if it were returning results from the network synchronously.

```go
result, err := client.Request(nodeID, request) // Blocks until receiving a response from the network
if err != nil {
    return err
}

foo(result) // do something with the result
```
