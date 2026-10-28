#!/usr/bin/env python3
"""Re-check recorded block hashes, any time after a sampling run.

Reads the *-polls.csv.gz files written by sample.py, collects every
(height, hash) pair seen as "latest" and as "finalized", fetches each height
again, and reports how many hashes have changed since they were recorded.
Requests are throttled and HTTP 429 is retried with backoff.

Usage: recheck.py NAME=URL CSV_GLOB [NAME=URL CSV_GLOB ...]
"""
import csv, glob, gzip, json, sys, time, urllib.error, urllib.request

def post(url, payload, tries=7):
    req = urllib.request.Request(url, data=json.dumps(payload).encode(),
                                 headers={"Content-Type": "application/json", "User-Agent": "enygma-bench-finality"})
    for i in range(tries):
        try:
            with urllib.request.urlopen(req, timeout=20) as r:
                return json.load(r)
        except urllib.error.HTTPError as e:
            if e.code != 429 or i == tries - 1:
                raise
            time.sleep(2 ** i)

args = sys.argv[1:]
print(f"$ recheck started {time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime())}", flush=True)
for spec, pattern in zip(args[0::2], args[1::2]):
    name, url = spec.split("=", 1)
    for f in sorted(glob.glob(pattern)):
        latest, final, first_wall, last_wall = {}, {}, None, None
        with gzip.open(f, "rt") as fh:
            for row in csv.DictReader(fh):
                latest.setdefault(int(row["latest"]), row["latest_hash"])
                final.setdefault(int(row["finalized"]), row["finalized_hash"])
                w = float(row["wall_time"])
                first_wall = first_wall or w
                last_wall = w
        pairs = dict(latest)
        for h, v in final.items():
            pairs.setdefault(h, v)
        changed = checked = failed = 0
        for h, want in sorted(pairs.items()):
            time.sleep(0.12)
            try:
                got = post(url, {"jsonrpc": "2.0", "id": 1, "method": "eth_getBlockByNumber", "params": [hex(h), False]})["result"]["hash"]
                checked += 1
                changed += got != want
            except Exception:
                failed += 1
        age_h = (time.time() - last_wall) / 3600
        print(f"RESULT recheck {name} file={f.split('/')[-1]} heights_latest={len(latest)} heights_finalized={len(final)} "
              f"checked={checked} changed={changed} failed={failed} hours_since_sampling={age_h:.1f}", flush=True)
print(f"$ recheck finished {time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime())}")
