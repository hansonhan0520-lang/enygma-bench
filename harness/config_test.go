package enygma

// Settings and extra measurements for the enygma-bench harness.
// Everything here runs against the unmodified upstream package; nothing in the
// repository's circuits or proving code is changed.

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	primitives "github.com/raylsnetwork/rayls-sovereign-gnark-api/primitives"
)

// vecDir is where the extracted and generated vectors live (ENYGMA_VEC_DIR, default /tmp/vec).
func vecDir() string {
	if d := os.Getenv("ENYGMA_VEC_DIR"); d != "" {
		return d
	}
	return "/tmp/vec"
}

// benchProcs lists the GOMAXPROCS values to time (BENCH_PROCS, default "1,2").
func benchProcs() []int {
	spec := os.Getenv("BENCH_PROCS")
	if spec == "" {
		spec = "1,2"
	}
	var out []int
	for _, f := range strings.Split(spec, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// benchRuns is the number of timed proofs per configuration (BENCH_RUNS, default 20).
func benchRuns() int {
	if n, err := strconv.Atoi(os.Getenv("BENCH_RUNS")); err == nil && n > 0 {
		return n
	}
	return 20
}

// cpuModel reads the processor name so every log says which hardware produced it.
func cpuModel() string {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	for _, l := range strings.Split(string(b), "\n") {
		for _, key := range []string{"model name", "Model", "CPU part"} {
			if strings.HasPrefix(l, key) {
				if i := strings.Index(l, ":"); i >= 0 {
					return strings.TrimSpace(l[i+1:])
				}
			}
		}
	}
	return "unknown"
}

func medianDur(d []time.Duration) time.Duration {
	c := append([]time.Duration{}, d...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	return c[len(c)/2]
}

// TestKeyLoad times what the HTTP service does on the first request for each k:
// read the constraint system and the proving key from last_build/. This is the
// candidate cause of the cold start, measured rather than assumed. The first of
// five reads is reported separately from the median of the other four.
func TestKeyLoad(t *testing.T) {
	for _, k := range []int{2, 3, 4, 5, 6} {
		var ccsD, pkD []time.Duration
		for i := 0; i < 5; i++ {
			st := time.Now()
			_ = loadCCS(t, k)
			ccsD = append(ccsD, time.Since(st))
			st = time.Now()
			if _, err := primitives.LoadProvingKey(ecc.BN254, fmt.Sprintf("../../../../../last_build/keys/Enygmak%dPk.key", k)); err != nil {
				t.Fatal(err)
			}
			pkD = append(pkD, time.Since(st))
		}
		fmt.Printf("RESULT keyload k=%d ccs_first=%v ccs_rest=%v pk_first=%v pk_rest=%v\n", k,
			ccsD[0].Round(time.Millisecond), medianDur(ccsD[1:]).Round(time.Millisecond),
			pkD[0].Round(time.Millisecond), medianDur(pkD[1:]).Round(time.Millisecond))
	}
}
