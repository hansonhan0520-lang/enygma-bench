#!/usr/bin/env python3
"""Measure finality as each chain's own JSON-RPC reports it, and test it.

For each chain, in parallel, for DURATION seconds:
  * Every POLL seconds, one batched request returns the "latest", "safe" and
    "finalized" blocks together, so the three are read at the same moment.
  * The hash of every height is recorded the first time it is seen as latest.
At the end:
  * Every recorded height is fetched again and its hash compared with the one
    first seen. A changed hash means the head was replaced (a reorg).
  * The same is done for every height first seen as finalized.
For chains named in HISTORY, block timestamps are also read at evenly spaced
heights from genesis to the current head, to show how the block interval has
changed over the chain's life.

Usage: sample.py NAME=URL[,poll_seconds] ...
Env:   DURATION (default 3600), HISTORY (comma-separated names), OUT (dir)
"""
import csv, json, os, statistics as st, sys, threading, time, urllib.error, urllib.request

DURATION = float(os.environ.get("DURATION", "3600"))
HISTORY = set(filter(None, os.environ.get("HISTORY", "").split(",")))
OUT = os.environ.get("OUT", "results")
LABEL = os.environ.get("LABEL", "finality")


def post(url, payload, timeout=20, tries=6):
    """POST JSON-RPC. HTTP 429 (rate limited) is retried with exponential backoff."""
    req = urllib.request.Request(url, data=json.dumps(payload).encode(),
                                 headers={"Content-Type": "application/json", "User-Agent": "enygma-bench-finality"})
    for i in range(tries):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as r:
                return json.load(r)
        except urllib.error.HTTPError as e:
            if e.code != 429 or i == tries - 1:
                raise
            time.sleep(2 ** i)


def block(url, tag):
    return post(url, {"jsonrpc": "2.0", "id": 1, "method": "eth_getBlockByNumber", "params": [tag, False]})["result"]


def heads(url):
    batch = [{"jsonrpc": "2.0", "id": i, "method": "eth_getBlockByNumber", "params": [t, False]}
             for i, t in enumerate(["latest", "safe", "finalized"])]
    res = post(url, batch)
    res = sorted(res, key=lambda r: r["id"])
    return [r["result"] for r in res]


def q(xs, p):
    xs = sorted(xs)
    if not xs:
        return float("nan")
    return xs[min(len(xs) - 1, max(0, int(p * len(xs) + 0.999999) - 1))]


def run_chain(name, url, poll, results):
    rows = []                 # one row per poll
    first_latest = {}         # height -> (hash, wall time first seen, block timestamp)
    first_final = {}          # height -> hash, for heights first seen as finalized
    errors = 0
    t_end = time.time() + DURATION
    while time.time() < t_end:
        t0 = time.time()
        try:
            lat, safe, fin = heads(url)
        except Exception:
            errors += 1
            time.sleep(poll)
            continue
        t1 = time.time()
        ln, fn, sn = int(lat["number"], 16), int(fin["number"], 16), int(safe["number"], 16)
        lts, fts = int(lat["timestamp"], 16), int(fin["timestamp"], 16)
        rows.append([round(t1, 3), round((t1 - t0) * 1000, 1), ln, sn, fn, lts, fts, lat["hash"], fin["hash"]])
        if ln not in first_latest:
            first_latest[ln] = (lat["hash"], t1, lts)
        if fn not in first_final:
            first_final[fn] = fin["hash"]
        time.sleep(max(0.0, poll - (time.time() - t0)))

    # Re-check every recorded height after the window closes.
    def recheck(hashes):
        changed = checked = failed = 0
        for h, want in hashes.items():
            time.sleep(0.1)
            try:
                got = block(url, hex(h))["hash"]
                checked += 1
                changed += got != want
            except Exception:
                failed += 1
        return changed, checked, failed

    lat_changed, lat_checked, lat_failed = recheck({h: v[0] for h, v in first_latest.items()})
    fin_changed, fin_checked, fin_failed = recheck(first_final)

    lag_blocks = [r[2] - r[4] for r in rows]
    lag_secs = [r[5] - r[6] for r in rows]
    safe_lag = [r[2] - r[3] for r in rows]
    rtt = [r[1] for r in rows]
    hs = sorted(first_latest)
    span_blocks = hs[-1] - hs[0] if hs else 0
    span_ts = first_latest[hs[-1]][2] - first_latest[hs[0]][2] if hs else 0
    # observer delay: first wall-clock sighting minus the block's own timestamp (1 s resolution)
    delay = [v[1] - v[2] for v in first_latest.values()]
    # per-block timestamp deltas for consecutive heights we saw
    deltas = [first_latest[h][2] - first_latest[h - 1][2] for h in hs if h - 1 in first_latest]
    hist = {}
    for d in deltas:
        hist[d] = hist.get(d, 0) + 1

    os.makedirs(OUT, exist_ok=True)
    with open(os.path.join(OUT, f"{LABEL}-{name}-polls.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["wall_time", "rtt_ms", "latest", "safe", "finalized", "latest_ts", "finalized_ts", "latest_hash", "finalized_hash"])
        w.writerows(rows)

    lines = [
        f"RESULT finality {name} polls={len(rows)} errors={errors} poll={poll}s window={DURATION:.0f}s rtt_p50={q(rtt, .5):.0f}ms",
        f"RESULT finality {name} finalized_lag_blocks min={min(lag_blocks)} p50={q(lag_blocks, .5)} max={max(lag_blocks)} "
        f"finalized_lag_secs min={min(lag_secs)} p50={q(lag_secs, .5)} max={max(lag_secs)} "
        f"polls_with_finalized_eq_latest={sum(1 for x in lag_blocks if x == 0)}",
        f"RESULT finality {name} safe_lag_blocks min={min(safe_lag)} p50={q(safe_lag, .5)} max={max(safe_lag)}",
        f"RESULT finality {name} heights_seen={len(first_latest)} span_blocks={span_blocks} span_secs={span_ts} "
        f"mean_block_interval={span_ts / span_blocks if span_blocks else float('nan'):.3f}s",
        f"RESULT finality {name} block_ts_delta_histogram={json.dumps(dict(sorted(hist.items())))}",
        f"RESULT finality {name} observer_delay_secs p50={q(delay, .5):.2f} p95={q(delay, .95):.2f} (block timestamps have 1 s resolution)",
        f"RESULT finality {name} recheck_latest changed={lat_changed} checked={lat_checked} failed={lat_failed}",
        f"RESULT finality {name} recheck_finalized changed={fin_changed} checked={fin_checked} failed={fin_failed}",
    ]
    results[name] = lines  # keep the summary even if the history step fails
    if name in HISTORY:
        head = int(post(url, {"jsonrpc": "2.0", "id": 1, "method": "eth_blockNumber", "params": []})["result"], 16)
        step = max(1, head // 60)
        prev = None
        for h in list(range(1, head, step)) + [head]:
            try:
                b = block(url, hex(h))
                ts = int(b["timestamp"], 16)
                if prev:
                    ph, pts = prev
                    lines.append(f"RESULT history {name} from={ph} to={h} "
                                 f"from_utc={time.strftime('%Y-%m-%d', time.gmtime(pts))} to_utc={time.strftime('%Y-%m-%d', time.gmtime(ts))} "
                                 f"mean_interval={(ts - pts) / (h - ph):.3f}s")
                prev = (h, ts)
            except Exception as e:
                lines.append(f"RESULT history {name} height={h} error={e}")
    results[name] = lines


def main():
    results = {}
    threads = []
    for arg in sys.argv[1:]:
        name, rest = arg.split("=", 1)
        url, _, poll = rest.partition(",")
        th = threading.Thread(target=run_chain, args=(name, url, float(poll or 1.0), results))
        th.start()
        threads.append(th)
    print(f"$ sampling {len(threads)} chains for {DURATION:.0f}s, started {time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime())}", flush=True)
    for th in threads:
        th.join()
    print(f"$ finished {time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime())}")
    for name in sorted(results):
        print("\n".join(results[name]))


if __name__ == "__main__":
    main()
