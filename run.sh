#!/usr/bin/env bash
# Reproduce the Enygma transfer-proof benchmark against an unmodified upstream commit.
#
#   ./run.sh                     # clone upstream at the pinned commit and run everything
#   UPSTREAM_DIR=/path ./run.sh  # use an existing checkout of rayls-sovereign-gnark-api
#
# Settings (environment):
#   BENCH_PROCS  GOMAXPROCS values to time, comma separated (default "1,2")
#   BENCH_RUNS   timed proofs per configuration (default 20)
#   HTTP_RUNS    timed HTTP requests per k through the real service (default 10, 0 to skip)
#   LABEL        name for the log file in results/ (default: hostname)
set -euo pipefail

UPSTREAM_REPO=https://github.com/raylsnetwork/rayls-sovereign-gnark-api
UPSTREAM_COMMIT=67c4c26696016b48e70950e284ae1a51b7d0cf0e
HERE=$(cd "$(dirname "$0")" && pwd)
LABEL=${LABEL:-$(hostname)}
HTTP_RUNS=${HTTP_RUNS:-10}
export ENYGMA_VEC_DIR=${ENYGMA_VEC_DIR:-$HERE/work/vectors}
mkdir -p "$HERE/results" "$ENYGMA_VEC_DIR"
LOG="$HERE/results/$LABEL.log"

# 1. Upstream source at the pinned commit. Circuits and proving code are not modified.
if [ -z "${UPSTREAM_DIR:-}" ]; then
  UPSTREAM_DIR=$HERE/work/upstream
  if [ ! -d "$UPSTREAM_DIR/.git" ]; then
    GIT_LFS_SKIP_SMUDGE=1 git clone -q "$UPSTREAM_REPO" "$UPSTREAM_DIR"
  fi
  git -C "$UPSTREAM_DIR" -c advice.detachedHead=false checkout -q "$UPSTREAM_COMMIT"
fi
T=./pkg/circuits/enygma/enygma-payments/enygma-transfer/

# 2. Official vectors from the upstream Postman collection, plus the harness files.
python3 "$HERE/extract_vectors.py" "$UPSTREAM_DIR/tests/Gnark API.postman_collection.json" "$ENYGMA_VEC_DIR" > /dev/null
cp "$HERE"/harness/*_test.go "$UPSTREAM_DIR/$T"
cd "$UPSTREAM_DIR"

run() { echo "\$ $1"; bash -c "$1"; }
{
  run "date -u '+%Y-%m-%d %H:%M:%S UTC'; git rev-parse HEAD; nproc; uname -m; go version"
  # 3. Generator: regenerate both published vectors field for field, then build k=3,4,5.
  run "go test $T -run 'TestGeneratorReproducesPublishedVectors|TestGenerateMidVectors' -count=1 -v | grep RESULT"
  # 4. Compile all five circuits and run a local groth16.Setup into last_build/.
  run "go test $T -run TestSetupArtifacts -count=1 -v -timeout 60m | grep RESULT"
  # 5. Main benchmark: verify once, then time BENCH_RUNS proofs at each GOMAXPROCS value.
  run "go test $T -run TestProveAcrossK -count=1 -v -timeout 120m | grep RESULT"
  # 6. Negative tests, sizes, and the cost of loading keys from disk.
  run "go test $T -run TestRejectsBadVectors -count=1 -v | grep RESULT | cut -c1-110"
  run "go test $T -run TestSizes -count=1 -v | grep RESULT"
  run "go test $T -run TestKeyLoad -count=1 -v | grep RESULT"

  # 7. End to end through the real HTTP service.
  if [ "$HTTP_RUNS" -gt 0 ]; then
    go build -o /tmp/enygma-gnark-server ./cmd/server
    /tmp/enygma-gnark-server > /tmp/enygma-server.log 2>&1 &
    SRV=$!
    for i in $(seq 60); do curl -sf -o /dev/null http://localhost:3003/healthcheck && break; sleep 1; done
    echo "\$ for k in 2..6: one cold request, then $HTTP_RUNS timed requests; report median"
    for k in 2 3 4 5 6; do
      V=$ENYGMA_VEC_DIR/k$k.json; [ -f "$V" ] || V=$ENYGMA_VEC_DIR/gen_k$k.json
      cold=$(curl -s -o /dev/null -w '%{time_total}' -H 'Content-Type: application/json' --data @"$V" "http://localhost:3003/generateProofTransfer-$k")
      ts=""
      for i in $(seq "$HTTP_RUNS"); do
        ts="$ts $(curl -s -o /dev/null -w '%{time_total}' -H 'Content-Type: application/json' --data @"$V" "http://localhost:3003/generateProofTransfer-$k")"
      done
      med=$(echo $ts | tr ' ' '\n' | sort -n | awk '{a[NR]=$1} END{print a[int((NR+1)/2)]}')
      echo "RESULT http k=$k cold=${cold}s median=${med}s n=$HTTP_RUNS"
    done
    kill $SRV 2>/dev/null || true
  fi
  run "date -u '+%Y-%m-%d %H:%M:%S UTC'"
} 2>&1 | tee "$LOG"
echo "log written to $LOG"
