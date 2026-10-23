#!/usr/bin/env bash
# Controlled-CPU and sustained-load experiments on the Enygma transfer prover.
#
#   ./run_load.sh                     # clone upstream at the pinned commit and run everything
#   UPSTREAM_DIR=/path ./run_load.sh  # use an existing checkout of rayls-sovereign-gnark-api
#
# Part A, CPU pinning. The k=6 proving benchmark runs inside fixed CPU sets
# chosen from the guest topology: one CPU; two CPUs that are hyperthread
# siblings of one core (if the guest has SMT); two CPUs on different cores;
# all CPUs. The conditions run in alternating order for PIN_ROUNDS rounds so
# slow drift on the host affects every condition equally.
#
# Part B, sustained load. The real HTTP service is started unmodified and a
# closed-loop load generator keeps C requests in flight for LOAD_SECS at each
# level in LOAD_LEVELS, then holds SUSTAIN_C in flight for SUSTAIN_SECS and
# reports per-window throughput, p99 latency and peak server memory.
#
# Settings (environment):
#   PIN_ROUNDS    rounds of the pinning experiment (default 3)
#   PIN_RUNS      timed proofs per pinned condition (default 20)
#   LOAD_LEVELS   requests in flight to test (default "1 2 4 8 16")
#   LOAD_SECS     measured seconds per level (default 60), after a 5 s warm-up
#   SUSTAIN_C     requests in flight for the long run (default: number of CPUs)
#   SUSTAIN_SECS  length of the long run (default 300), reported in 30 s windows
#   LABEL         name for the log file in results/ (default: hostname)
set -euo pipefail

UPSTREAM_REPO=https://github.com/raylsnetwork/rayls-sovereign-gnark-api
UPSTREAM_COMMIT=67c4c26696016b48e70950e284ae1a51b7d0cf0e
HERE=$(cd "$(dirname "$0")" && pwd)
LABEL=${LABEL:-$(hostname)}
PIN_ROUNDS=${PIN_ROUNDS:-3}
PIN_RUNS=${PIN_RUNS:-20}
LOAD_LEVELS=${LOAD_LEVELS:-"1 2 4 8 16"}
LOAD_SECS=${LOAD_SECS:-60}
SUSTAIN_C=${SUSTAIN_C:-$(nproc)}
SUSTAIN_SECS=${SUSTAIN_SECS:-300}
export ENYGMA_VEC_DIR=${ENYGMA_VEC_DIR:-$HERE/work/vectors}
mkdir -p "$HERE/results" "$ENYGMA_VEC_DIR"
LOG="$HERE/results/$LABEL.log"

if [ -z "${UPSTREAM_DIR:-}" ]; then
  UPSTREAM_DIR=$HERE/work/upstream
  if [ ! -d "$UPSTREAM_DIR/.git" ]; then
    GIT_LFS_SKIP_SMUDGE=1 git clone -q "$UPSTREAM_REPO" "$UPSTREAM_DIR"
  fi
  git -C "$UPSTREAM_DIR" -c advice.detachedHead=false checkout -q "$UPSTREAM_COMMIT"
fi
T=./pkg/circuits/enygma/enygma-payments/enygma-transfer/
python3 "$HERE/extract_vectors.py" "$UPSTREAM_DIR/tests/Gnark API.postman_collection.json" "$ENYGMA_VEC_DIR" > /dev/null
cp "$HERE"/harness/*_test.go "$UPSTREAM_DIR/$T"
(cd "$HERE/loadgen" && go build -o /tmp/enygma-loadgen .)
cd "$UPSTREAM_DIR"

run() { echo "\$ $1"; bash -c "$1"; }
NCPU=$(nproc)

# Pick CPU sets from the guest topology. cpu0's hyperthread sibling, if any,
# forms the "same core" pair; the first CPU on a different core forms the
# "different cores" pair.
topology() {
  python3 - <<'PY'
import glob, os
cpus = sorted(int(p.rsplit('cpu', 1)[1]) for p in glob.glob('/sys/devices/system/cpu/cpu[0-9]*') if os.path.isdir(p + '/topology'))
def rd(c, f):
    try:
        return open(f'/sys/devices/system/cpu/cpu{c}/topology/{f}').read().strip()
    except OSError:
        return '?'
core = {c: (rd(c, 'physical_package_id'), rd(c, 'core_id')) for c in cpus}
for c in cpus:
    print(f"TOPO cpu{c} package={core[c][0]} core={core[c][1]} siblings={rd(c, 'thread_siblings_list')}")
c0 = cpus[0]
same = [c for c in cpus if c != c0 and core[c] == core[c0]]
diff = [c for c in cpus if core[c] != core[c0]]
print(f"SETS one={c0} same_core={(str(c0) + ',' + str(same[0])) if same else 'none'} diff_core={(str(c0) + ',' + str(diff[0])) if diff else 'none'} all={','.join(map(str, cpus))}")
PY
}

{
  run "date -u '+%Y-%m-%d %H:%M:%S UTC'; git rev-parse HEAD; nproc; uname -m; go version"
  run "lscpu | grep -E '^CPU\\(s\\)|Model name|Thread\\(s\\) per core|Core\\(s\\) per socket|Socket\\(s\\)|Vendor ID|Hypervisor vendor|Virtualization type' || true"
  run "grep -E 'MemTotal' /proc/meminfo"
  echo "\$ guest topology from /sys/devices/system/cpu (guest view, not host allocation)"
  topology
  SETS=$(topology | grep '^SETS')
  ONE=$(echo "$SETS" | sed -E 's/.*one=([^ ]+).*/\1/')
  SAME=$(echo "$SETS" | sed -E 's/.*same_core=([^ ]+).*/\1/')
  DIFF=$(echo "$SETS" | sed -E 's/.*diff_core=([^ ]+).*/\1/')
  ALL=$(echo "$SETS" | sed -E 's/.*all=([^ ]+).*/\1/')

  # Vectors and keys, as in run.sh.
  go test $T -run 'TestGeneratorReproducesPublishedVectors|TestGenerateMidVectors' -count=1 -v | grep RESULT
  go test $T -run TestSetupArtifacts -count=1 -v -timeout 60m | grep RESULT

  # ---------------------------------------------------------------- Part A
  echo "\$ part A: k=6 proving pinned with taskset, $PIN_RUNS proofs per condition, $PIN_ROUNDS alternating rounds"
  go test -c -o /tmp/enygma.test $T
  pin() {  # name cpuset
    local name=$1 set=$2
    [ "$set" = "none" ] && { echo "RESULT pin cond=$name skipped=no_such_cpu_pair"; return; }
    local n; n=$(echo "$set" | tr ',' '\n' | wc -l)
    local out
    out=$(cd $T && BENCH_KS=6 BENCH_PROCS=$n BENCH_RUNS=$PIN_RUNS taskset -c "$set" /tmp/enygma.test -test.run TestProveAcrossK -test.v 2>&1 | grep 'RESULT prove' || true)
    echo "RESULT pin round=$ROUND cond=$name cpus=$set ${out#RESULT prove }"
  }
  for ROUND in $(seq "$PIN_ROUNDS"); do
    if [ $((ROUND % 2)) -eq 1 ]; then ORDER="one same diff all"; else ORDER="all diff same one"; fi
    for c in $ORDER; do
      case $c in one) pin one "$ONE";; same) pin same_core "$SAME";; diff) pin diff_core "$DIFF";; all) pin all "$ALL";; esac
    done
  done

  # ---------------------------------------------------------------- Part B
  echo "\$ part B: real HTTP service under closed-loop load, k=6, GOMAXPROCS=$NCPU, no pinning"
  go build -o /tmp/enygma-gnark-server ./cmd/server
  SLOG=/tmp/enygma-server.log
  : > "$SLOG"
  /tmp/enygma-gnark-server >> "$SLOG" 2>&1 &
  SRV=$!
  for i in $(seq 60); do curl -sf -o /dev/null http://localhost:3003/healthcheck && break; sleep 1; done
  V=$ENYGMA_VEC_DIR/k6.json
  curl -s -o /dev/null -H 'Content-Type: application/json' --data @"$V" http://localhost:3003/generateProofTransfer-6
  sleep 2
  echo "RESULT idle_rss after_warmup=$(( $(awk '/VmRSS/{print $2}' /proc/$SRV/status) / 1024 ))MB"

  server_prove() {  # summarise the service's own Prove timings from its log
    python3 - "$SLOG" "$1" <<'PY'
import re, sys
def dur(s):
    m = re.fullmatch(r'([0-9.]+)(ns|µs|us|ms|s|m)', s)
    if not m:
        h = re.fullmatch(r'(\d+)m([0-9.]+)s', s)
        return (int(h[1]) * 60 + float(h[2])) * 1000 if h else None
    v, u = float(m[1]), m[2]
    return v * {'ns': 1e-6, 'µs': 1e-3, 'us': 1e-3, 'ms': 1, 's': 1000, 'm': 60000}[u]
xs = sorted(d for d in (dur(x) for x in re.findall(r'K=6 Timing:.*?Prove=([^,]+),', open(sys.argv[1], errors='ignore').read())) if d)
if xs:
    q = lambda p: xs[min(len(xs) - 1, max(0, int(p * len(xs) + 0.999999) - 1))]
    print(f"RESULT server_prove c={sys.argv[2]} n={len(xs)} p50={q(.5):.0f}ms p99={q(.99):.0f}ms")
PY
  }

  for C in $LOAD_LEVELS; do
    : > "$SLOG"
    ( sleep 5; : > "$SLOG" ) &
    /tmp/enygma-loadgen -url http://localhost:3003/generateProofTransfer-6 -body "$V" -c "$C" -d "${LOAD_SECS}s" -warmup 5s -pid $SRV -label k6
    server_prove "$C"
  done

  echo "\$ part B, sustained: $SUSTAIN_C in flight for ${SUSTAIN_SECS}s, 30 s windows"
  : > "$SLOG"
  /tmp/enygma-loadgen -url http://localhost:3003/generateProofTransfer-6 -body "$V" -c "$SUSTAIN_C" -d "${SUSTAIN_SECS}s" -warmup 5s -pid $SRV -window 30s -label sustained
  kill $SRV 2>/dev/null || true
  run "date -u '+%Y-%m-%d %H:%M:%S UTC'"
} 2>&1 | tee "$LOG"
echo "log written to $LOG"
