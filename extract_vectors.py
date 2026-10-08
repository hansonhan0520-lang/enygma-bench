#!/usr/bin/env python3
"""Copy the official Enygma transfer test vectors out of the upstream Postman collection.

Usage: extract_vectors.py <path to 'Gnark API.postman_collection.json'> <output dir>

Writes k2.json, k6.json (valid vectors) and k2_bad.json, k6_bad.json (the
repository's own deliberately corrupted vectors) byte for byte as published.
"""
import json
import os
import sys

src, out = sys.argv[1], sys.argv[2]
os.makedirs(out, exist_ok=True)
items = json.load(open(src))["item"]
bodies = {
    i["name"]: i["request"]["body"]["raw"]
    for i in items
    if i.get("request", {}).get("body", {}).get("raw")
}
for name, fn in [
    ("Transaction k=6", "k6"),
    ("Transaction k=2", "k2"),
    ("Bad Transaction k=6", "k6_bad"),
    ("Bad Transaction k=2", "k2_bad"),
]:
    with open(os.path.join(out, fn + ".json"), "w") as f:
        f.write(bodies[name])
    print(f"extracted {name!r} -> {fn}.json")
