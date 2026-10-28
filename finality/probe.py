#!/usr/bin/env python3
"""Probe what each chain's JSON-RPC exposes about finality.

For every endpoint: chain id, client version, and the block returned for the
standard tags "latest", "safe" and "finalized", with every top-level field name
of the latest block so non-standard timestamp fields are visible.
Usage: probe.py NAME=URL [NAME=URL ...]
"""
import json, sys, time, urllib.request

def rpc(url, method, params):
    req = urllib.request.Request(url, data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode(),
                                 headers={"Content-Type": "application/json", "User-Agent": "enygma-bench-finality"})
    t = time.time()
    with urllib.request.urlopen(req, timeout=15) as r:
        out = json.load(r)
    return out, (time.time() - t) * 1000

for arg in sys.argv[1:]:
    name, url = arg.split("=", 1)
    print(f"== {name} {url}")
    for m, p in [("eth_chainId", []), ("web3_clientVersion", []), ("eth_syncing", [])]:
        try:
            r, ms = rpc(url, m, p)
            print(f"RESULT probe {name} {m} -> {r.get('result', r.get('error'))} ({ms:.0f}ms)")
        except Exception as e:
            print(f"RESULT probe {name} {m} -> ERROR {e}")
    for tag in ["latest", "safe", "finalized"]:
        try:
            r, ms = rpc(url, "eth_getBlockByNumber", [tag, False])
            b = r.get("result")
            if b is None:
                print(f"RESULT probe {name} tag={tag} -> {r.get('error')}")
                continue
            print(f"RESULT probe {name} tag={tag} number={int(b['number'],16)} timestamp={int(b['timestamp'],16)} hash={b['hash'][:18]} rtt={ms:.0f}ms")
            if tag == "latest":
                print(f"RESULT probe {name} fields={sorted(b.keys())}")
                extra = {k: v for k, v in b.items() if k not in ("transactions", "logsBloom", "uncles", "withdrawals")}
                print(json.dumps(extra)[:1500])
        except Exception as e:
            print(f"RESULT probe {name} tag={tag} -> ERROR {e}")
    # block-to-block timestamps for the last 20 blocks
    try:
        r, _ = rpc(url, "eth_blockNumber", [])
        n = int(r["result"], 16)
        ts = []
        for i in range(n - 19, n + 1):
            b = rpc(url, "eth_getBlockByNumber", [hex(i), False])[0]["result"]
            ts.append(int(b["timestamp"], 16))
        print(f"RESULT probe {name} last20_timestamps={ts}")
    except Exception as e:
        print(f"RESULT probe {name} last20 -> ERROR {e}")
