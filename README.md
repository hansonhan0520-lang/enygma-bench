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

See `results/` and the Actions runs. Figures quoted in articles name the run they came from.
