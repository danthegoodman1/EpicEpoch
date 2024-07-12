# Client Design (WIP)

<!-- TOC -->
* [Client Design (WIP)](#client-design-wip)
  * [Client-aware routing](#client-aware-routing)
<!-- TOC -->

## Client-aware routing

To reduce latency, clients should route directly to the leader.

They may learn about this via the HTTP `/members` route. A client can read this from any node that is part of the cluster.

(NOT IMPLEMENTED YET) The HTTP interface will respond with a 301 to the correct host. The client should refresh their leader membership and follow the redirect.

Other interfaces such as gRPC will reject the get with a `ErrNotLeader` error, indicating that the client should refresh membership info from any node and retry. 