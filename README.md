# relayer 📡

Relayer is a command-line tool that facilitates encrypted relay services between machines. It enables secure communication without the need for complex certificate infrastructure by leveraging the libp2p framework. This makes it an ideal solution for creating secure, tunneled connections between machines in a decentralized manner.

## Key Features 🔑

1. Secure Tunnels: Relayer creates encrypted communication tunnels between machines, ensuring data privacy and integrity.
2. Decentralized: Built on libp2p, it uses a Distributed Hash Table (DHT) for peer discovery, eliminating the need for acentral authority.
3. Simplified Configuration: It simplifies secure networking by handling encryption automatically, so you don't have to managea complex Public Key Infrastructure (PKI) or certificate renewal process.
4. Flexible Service Relaying: It can relay various services (like HTTP) from a remote machine to a local machine, making them accessible as if they were running locally.

## How It Works 💡

Relayer operates on three main components: a DHT Server, a Servicer, and a Binder.

1. DHT Server: This is the discovery node. It's a central point (for now) that other nodes use to find each other. All Servicer and Binder nodes must be configured to bootstrap to this server.
2. Servicer: This is the machine that hosts the service you want to expose. It advertises the service's availability to the DHT Server.
3. Binder: This is the machine that wants to access the remote service. It queries the DHT Server to find the Servicer and then "binds" the remote service to a local port, making it accessible as a local service.

## Installation 💻

You can easily install the relayer tool directly from the source repository using the go install command. This will download the source code, compile it, and place the executable in your $GOPATH/bin directory.
Bash

```bash
go install github.com/RogueTeam/relayer
```

After running this command, you can use the relayer command directly from your terminal. Ensure that your $GOPATH/bin directory is included in your system's PATH environment variable.

## Getting Started 🚀

1. Run the DHT Server

First, start the DHT Server on a machine. This will be the discovery node for your network.
Bash

```bash
go run ./cmd/relayer/ dht --config ./examples/dht.yaml
```

`examples/dht.yaml`

```yaml
listen:
  - /ip4/0.0.0.0/udp/9999/quic-v1
identity-file: dht.id
```

This command starts the DHT server, listening for new connections on UDP port 9999. It will generate a unique peer identity in dht.id. Make a note of the p2p address of this server, as other nodes will need it to bootstrap.

2. Run the Servicer

On the machine hosting the service you want to expose (e.g., an HTTP server on port 8080), run the Servicer.
Bash

```bash
go run ./cmd/relayer/ run --config examples/runner-servicer.yaml
```

`examples/runner-servicer.yaml`

```yaml
listen:
  - /ip4/0.0.0.0/udp/8888/quic-v1
identity-file: servicer.id
dht:
  enabled: true
  bootstrap:
    - /ip4/127.0.0.1/udp/9999/quic-v1/p2p/12D3KooWBhshrWcjxXRULopbBvbfDFvWa7kEVsKXEpPATPDg7WbM # REPLACE WITH YOUR DHT SERVER'S ADDRESS
services:
  - name: HTTP
    addrs:
      - /ip4/127.0.0.1/tcp/8080
    advertise: true
```

The bootstrap address must point to your running DHT Server. The addrs field specifies the local address and port of the service to be relayed. Setting advertise to true makes the service discoverable by other nodes.

3. Run the Binder

On the machine that needs to access the remote service, run the Binder.
Bash

```bash
go run ./cmd/relayer/ run --config examples/runner-binder.yaml
```

`examples/runner-binder.yaml`

```yaml
listen:
  - /ip4/0.0.0.0/udp/9898/quic-v1
identity-file: binder.id
dht:
  enabled: true
  bootstrap:
    - /ip4/127.0.0.1/udp/9999/quic-v1/p2p/12D3KooWBhshrWcjxXRULopbBvbfDFvWa7kEVsKXEpPATPDg7WbM # REPLACE WITH YOUR DHT SERVER'S ADDRESS
remotes:
  - name: HTTP
    listen: /ip4/0.0.0.0/tcp/10000
```

Again, the bootstrap address must be the DHT Server's address. The remotes section specifies the service name to look for (HTTP) and the local address and port (10000) on which the remote service will be made available. After running this, you can access the remote HTTP service by connecting to localhost:10000.

```
                 +-------------------+
                 |    DHT Server     |
                 |   (Discovery)     |
                 |  `dht.yaml`      |
                 |      9999/udp     |
                 +-------------------+
                            |
        .-------------------|-------------------.
        |                   |                   |
        |                   |                   |
+--------------+    +--------------+    +--------------+
|   Servicer   |    |    Servicer  |    |     Binder   |
| (Advertiser) |    |  (Advertiser)|    |  (Consumer)  |
| servicer.yaml|    |servicer.yaml |    | binder.yaml  |
|  8888/udp    |    |   XYZW/udp   |    |    9898/udp  |
+--------------+    +--------------+    +--------------+
        |                   |                   |
        |  +-----------+    |                   |
        `->|  Service  |<---'                   |
           |  (e.g.,   |                        |
           |  HTTP)    |                        |
           +-----------+                        |
           |                                    |
           '------------------------------------'
           (Encrypted Peer-to-Peer Tunnel)
```
