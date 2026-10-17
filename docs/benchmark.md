# Benchmark: Vindice vs Loki

Vindice 0.1.0 and Grafana Loki 3.6.18 receive the same 2 million log records, then answer the same four searches.

**Short version:** Vindice finds things 3 to 45 times faster and builds level charts about 600 times faster, because it indexes log text and keeps pre-counted rollups. Loki makes new logs searchable 4 times sooner and used much less memory under load. The run also exposed a memory problem in Vindice that needs fixing: see [What this found in Vindice](#what-this-found-in-vindice).

Measured on 18 October 2026. One run on one machine, so treat the numbers as a comparison, not as capacity planning.

## Results

### Getting data in

| | Vindice | Loki |
|---|---|---|
| All 2,000 requests accepted | 5.4 s (368k records/s) | 5.1 s (389k records/s) |
| All 2,000,000 records searchable | **37.2 s** (54k records/s) | **8.9 s** (225k records/s) |
| Failed requests | 0 | 0 |

Both accept data at about the same rate. Loki's ingester serves new lines from memory straight away. Vindice passes them through Kafka, then the writer batches them into ClickHouse every 3 seconds or 50,000 rows, so they take longer to show up.

### Searching

Median of 5 runs after one warm-up, over the whole 52-minute window. Both systems returned the same rows.

| Search | Vindice | Loki | Faster |
|---|---|---|---|
| A rare word anywhere in any log (50 matches) | **34 ms** | 633 ms | Vindice, 19× |
| One user id (13 matches) | **15 ms** | 691 ms | Vindice, 47× |
| One service, errors only, containing "timeout" (first 1,000) | **22 ms** | 69 ms | Vindice, 3× |
| Count by level in 1-minute buckets | **2.5 ms** | 1,481 ms | Vindice, 590× |

Why: Vindice keeps a token index on the log body, so a search for a word skips the blocks that cannot contain it. Loki indexes labels only and reads every chunk in the time range for a text filter. The level chart in Vindice reads a 1-minute rollup that is counted on write; Loki counts the raw lines at query time. When a query is narrowed by labels first, as in the third row, the gap shrinks.

### Memory

| | Vindice (vindice + Kafka + ClickHouse) | Loki |
|---|---|---|
| Idle, before data | 587 MiB | 55 MiB |
| Peak while ingesting | **2,772 MiB** (vindice 1,536, ClickHouse 810, Kafka 521) | **959 MiB** |
| 90 s after ingest | 2,274 MiB | 1,022 MiB |

The vindice process reached its 1.5 GiB limit and still held about 1 GiB several minutes later.

### Disk

| | Vindice | Loki |
|---|---|---|
| Stored log data | 50 MB (ClickHouse `logs` table: 280 MB of rows compressed to 42 MB) | 61 MB (chunks) |
| Write buffer | 47 MB (Kafka topic, removed by Kafka retention) | 332 MB (write-ahead log) |
| Whole data volume | 105 MB | 403 MB |

The raw log bodies alone were 132 MB.

## What this found in Vindice

1. **The default memory limit is too small for a burst.** With the repository's `docker-compose.yml`, the vindice container (`mem_limit: 512m`) was OOM-killed partway through ingest. The numbers above use 1.5 GiB for that container; everything else is unchanged. The raw outcome is in `bench/results/vindice-default-512m.json`.
2. **Memory is not given back.** After the load ended, vindice stayed at about 1 GiB with no CPU use.
3. **New data is searchable later than in Loki.** 37 s versus 9 s for 2 million records.

Likely causes, not yet profiled: the writer and the ingest path run in one process with no shared memory budget, the Kafka consumer may fetch up to 64 MB at a time, and the Go runtime has no `GOMEMLIMIT`. Likely fixes are setting `GOMEMLIMIT` from the container limit, capping consumer fetches, and pushing back on ingest when the writer falls behind. Splitting `-mode=ingest` and `-mode=writer` into separate containers is also worth measuring.

## Setup

| | |
|---|---|
| Machine | Apple M4 MacBook Pro, Docker Desktop VM with 10 CPUs and 7.7 GiB of RAM |
| Vindice | `ghcr.io/b1ll1eee/vindice-observability:0.1.0`, Kafka 4.3.1, ClickHouse 26.3, the limits and ClickHouse config from `deploy/` |
| Loki | `grafana/loki:3.6.18`, single binary, TSDB index and chunks on the local filesystem, schema v13 |
| Data | 2,000,000 records, 8 services, levels 40% debug, 45% info, 10% warn, 5% error, spread over 50 minutes, attributes `user_id` (200,000 values) and `order_id` (unique), average body 66 bytes |
| Sending | OTLP over HTTP with protobuf, 1,000 records per request, 8 requests at a time, same seed for both systems |

To keep the comparison fair:
- Each system ran alone. The other was removed, data included, before the next one started.
- Loki's per-tenant ingestion limits were raised so the test measures throughput, not the default rate limit.
- Loki's query result cache was turned off, and ClickHouse's query cache is off by default, so repeated searches did real work.
- The searches ran right after ingest. Recent data in Loki was still in the ingester's memory, which is the usual case for "the last hour" searches.
- Not tested: Loki scaled out with object storage, which is how it is meant to grow, and either system under mixed read and write load.

## Run it yourself

You need Docker with Compose, Go and Python 3.

```bash
cd bench
./run.sh          # about 10 minutes; removes its containers and volumes at the end
```

`bench/loadgen` generates and sends the records, `bench/bench.py` measures one system, and the raw results are in `bench/results/`.
