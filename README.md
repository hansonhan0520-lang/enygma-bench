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

Notes on reading these numbers:

- The proving-key load is what the service does once per k on its first request. Load time plus one warm request comes within 22 ms, 12 ms and 0.2 s of the measured cold request on the three machines.
- Constraint count, evaluation domain, proof size (164 B) and public-witness size are identical on every machine and both architectures: the k=5 to k=6 cost step (65,536 to 131,072 evaluation domain) comes from the circuit, not the CPU.
- These are proof-generation timings for one circuit with requests handled one at a time. They are not end-to-end settlement throughput.
