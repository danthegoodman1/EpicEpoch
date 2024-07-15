# EpicEpoch

A highly concurrent, low latency, highly available monotonic hybrid timestamp service.

Used for distributed systems and clients, like distributed transactions. Self-sufficient via Raft.

<!-- TOC -->
* [EpicEpoch](#epicepoch)
  * [Getting started](#getting-started)
  * [Configuration (env vars)](#configuration-env-vars)
  * [Motivation (Why make this?)](#motivation-why-make-this)
  * [Reading the timestamp value](#reading-the-timestamp-value)
  * [HTTP endpoints (HTTP/1.1, H2C, HTTP/3 self-signed)](#http-endpoints-http11-h2c-http3-self-signed)
  * [Client design](#client-design)
  * [Latency and concurrency](#latency-and-concurrency)
    * [Latency optimizations](#latency-optimizations)
    * [Concurrency optimizations](#concurrency-optimizations)
  * [Performance testing (HTTP/1.1)](#performance-testing-http11)
    * [Simple test (buffer 10k)](#simple-test-buffer-10k)
    * [Performance test (buffer 10k)](#performance-test-buffer-10k)
<!-- TOC -->

## Getting started

WIP

- cmd to run
- data directory `_raft` folder, snapshots at `epoch-{nodeID}.json`, never delete these unless you know what you're doing.
- some info about disk usage (it's quite small)

Note that the HTTP/3 server will write a `cert.pem` and `key.pem` in the same directory as the binary if they do not already exist.

Note that currently this is hard-coded to run 3 nodes locally (see https://github.com/danthegoodman1/EpicEpoch/issues/9):

```
1: "localhost:60001"
2: "localhost:60002"
3: "localhost:60003"
```

## Configuration (env vars)

Configuration is done through environment variables

| **ENV VAR**              | **Required** | **Default**         | **Description**                                                                                                                                                                   |
|--------------------------|--------------|---------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| DEBUG                    | no           |                     | Enables debug logging if set to `1`                                                                                                                                               |
| PRETTY                   | no           |                     | Enabled pretty print logging if set to `1`                                                                                                                                        |
| LOG_TIME_MS              | no           |                     | Formats the time as unix milliseconds in logs                                                                                                                                     |
| NODE_ID                  | yes          | `0` (invalid value) | Sets the Raft node ID, must be unique                                                                                                                                             |
| TIMESTAMP_REQUEST_BUFFER | yes          | 10000               | Sets the channel buffer length for pending requests. Requests that are blocked when this buffer is full are responded to in random order, unlike requests that are in the buffer. |
| EPOCH_INTERVAL_MS        | yes          | 100                 | The interval at which the Raft leader will increment the epoch (and reset the epoch index).                                                                                       |
| EPOCH_DEADLINE_LIMIT     | yes          | 100                 | How many deadline exceeded errors incrementing the epoch can be tolerated before the system crashes                                                                               |
| RAFT_ADDR                | yes          |                     | The address which raft is exposed                                                                                                                                                 |
| HTTP_PORT                | yes          | 8080                | The address which the HTTP port is exposed (for interfacing with clients)                                                                                                         |


## Motivation (Why make this?)

Time is perhaps the most important thing to a distributed system.

Without reliable time handling distributed isolation become nearly impossible. You can't guarantee commits happen, or that they happen with the expected isolation. You can't guarantee that your data was replicated. It's a big deal.

I won't dive in to the details as to why time is so important, or so hard, for distributed systems, but just know that it's the bane of dist sys eng's existence. If we had perfect clocks, then dist systems would be orders of magnitude faster, have far fewer bugs, and be far easier to develop.

But we don't have perfect clocks, so we have to play some tricks instead like using hybrid timestamps: Combining real time and "fake time" in the form of an incrementing counter that's reset when the real time changes.

In building my own [Percolator clients](https://github.com/danthegoodman1/Percolators), the last remaining task was to have a global timestamp oracle that could be used to guarantee monotonic hybrid timestamps for transactions. This, as a standalone service, didn't really exist. Plus I thought it would be super fun to make (it was) as I'm arguably obsessed with distributed systems.

So EpicEpoch (name change possibly pending) was born for that sole purpose: Serve monotonic hybrid timestamps as fast as possible, to as many clients as possible.

Building a bespoke timestamp oracle service has massive benefits over using something like etcd: performance and latency.

The system is designed with the sole purpose of serving unique timestamps as fast as possible. It only needs to replicate a single value over Raft, and store 46 bytes to disk as a raft snapshot. It leverages linearizable reads, request collapsing, and an efficient actor model to ensure that it can maximize the available compute to serve timestamps at the highest possible concurrency. It supports HTTP/1.1, HTTP/2 (cleartext), and HTTP/3 to give clients the most performant option they can support.

Generalized solutions couldn't meet 1% of the performance EpicEpoch can for monotonic hybrid timestamps while also maintain guarantees during failure scenarios (I'm looking at you in particular, Redis).

This is not considered production ready (at least for anyone but me, or who ever is ready to write code about it). There is still lots of testing, instrumenting, documentation, and more to be done before I'd dare to suggest someone else try it. At a minimum, it can serve as an implementation reference.

## Reading the timestamp value

The timestamp value is 16 bytes, constructed of an 8 byte timestamp (unix nanoseconds) + an 8 byte epoch index.

This ensures that transactions are always unique and in order, latency and concurrency for serving timestamp requests is maximized, and timestamps can be reversed for time-range queries against your data.

This also ensures that the request and response are each a single TCP frame.

## HTTP endpoints (HTTP/1.1, H2C, HTTP/3 self-signed)

`/up` exists to check if the HTTP server is running
`/ready` checks to see if the node has joined the cluster and is ready to serve requests


`/timestamp` can be used to fetch a unique monotonic 16 byte hybrid timestamp value (this is the one you want to use).

This will return a 409 if the node is not the leader. Clients should use client-aware routing to update their local address cache if they encounter this (see [CLIENT_DESIGN.md](CLIENT_DESIGN.md)).

Can use the query param `n` to specify a number >= 1, which will return multiple timestamps that are guaranteed to share the same epoch and have a sequential epoch index. These timestamps are appended to each other, so `n=2` will return a 32 byte body.


`/members` returns a JSON in the shape of:

```json
{
  "leader": {
    "nodeID": 1,
    "addr": "addr1"
  },
  "members": [
    {
      "nodeID": 1,
      "addr": "addr1"
    }
  ]
}
```

This is used for client-aware routing.

## Client design

See [CLIENT_DESIGN.md](CLIENT_DESIGN.md)

## Latency and concurrency

Optimizations have been made to reduce the latency and increase concurrency as much as possible, trading concurrency for latency where needed.

### Latency optimizations

Instead of using channels, ring buffers are used where ever practical (with some exceptions due to convenience). [Ring buffers are multiple times faster under high concurrency situations](https://bravenewgeek.com/so-you-wanna-go-fast/).

For example, all requests queue to have a timestamp generated by putting a request into a ring buffer. The reader agent is poked with a channel (for select convenience with shutdown), fetches the current epoch, reads from the ring buffer to generate timestamps, and responds to the request with a ring buffer provided in the request.

### Concurrency optimizations

The nature of the hybrid timestamp allows concurrency limited only by the epoch interval and a uint64. Within an epoch interval, a monotonic counter is incremented for every request, meaning that we are not bound to the write of raft to serve a request, and we can serve up to the max uint64 requests for a single epoch interval (which should be far faster than any server could serve pending requests).

As a result the latency ceiling of request time is roughly the latency of a linearizable read through raft + the time to respond to all pending requests. The raft read is amortized across all pending requests.

To ensure that this node is the leader, a linearizable read across the cluster must take place, but many requests can share this via request collapsing.

## Performance testing (HTTP/1.1)

Using k6 on a 200 core C3D from GCP, running all 3 instances and the test, the following was observed.

TLDR: Lack of h2c or h3 support from k6, and not tuning the tcp stack or ulimit on the machine greatly reduced potential results. This could easily do 1M+ on the same hardware with more capable clients.

### Simple test (buffer 10k)

5-7 cores used at peak

followers using 50-80% cpu

```
     scenarios: (100.00%) 1 scenario, 100 max VUs, 2m30s max duration (incl. graceful stop):
              * default: Up to 100 looping VUs for 2m0s over 3 stages (gracefulRampDown: 30s, gracefulStop: 30s)


running (0m22.0s), 073/100 VUs, 1385955 complete and 0 interrupted iteration

     ✗ is status 200
      ↳  99% — ✓ 10270369 / ✗ 443

     checks.........................: 99.99%   ✓ 10270369     ✗ 443
     data_received..................: 1.5 GB   13 MB/s
     data_sent......................: 914 MB   7.6 MB/s
     http_req_blocked...............: avg=1.42µs   min=320ns    med=1.1µs    max=51.93ms p(90)=1.88µs  p(95)=2.23µs
     http_req_connecting............: avg=1ns      min=0s       med=0s       max=1.07ms  p(90)=0s      p(95)=0s
   ✓ http_req_duration..............: avg=822.67µs min=132.05µs med=576.33µs max=1s      p(90)=1.21ms  p(95)=1.78ms
       { expected_response:true }...: avg=779.52µs min=132.05µs med=576.33µs max=1s      p(90)=1.21ms  p(95)=1.78ms
     ✓ { staticAsset:yes }..........: avg=0s       min=0s       med=0s       max=0s      p(90)=0s      p(95)=0s
   ✓ http_req_failed................: 0.00%    ✓ 443          ✗ 10270369
     http_req_receiving.............: avg=21.84µs  min=5.02µs   med=18.35µs  max=51.44ms p(90)=25.42µs p(95)=30.34µs
     http_req_sending...............: avg=7.32µs   min=1.71µs   med=5.85µs   max=31.67ms p(90)=7.98µs  p(95)=9.27µs
     http_req_tls_handshaking.......: avg=0s       min=0s       med=0s       max=0s      p(90)=0s      p(95)=0s
     http_req_waiting...............: avg=793.5µs  min=122.14µs med=548.77µs max=1s      p(90)=1.17ms  p(95)=1.73ms
     http_reqs......................: 10270812 85589.648371/s
     iteration_duration.............: avg=871.13µs min=151.51µs med=621.92µs max=1s      p(90)=1.26ms  p(95)=1.86ms
     iterations.....................: 10270812 85589.648371/s
     vus............................: 1        min=1          max=100
     vus_max........................: 100      min=100        max=100


running (2m00.0s), 000/100 VUs, 10270812 complete and 0 interrupted iterations
```

### Performance test (buffer 10k)

_Also tested with 1M buffer, didn't make a difference in this test._

63-70 cores used at peak during the test
55-65 cores used by the test runner
<7GB of ram used

followers using 50-80% cpu

```
     scenarios: (100.00%) 1 scenario, 10000 max VUs, 4m30s max duration (incl. graceful stop):
              * default: Up to 10000 looping VUs for 4m0s over 7 stages (gracefulRampDown: 30s, gracefulStop: 30s)


     ✗ is status 200
      ↳  99% — ✓ 34288973 / ✗ 61

     checks.........................: 99.99%   ✓ 34288973      ✗ 61
     data_received..................: 5.0 GB   21 MB/s
     data_sent......................: 3.1 GB   13 MB/s
     http_req_blocked...............: avg=7.89µs   min=320ns    med=1.9µs   max=221.57ms p(90)=2.71µs  p(95)=3.24µs
     http_req_connecting............: avg=294ns    min=0s       med=0s      max=123.89ms p(90)=0s      p(95)=0s
   ✓ http_req_duration..............: avg=16.99ms  min=172.36µs med=6.31ms  max=1s       p(90)=67.13ms p(95)=83.12ms
       { expected_response:true }...: avg=16.99ms  min=172.36µs med=6.31ms  max=1s       p(90)=67.13ms p(95)=83.12ms
     ✓ { staticAsset:yes }..........: avg=0s       min=0s       med=0s      max=0s       p(90)=0s      p(95)=0s
   ✓ http_req_failed................: 0.00%    ✓ 61            ✗ 34288973
     http_req_receiving.............: avg=147.78µs min=5.3µs    med=16.97µs max=215.76ms p(90)=27.1µs  p(95)=41.97µs
     http_req_sending...............: avg=92.2µs   min=1.76µs   med=6.3µs   max=222.21ms p(90)=9.36µs  p(95)=20.79µs
     http_req_tls_handshaking.......: avg=0s       min=0s       med=0s      max=0s       p(90)=0s      p(95)=0s
     http_req_waiting...............: avg=16.75ms  min=149.66µs med=6.22ms  max=1s       p(90)=66.73ms p(95)=82.74ms
     http_reqs......................: 34289034 142866.904307/s
     iteration_duration.............: avg=30.51ms  min=205.04µs med=19.54ms max=1s       p(90)=87.72ms p(95)=108.99ms
     iterations.....................: 34289034 142866.904307/s
     vus............................: 6        min=6           max=10000
     vus_max........................: 10000    min=10000       max=10000


running (4m00.0s), 00000/10000 VUs, 34289034 complete and 0 interrupted iterations
```

It seems like the request completion rate did not grow much past 300 vus, so considering that we ran up to 10k vus this is HTTP/1.1 rearing it's less performant head.

Some log output of the duration between reading from raft and writing to the pending request channels with incremented hybrid timestamps:
```
raft.(*EpochHost).generateTimestamps() > Served 27 requests in 14.72µs
raft.(*EpochHost).generateTimestamps() > Served 1 requests in 5.75µs
raft.(*EpochHost).generateTimestamps() > Served 6 requests in 5.9µs
raft.(*EpochHost).generateTimestamps() > Served 2 requests in 6.52µs
raft.(*EpochHost).generateTimestamps() > Served 1 requests in 1.78µs
raft.(*EpochHost).generateTimestamps() > Served 67 requests in 63.51µs
raft.(*EpochHost).generateTimestamps() > Served 87 requests in 83.8µs
raft.(*EpochHost).generateTimestamps() > Served 212 requests in 2.81964ms
raft.(*EpochHost).generateTimestamps() > Served 7139 requests in 6.427489ms
raft.(*EpochHost).generateTimestamps() > Served 26 requests in 497.96µs
raft.(*EpochHost).generateTimestamps() > Served 2034 requests in 1.50941ms
raft.(*EpochHost).generateTimestamps() > Served 1 requests in 4.66µs
raft.(*EpochHost).generateTimestamps() > Served 1 requests in 3.84µs
raft.(*EpochHost).generateTimestamps() > Served 1 requests in 1.55µs
raft.(*EpochHost).generateTimestamps() > Served 1 requests in 1.35µs
```

Combined with:
```
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 152.25µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 274.583µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 234.166µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 191.709µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 188.583µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 196.792µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 203.333µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 145.833µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 169.792µs
http_server.(*HTTPServer).GetTimestamp() > handled request (HTTP/1.1) in 226.542µs
```

It's quite clear that either k6 (the load tester), or it's lack of h2c/h3 support is to blame, considering the known performance of the echo framework, the fact that it used ~70% of available CPU on the machine, and the logs above.

There would also likely be massive performance gains by increaseing the ulimit and tuning the tcp stack.
