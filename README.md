# enygma-bench

A reproducible benchmark of the Enygma confidential-transfer prover published by Rayls in [`raylsnetwork/rayls-sovereign-gnark-api`](https://github.com/raylsnetwork/rayls-sovereign-gnark-api), pinned to commit [`67c4c26`](https://github.com/raylsnetwork/rayls-sovereign-gnark-api/commit/67c4c26696016b48e70950e284ae1a51b7d0cf0e).

It answers one question with measurements rather than estimates: **how much compute does generating a zero-knowledge proof for one private transfer take, and how does that change with the anonymity-set size k, the number of cores, and the hardware?**

## What is and is not modified

Nothing in the upstream circuits, prover or service is changed. `run.sh` checks out the pinned commit, copies the test files in `harness/` into the upstream package `pkg/circuits/enygma/enygma-payments/enygma-transfer/` (Go test files in the same package can call its unexported functions), and runs them. Keys are produced locally with the repository's own `groth16.Setup`; the repository's README states its shipped keys are single-party development artifacts too, and proving time depends on circuit structure rather than key values.

The only other difference from a plain upstream build is dependency resolution in environments that cannot reach `proxy.golang.org`, handled with `replace` directives to GitHub mirrors. On GitHub Actions no change is needed.

## What the harness measures

| Test | What it does |
|---|---|
| `TestGeneratorReproducesPublishedVectors` | The upstream Postman collection ships valid vectors only for k=2 and k=6. `vecgen_test.go` derives every circuit-constrained field from a vector's seeds following `common/common-checks.go`, and must regenerate both published vectors field for field before it is trusted. |
| `TestGenerateMidVectors` | Builds k=3, 4 and 5 vectors from the k=6 seeds with the same derivation. |
| `TestSetupArtifacts` | Compiles all five transfer circuits and writes r1cs, proving and verifying keys into `last_build/`, the layout the HTTP service loads. |
| `TestProveAcrossK` | For each k: one proof, verified against its verifying key, then `BENCH_RUNS` timed proofs through the repository's own proof function at each `GOMAXPROCS` value in `BENCH_PROCS`. Reports min, median and max. |
| `TestRejectsBadVectors` | Feeds the repository's deliberately corrupted vectors in and shows the unsatisfied constraint. |
| `TestSizes` | Constraint count, evaluation-domain size, proof bytes, public-witness bytes and proving-key bytes per k. |
| `TestKeyLoad` | Times reading the constraint system and the proving key from disk, which the service does once per k on its first request. |
| HTTP section of `run.sh` | Starts the real service, sends one cold request and `HTTP_RUNS` timed requests per k, reports the median. |

## Load and controlled-CPU experiments

`run_load.sh` (workflow `enygma-load`) adds two experiments on the same unmodified upstream commit, k=6:

- **CPU pinning.** The proving benchmark runs inside fixed CPU sets chosen from the guest topology with `taskset`: one CPU, two hyperthread siblings of one core (when the guest has SMT), two CPUs on different cores, and all CPUs. Conditions alternate order across rounds so host drift hits each one equally. This is a controlled experiment inside the guest; it still cannot see how the host maps vCPUs to physical cores.
- **Sustained load.** The real HTTP service is started as shipped, and `loadgen/` (standard library only) keeps 1, 2, 4, 8 and 16 requests in flight for 60 s each, recording every request's latency (p50, p95, p99), throughput, and the server's resident memory sampled every 100 ms. The service's own per-request `Prove` timings are summarised alongside. A final 5-minute run holds one request per CPU in flight and reports 30 s windows.

The workflow runs two independent jobs on each of the x64 and arm64 runners, so every result is repeated on a separately provisioned machine. Logs land in `results/load-*.log`.

These measure the prover service under load. They are not measurements of Rayls settlement throughput, which also involves the ledgers, the relayer and the Private Network Hub.

## Finality experiments

`finality/` asks a different question from the prover benchmarks: when is a transfer final? It reads the chains' own JSON-RPC, nothing is modified, and every output is committed to `results/`.

- **`finality.yml` / `finality/sample.py`.** Samples the Rayls public chain (`mainnet-rpc.rayls.com`, every 0.25 s) and Ethereum mainnet (PublicNode, every 1 s) in parallel for an hour. Each poll is one batched request for the `latest`, `safe` and `finalized` blocks, so the three are read together. The hash of every height is recorded when first seen; after the window every height is fetched again and compared, which detects any replaced block. For Rayls it also reads block timestamps at 60 evenly spaced heights from genesis to show how the block interval has changed.
- **`finality-halt.yml` / `finality/halt.py`.** Bisects block timestamps inside a date range to find the longest gap between consecutive blocks, and checks that the block after the gap has the block before it as its parent.
- **`finality-probe.yml`.** One-shot check of what each RPC reports.

The Rayls RPC reports `web3_clientVersion` `1.4.1 (6909618…)`, the public tag `mainnet-v1.4.1` of [raylsnetwork/axyl](https://github.com/raylsnetwork/axyl). In that tag, `crates/middleware/processor/src/execution/orchestrator.rs` (lines 268 to 269) executes each consensus output and immediately calls `finalize_block`, which sets both `finalized` and `safe` to that block (`crates/execution/evm/src/reth_env/persistence.rs`, lines 27 and 33).

Results so far:

| | Rayls public chain | Ethereum mainnet |
|---|---|---|
| Run 40642493952, 2026-10-28 06:12 to 07:12 UTC | 14,392 polls, `finalized` = `latest` in all of them | `finalized` 62 to 95 blocks behind (756 to 1,152 s, median 960 s) |
| Mean block interval in the hour | 1.350 s | 12.081 s |
| Heights re-checked after the hour, hashes changed | 2,661 checked, 0 changed | 299 checked, 0 changed |
| Run 40649630038, 07:22 to 08:22 UTC | 13,396 polls, `finalized` = `latest` in all of them, mean interval 1.347 s (recomputed from the polls CSV: the public RPC returned HTTP 429 during the closing history step and the summary was lost; `sample.py` now retries 429) | `finalized` 63 to 95 blocks behind (756 to 1,140 s, median 960 s), mean interval 12.040 s |
| Delayed re-check (`finality-recheck`), 0.3 to 1.4 h after sampling | 5,325 heights from both runs, 0 changed | 605 heights, 0 changed |

Two halts located with `finality-halt` (block after the gap links to the block before it in both cases, so the chain resumed rather than forked):

| Gap | Length | Epoch, rounds | Log |
|---|---|---|---|
| 2026-05-05 21:59:32 to 2026-05-06 09:42:29 UTC, heights 351198 to 351199 | 11.72 h | 6, 33416 to 33420 | [log](results/finality-halt-run40649527506.log) |
| 2026-06-21 01:05:33 to 10:14:47 UTC, heights 3367007 to 3367008 | 9.15 h | 52, 50386 to 50394 | [log](results/finality-halt-run40642835254.log) |

Only the longest gap in each date range was searched, so shorter halts are not excluded. An hour without an Ethereum reorg says nothing about Ethereum's reorg rate.

## Run it

```bash
./run.sh                                   # clones upstream at the pinned commit
BENCH_PROCS=1,2,4 BENCH_RUNS=20 ./run.sh   # choose core counts and sample size
```

Requires Go 1.26, git, python3 and curl. The log lands in `results/<hostname>.log`.

Every push to `main` that touches the harness, and every manual run from the Actions tab, runs the same script on two GitHub-hosted machines, x64 and arm64, at 1, 2 and 4 cores. Their logs are public in the Actions tab and attached to each run as artifacts.

## Results

Transfer circuit, anonymity set k=6, median of 20 proofs per configuration. GitHub figures are from Actions run [37741247183](https://github.com/hansonhan0520-lang/enygma-bench/actions/runs/37741247183) on 2026-10-08; the cloud VM figures are from a local run the same day. Every figure is parsed from the logs into [`data/bench_data.json`](data/bench_data.json), which is the single source for the article that cites this repository.

| Machine | k=6, 1 vCPU | 2 vCPU | 4 vCPU | 2→4 vCPU speedup | verify | proving-key load | cold HTTP request | warm HTTP request | Log |
|---|---|---|---|---|---|---|---|---|---|
| GitHub x64 (`ubuntu-24.04`), Intel Xeon Platinum 8573C, 2 cores × 2 threads | 1.55 s | 0.82 s | 0.71 s | 1.17× | 0.98 ms | 2.07 s | 2.84 s | 0.70 s | [log](results/github-x64-run37741247183.log) |
| GitHub arm64 (`ubuntu-24.04-arm`), ARM Neoverse-N2, 4 cores × 1 thread | 1.81 s | 0.95 s | 0.63 s | 1.51× | 1.24 ms | 1.94 s | 2.62 s | 0.62 s | [log](results/github-arm64-run37741247183.log) |
| Cloud VM, Intel Xeon @ 2.10GHz, 2 cores × 1 thread | 1.95 s | 1.37 s | n/a | n/a | 1.28 ms | 2.94 s | 4.12 s | 1.30 s | [log](results/claude-cloud-x64-2vcpu-20261008.log) |

CPU topology comes from `lscpu` at the top of each log (the cloud VM's was read in the same container, since that log predates the `lscpu` line). An earlier Actions run, [37740445393](https://github.com/hansonhan0520-lang/enygma-bench/actions/runs/37740445393), received an AMD EPYC 9V45 on the same x64 runner label; its logs are kept in `results/` but its topology was not recorded.

### Community runs

Runs reported by other people using this harness. They are listed as reported; the logs are not in this repository.

| Who | Machine | k=6, 1 vCPU | 2 vCPU | 4 vCPU | 2→4 vCPU speedup | Runs |
|---|---|---|---|---|---|---|
| turrizt (Rayls Discord), 2026-10-08 | Contabo VPS, AMD EPYC (with IBPB), 6 vCPU, guest `lscpu`: 6 cores × 1 thread, QEMU, shared with other services | 2.41 s | 1.28 s | 0.97 s | 1.32× | 10 per configuration; proof, verification and invalid-input tests all passed |

This run qualifies the topology reading above. The guest reports one thread per core, yet 2 to 4 vCPU gave 1.32×, below the ARM runner's 1.51×. Inside a virtual machine `lscpu` shows the guest topology, not how the host maps vCPUs onto physical cores and hyperthreads, and this VPS was also running other services. So guest topology is a hint about scaling, not a measurement of its cause.

### Load and pinning results

Actions run [39977915567](https://github.com/hansonhan0520-lang/enygma-bench/actions/runs/39977915567), 2026-10-23, four independently provisioned 4 vCPU runners, k=6 transfer proof through the unmodified HTTP service. 4,671 requests, 0 errors. Parsed figures: [`data/load_data.json`](data/load_data.json).

Throughput by requests in flight (closed loop, 60 s each after a 5 s warm-up):

| Machine | 1 in flight | 2 | 4 | 8 | 16 | p50 at 1 → 16 | p99 at 16 | peak RSS at 1 → 16 | 5-min run, 4 in flight | Log |
|---|---|---|---|---|---|---|---|---|---|---|
| ARM Neoverse-N2 (arm64, a) | 1.58/s | 2.08/s | 2.14/s | 2.06/s | 2.05/s | 0.63 s → 7.10 s | 11.33 s | 254 → 718 MB | 2.15/s, windows 2.03 to 2.23/s, 0 errors | [log](results/load-arm64-a-run39977915567.log) |
| ARM Neoverse-N2 (arm64, b) | 1.59/s | 2.09/s | 2.12/s | 2.07/s | 2.02/s | 0.62 s → 7.25 s | 10.00 s | 258 → 726 MB | 2.17/s, windows 2.13 to 2.23/s, 0 errors | [log](results/load-arm64-b-run39977915567.log) |
| AMD EPYC 9V45 (x64, a) | 1.93/s | 2.27/s | 2.31/s | 2.23/s | 2.16/s | 0.52 s → 6.70 s | 11.55 s | 264 → 682 MB | 2.29/s, windows 2.13 to 2.37/s, 0 errors | [log](results/load-x64-a-run39977915567.log) |
| AMD EPYC 7763 (x64, b) | 1.16/s | 1.31/s | 1.34/s | 1.33/s | 1.20/s | 0.86 s → 11.81 s | 13.91 s | 269 → 747 MB | 1.35/s, windows 1.30 to 1.43/s, 0 errors | [log](results/load-x64-b-run39977915567.log) |

At 16 in flight the service's own `Prove` timing is within 3% of the client-measured latency on every machine: requests are not queued at the door, they all start proving at once and share the CPUs. The service has no concurrency limit.

CPU pinning with `taskset`, k=6 proof, median of 20 proofs per condition, median of 3 alternating rounds (speedup against 1 CPU):

| Machine | guest topology | 1 CPU | 2 CPUs, same core | 2 CPUs, different cores | all 4 |
|---|---|---|---|---|---|
| ARM Neoverse-N2 (arm64, a) | 4 cores × 1 thread | 1.83 s | n/a (no SMT) | 1.16 s (1.58×) | 0.63 s (2.92×) |
| ARM Neoverse-N2 (arm64, b) | 4 cores × 1 thread | 1.83 s | n/a (no SMT) | 1.23 s (1.49×) | 0.63 s (2.89×) |
| AMD EPYC 9V45 (x64, a) | 2 cores × 2 threads | 1.17 s | 0.99 s (1.18×) | 0.74 s (1.57×) | 0.52 s (2.24×) |
| AMD EPYC 7763 (x64, b) | 2 cores × 2 threads | 1.80 s | 1.64 s (1.10×) | 1.17 s (1.53×) | 0.85 s (2.11×) |

Pinned figures compare conditions on one machine. Two pinned CPUs gave about 1.5×, below the 1.9× seen earlier with `GOMAXPROCS=2` and no pinning; the cause was not isolated. Topology is the guest's view, not the host's allocation.

Notes on reading these numbers:

- The proving-key load is what the service does once per k on its first request. Load time plus one warm request comes within 22 ms, 12 ms and 0.2 s of the measured cold request on the three machines.
- Constraint count, evaluation domain, proof size (164 B) and public-witness size are identical on every machine and both architectures: the k=5 to k=6 cost step (65,536 to 131,072 evaluation domain) comes from the circuit, not the CPU.
- These are proof-generation timings for one circuit with requests handled one at a time. They are not end-to-end settlement throughput.
