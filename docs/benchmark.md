# Benchmark: Vindice vs Loki

Vindice and Grafana Loki 3.6.18 receive the same 2 million log records, then answer the same four searches.

**Short version:** Vindice finds things 7 to 79 times faster and draws level charts more than 1,000 times faster, because it indexes log text and keeps pre-counted rollups. Loki makes new logs searchable about 5 times sooner. Under load the two now use about the same memory: the first run found a memory bug in Vindice, which is fixed and re-measured here (see [What the first run found](#what-the-first-run-found)).

Measured on 18 October 2026, on one machine. Treat the numbers as a comparison, not as capacity planning.

## Results

Both systems were measured in the same session, one after the other. Vindice is the build after 0.1.0 with the memory fixes, at the default limits from `deploy/docker-compose.yml`.

### Getting data in

| | Vindice | Loki |
|---|---|---|
| All 2,000 requests accepted | 4.8 s (418k records/s) | 4.0 s (504k records/s) |
| All 2,000,000 records searchable | **37.2 s** (54k records/s) | **7.6 s** (263k records/s) |
| Failed requests | 0 | 0 |

Both accept data at a similar rate. Loki's ingester serves new lines from memory straight away. Vindice passes them through Kafka, and the writer then inserts them into ClickHouse in batches of 50,000 rows, so they take longer to show up. The memory fixes did not change this number; where the time goes is still to be profiled.

### Searching

Median of 5 runs after one warm-up, over the whole 52-minute window. Both systems returned the same rows. The last column covers this run and the first run, because Loki's timings moved by up to 1.8× between runs on the same machine.

| Search | Vindice | Loki | Vindice faster by |
|---|---|---|---|
| A rare word anywhere in any log (50 matches) | **28 ms** | 1,111 ms | 19–40× |
| One user id (13 matches) | **14 ms** | 1,108 ms | 47–79× |
| One service, errors only, containing "timeout" (first 1,000) | **13 ms** | 91 ms | 3–7× |
| Count by level in 1-minute buckets | **1.3 ms** | 2,670 ms | 590–2,000× |

Why: Vindice keeps a token index on the log body, so a search for a word skips the blocks that cannot contain it. Loki indexes labels only and reads every chunk in the time range for a text filter. The level chart in Vindice reads a 1-minute rollup that is counted on write; Loki counts the raw lines at query time. When a query is narrowed by labels first, as in the third row, the gap is smallest.

### Memory

| | Vindice (vindice + Kafka + ClickHouse) | Loki |
|---|---|---|
| Idle, before data | 550 MiB | 47 MiB |
| Peak while ingesting | **1,163 MiB** (vindice 367, ClickHouse 408, Kafka 416) | **1,029 MiB** |
| 90 s after ingest | 848 MiB (vindice 105) | 889 MiB |
| 7 minutes after ingest | vindice back to 65 MiB | not measured |

Vindice's idle cost is Kafka and ClickHouse. Under load both stacks peak at about 1 GiB.

### Disk

| | Vindice | Loki |
|---|---|---|
| Stored log data | 50 MB (ClickHouse `logs` table) | 63 MB (chunks) |
| Write buffer | 47 MB (Kafka topic, removed by Kafka retention) | 341 MB (write-ahead log) |
| Whole data volume | 113 MB | 403 MB |

The raw log bodies alone were 132 MB. Measured 90 s after ingest, the ClickHouse volume was 141 MB because parts that had already been merged were still waiting for deletion; the 113 MB above is after that cleanup.

## What the first run found

The first run used the released 0.1.0 image. Its raw results are in `bench/results/0.1.0/`.

1. **The default setup ran out of memory.** With `mem_limit: 512m`, the vindice container was OOM-killed during ingest. With 1.5 GiB it finished, but vindice used all of it and still held about 1 GiB minutes later.
2. **Cause one: the batch limit counted the wrong thing.** The Kafka consumer passed `VINDICE_BATCH_ROWS` (50,000) to `PollRecords`, which counts Kafka records. One record holds a whole OTLP request, here 1,000 log rows, so one poll could hand over 50 million rows. The 2-million-row backlog was 47 MB compressed, small enough for a single 64 MB fetch, so all of it was decoded before the first flush. In isolation that was 1,160 MB of heap in the writer.
3. **Cause two: written rows were never freed.** After each flush the buffer was emptied with `rows[:0]`, which keeps the old array and every string and map it points to. The 1,160 MB stayed allocated after the flush.

This mattered beyond benchmarks: any backlog in Kafka, for example after ClickHouse has been down, would trigger it on recovery.

**The fix:**
- The consumer now sizes each poll from a running estimate of rows per record, so it takes about one batch of rows at a time, and the Kafka client buffers at most 16 MB per fetch.
- After a flush the writer zeroes the written rows, and drops buffers larger than 131,072 rows.
- `deploy/docker-compose.yml` sets `GOMEMLIMIT=400MiB` under the 512 MB container limit.
- Tests cover both causes: `TestBacklogIsFlushedInRowSizedBatches` fills an in-process Kafka with a backlog and checks every flush, and `TestFlushReleasesWrittenRows` checks that flushed rows are released.

| vindice process, same load | 0.1.0 | After the fix |
|---|---|---|
| With `mem_limit: 512m` | OOM-killed | Finished |
| Peak | 1,536 MiB (its 1.5 GiB limit) | 367 MiB |
| After the load | about 1 GiB, minutes later | 105 MiB after 90 s, 65 MiB after 7 minutes |

## Setup

| | |
|---|---|
| Machine | Apple M4 MacBook Pro, Docker Desktop VM with 10 CPUs and 7.7 GiB of RAM |
| Vindice | Built from this repository after 0.1.0, Kafka 4.3.1, ClickHouse 26.3, limits and ClickHouse config from `deploy/` |
| Loki | `grafana/loki:3.6.18`, single binary, TSDB index and chunks on the local filesystem, schema v13 |
| Data | 2,000,000 records, 8 services, levels 40% debug, 45% info, 10% warn, 5% error, spread over 50 minutes, attributes `user_id` (200,000 values) and `order_id` (unique), average body 66 bytes |
| Sending | OTLP over HTTP with protobuf, 1,000 records per request, 8 requests at a time, same seed for both systems |

To keep the comparison fair:
- Each system ran alone. The other was removed, data included, before the next one started. Other unrelated containers kept running on the machine for both systems.
- Loki's per-tenant ingestion limits were raised so the test measures throughput, not the default rate limit.
- Loki's query result cache was turned off, and ClickHouse's query cache is off by default, so repeated searches did real work.
- The searches ran right after ingest. Recent data in Loki was still in the ingester's memory, which is the usual case for "the last hour" searches.
- Not tested: Loki scaled out with object storage, which is how it is meant to grow, and either system under mixed read and write load.

## Run it yourself

You need Docker with Compose, Go and Python 3.

```bash
cd bench
./run.sh                                      # about 10 minutes; removes its containers and volumes at the end
VINDICE_IMAGE=my-build:latest ./run.sh        # test a local build instead of the released image
```

`bench/loadgen` generates and sends the records, `bench/bench.py` measures one system, and the raw results are in `bench/results/`.
