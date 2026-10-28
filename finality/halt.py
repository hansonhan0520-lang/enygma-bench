#!/usr/bin/env python3
"""Locate the longest gap between consecutive blocks inside a date range.

Binary-searches heights for the range boundaries, then repeatedly bisects
toward the half whose elapsed time most exceeds what its block count would
take at the range's typical interval, until it reaches two consecutive
blocks. Prints both blocks, the gap, and whether the later block's
parentHash is the earlier block's hash (an unbroken chain across the gap).

Usage: halt.py URL START_UTC END_UTC     e.g. 2026-06-20 2026-06-23
"""
import calendar, json, sys, time, urllib.request

url, start, end = sys.argv[1], sys.argv[2], sys.argv[3]
calls = 0
HDR = {"Content-Type": "application/json", "User-Agent": "enygma-bench-finality"}

def blk(h):
    global calls
    calls += 1
    req = urllib.request.Request(url, data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": "eth_getBlockByNumber",
                                 "params": [hex(h), False]}).encode(), headers=HDR)
    with urllib.request.urlopen(req, timeout=20) as r:
        b = json.load(r)["result"]
    return {"n": h, "ts": int(b["timestamp"], 16), "hash": b["hash"], "parent": b["parentHash"],
            "nonce": b.get("nonce"), "txs": len(b.get("transactions", []))}

def height_at(ts):
    lo, hi = 1, int(json.load(urllib.request.urlopen(urllib.request.Request(url, data=json.dumps(
        {"jsonrpc": "2.0", "id": 1, "method": "eth_blockNumber", "params": []}).encode(),
        headers=HDR), timeout=20))["result"], 16)
    while lo < hi:
        mid = (lo + hi) // 2
        if blk(mid)["ts"] < ts:
            lo = mid + 1
        else:
            hi = mid
    return lo

t0 = calendar.timegm(time.strptime(start, "%Y-%m-%d"))
t1 = calendar.timegm(time.strptime(end, "%Y-%m-%d"))
a, b = height_at(t0), height_at(t1)
A, B = blk(a), blk(b)
typical = (B["ts"] - A["ts"]) / (b - a)
lo, hi, L, H = a, b, A, B
while hi - lo > 1:
    m = (lo + hi) // 2
    M = blk(m)
    left = (M["ts"] - L["ts"]) - (m - lo) * typical
    right = (H["ts"] - M["ts"]) - (hi - m) * typical
    if left >= right:
        hi, H = m, M
    else:
        lo, L = m, M
fmt = lambda t: time.strftime("%Y-%m-%d %H:%M:%S UTC", time.gmtime(t))
nonce_round = lambda x: int(x, 16) & 0xFFFFFFFF if x else None
nonce_epoch = lambda x: int(x, 16) >> 32 if x else None
print(f"RESULT halt range={start}..{end} heights={a}..{b} blocks={b - a} mean_interval_incl_gap={typical:.3f}s")
print(f"RESULT halt before height={L['n']} time={fmt(L['ts'])} epoch={nonce_epoch(L['nonce'])} round={nonce_round(L['nonce'])} txs={L['txs']} hash={L['hash']}")
print(f"RESULT halt after  height={H['n']} time={fmt(H['ts'])} epoch={nonce_epoch(H['nonce'])} round={nonce_round(H['nonce'])} txs={H['txs']} parent={H['parent']}")
gap = H["ts"] - L["ts"]
print(f"RESULT halt gap={gap}s ({gap / 3600:.2f} h) parent_links_across_gap={H['parent'] == L['hash']} rpc_calls={calls}")
