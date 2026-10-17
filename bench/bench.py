"""Run one benchmark pass against Vindice or Loki and write results/<system>.json.

usage: python3 -I bench.py vindice|loki
Same dataset (loadgen, seed 42), same batch size and concurrency for both systems.
"""
import json
import os
import re
import statistics
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
SYSTEM = sys.argv[1]
N = int(os.environ.get("BENCH_N", "2000000"))
RUNS = 5

if SYSTEM == "vindice":
    BASE = "http://localhost:18080"
    OTLP = "http://localhost:14318/v1/logs"
    AUTH = "Bearer " + open(os.path.join(HERE, ".vkey")).read().strip()
    CONTAINERS = ["vbench-vindice-1", "vbench-kafka-1", "vbench-clickhouse-1"]
    VOLUMES = ["vbench_clickhouse-data", "vbench_kafka-data"]
else:
    BASE = "http://localhost:13100"
    OTLP = "http://localhost:13100/otlp/v1/logs"
    AUTH = ""
    CONTAINERS = ["lbench-loki-1"]
    VOLUMES = ["lbench_loki-data"]

COOKIE = None


def http(path, params=None, method="GET", body=None):
    url = BASE + path + ("?" + urllib.parse.urlencode(params) if params else "")
    req = urllib.request.Request(url, method=method, data=body)
    if COOKIE:
        req.add_header("Cookie", COOKIE)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=300) as r:
        data = r.read()
        return r.headers, (json.loads(data) if data and data[:1] in b"{[" else data)


def login():
    global COOKIE
    env = dict(l.strip().split("=", 1) for l in open(os.path.join(HERE, "bench.env")) if "=" in l)
    hdrs, _ = http("/api/v1/auth/login", method="POST",
                   body=json.dumps({"username": "admin", "password": env["VINDICE_ADMIN_PASSWORD"]}).encode())
    COOKIE = hdrs.get("Set-Cookie").split(";", 1)[0]


# ------------------------------------------------------------------ resources
def mem_mib():
    out = subprocess.run(["docker", "stats", "--no-stream", "--format", "{{.Name}} {{.MemUsage}}", *CONTAINERS],
                         capture_output=True, text=True).stdout
    total = 0.0
    per = {}
    for line in out.strip().splitlines():
        name, usage = line.split(" ", 1)
        m = re.match(r"([\d.]+)\s*([KMG]?i?B)", usage)
        if not m:
            continue
        v = float(m.group(1)) * {"B": 1 / 1048576, "KiB": 1 / 1024, "MiB": 1, "GiB": 1024,
                                 "kB": 1 / 1024, "MB": 1, "GB": 1024}[m.group(2)]
        per[name] = round(v)
        total += v
    return round(total), per


class Sampler(threading.Thread):
    def __init__(self):
        super().__init__(daemon=True)
        self.peak, self.per, self.stop = 0, {}, False

    def run(self):
        while not self.stop:
            total, per = mem_mib()
            self.peak = max(self.peak, total)
            for k, v in per.items():
                self.per[k] = max(self.per.get(k, 0), v)


def volume_bytes():
    out = {}
    for v in VOLUMES:
        r = subprocess.run(["docker", "run", "--rm", "-v", f"{v}:/d:ro", "alpine:3.22", "du", "-sk", "/d"],
                           capture_output=True, text=True).stdout
        out[v] = int(r.split()[0]) * 1024
    return out


# ------------------------------------------------------------------ counting
def count(t0, t1):
    if SYSTEM == "vindice":
        _, h = http("/api/v1/logs/histogram", {"from": t0, "to": t1})
        return sum(b["debug"] + b["info"] + b["warn"] + b["error"] for b in h["buckets"])
    rng = f"{max(1, (t1 - t0) // 1000)}s"
    _, r = http("/loki/api/v1/query", {"query": f'sum(count_over_time({{service_name=~".+"}}[{rng}]))',
                                       "time": str(t1 * 1_000_000)})
    res = r["data"]["result"]
    return int(float(res[0]["value"][1])) if res else 0


# ------------------------------------------------------------------ queries
def queries(t0, t1):
    if SYSTEM == "vindice":
        def logs(**p):
            def f():
                _, r = http("/api/v1/logs", {"from": t0, "to": t1, **p})
                return len(r["logs"])
            return f

        def hist():
            _, r = http("/api/v1/logs/histogram", {"from": t0, "to": t1})
            return len(r["buckets"])
        return {
            "needle": logs(q="needle7f3a9c", limit=1000),
            "user_id": logs(q="u-048213", limit=1000),
            "service_level_text": logs(service="checkout", level="error", q="timeout", limit=1000),
            "level_histogram": hist,
        }

    def rng(q, limit=1000, step=None):
        def f():
            p = {"query": q, "start": str(t0 * 1_000_000), "end": str(t1 * 1_000_000), "limit": limit,
                 "direction": "backward"}
            if step:
                p["step"] = step
            _, r = http("/loki/api/v1/query_range", p)
            res = r["data"]["result"]
            if r["data"]["resultType"] == "streams":
                return sum(len(s["values"]) for s in res)
            return len(res[0]["values"]) if res else 0
        return f
    return {
        "needle": rng('{service_name=~".+"} |= "needle7f3a9c"'),
        "user_id": rng('{service_name=~".+"} |= "u-048213"'),
        "service_level_text": rng('{service_name="checkout"} | severity_text="ERROR" |= "timeout"'),
        "level_histogram": rng('sum by (severity_text) (count_over_time({service_name=~".+"}[1m]))', step="60s"),
    }


def timed(fn):
    fn()  # warm-up
    times, rows = [], None
    for _ in range(RUNS):
        s = time.perf_counter()
        rows = fn()
        times.append((time.perf_counter() - s) * 1000)
    return {"median_ms": round(statistics.median(times), 1), "min_ms": round(min(times), 1),
            "max_ms": round(max(times), 1), "rows": rows}


def main():
    if SYSTEM == "vindice":
        login()
    res = {"system": SYSTEM, "records": N}
    res["idle_before_mib"] = mem_mib()

    s = Sampler()
    s.start()
    began = time.time()
    out = subprocess.run([os.path.join(HERE, "loadgen-bin"), "-url", OTLP, "-auth", AUTH, "-n", str(N)],
                         capture_output=True, text=True)
    if out.returncode:
        sys.exit(out.stderr)
    gen = json.loads(out.stdout.strip().splitlines()[-1])
    res["ingest"] = gen
    t0, t1 = gen["t0_ms"] - 60_000, gen["t1_ms"] + 60_000
    seen = 0
    while time.time() - began < 1800:
        seen = count(t0, t1)
        if seen >= N:
            break
        time.sleep(2)
    res["end_to_end_seconds"] = round(time.time() - began, 1)
    res["end_to_end_records_per_sec"] = round(N / res["end_to_end_seconds"])
    res["queryable_records"] = seen
    s.stop = True
    s.join()
    res["peak_during_ingest_mib"] = s.peak
    res["peak_per_container_mib"] = s.per

    res["queries"] = {name: timed(fn) for name, fn in queries(t0, t1).items()}

    if SYSTEM == "loki":
        http("/flush", method="POST", body=b"")
    time.sleep(90)  # let flushes, merges and GC settle
    res["idle_after_mib"] = mem_mib()
    res["disk_bytes"] = volume_bytes()
    if SYSTEM == "vindice":
        q = ("SELECT table, sum(rows), sum(data_compressed_bytes), sum(data_uncompressed_bytes), sum(bytes_on_disk) "
             "FROM system.parts WHERE active AND database NOT IN ('system','INFORMATION_SCHEMA','information_schema') "
             "GROUP BY table ORDER BY 5 DESC FORMAT JSONCompact")
        r = subprocess.run(["docker", "exec", "vbench-clickhouse-1", "clickhouse-client", "--user", "vindice",
                            "--password", "vindice", "--query", q], capture_output=True, text=True).stdout
        res["clickhouse_tables"] = json.loads(r)["data"]
    os.makedirs(os.path.join(HERE, "results"), exist_ok=True)
    with open(os.path.join(HERE, "results", f"{SYSTEM}.json"), "w") as f:
        json.dump(res, f, indent=2)
    print(json.dumps(res, indent=2))


main()
