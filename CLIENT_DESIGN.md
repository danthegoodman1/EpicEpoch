# Client Design (WIP)

<!-- TOC -->
* [Client Design (WIP)](#client-design-wip)
  * [Client-aware routing](#client-aware-routing)
  * [Choosing a protocol](#choosing-a-protocol)
  * [Notes on Raft](#notes-on-raft)
<!-- TOC -->

## Client-aware routing

To reduce latency, clients should route directly to the leader.

They may learn about this via the HTTP `/members` route. A client can read this from any node that is part of the cluster.

(NOT IMPLEMENTED YET) The HTTP interface will respond with a 301 to the correct host. The client should refresh their leader membership and follow the redirect.

Other interfaces such as gRPC will reject the get with a `ErrNotLeader` error, indicating that the client should refresh membership info from any node and retry. 

## Choosing a protocol

While HTTP/3 should be the no-brainer, you will want to test the performance of h2c vs h3 for your client, as h3 is not super widely supported so some community implementations could end up being slower than a good h2c implementation.

Avoid HTTP/1.1 when ever you can, it will have a monstrous negative performance impact.

## Notes on Raft

This is expected to run with very low latency, and by default the raft RTT is set very low.

Dragonboat (the underlying package for Raft) still has some decently high election times, so consider that it can take upwards of 100 RTTs to elect a new leader. In this time requests could time out, so clients must retry.