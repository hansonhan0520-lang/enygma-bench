package enygma

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/constraint"
	primitives "github.com/raylsnetwork/rayls-sovereign-gnark-api/primitives"
)

func emptyCircuit(k int) frontend.Circuit {
	switch k {
	case 2: return &Enygmak2Circuit{}
	case 3: return &Enygmak3Circuit{}
	case 4: return &Enygmak4Circuit{}
	case 5: return &Enygmak5Circuit{}
	case 6: return &Enygmak6Circuit{}
	}
	panic("k")
}

func vectorPath(k int) string {
	if k == 2 || k == 6 { return fmt.Sprintf("%s/k%d.json", vecDir(), k) }
	return fmt.Sprintf("%s/gen_k%d.json", vecDir(), k)
}

// buildWitness loads the vector for k, runs the repo's own validation and
// witness setter, and returns a filled circuit plus a prove closure.
func buildWitness(t *testing.T, k int, path string) (frontend.Circuit, func(*CompiledCircuit) error) {
	b, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	switch k {
	case 2:
		var r Enygmak2Request
		if err := json.Unmarshal(b, &r); err != nil { t.Fatal(err) }
		if err := validateInputsK2(&r); err != nil { t.Fatal(err) }
		var w Enygmak2Circuit; setWitness2Optimized(&w, &r)
		return &w, func(c *CompiledCircuit) error { var wi Enygmak2Circuit; setWitness2Optimized(&wi, &r); _, e := generateProofK2(&wi, c, &r); return e }
	case 3:
		var r Enygmak3Request
		if err := json.Unmarshal(b, &r); err != nil { t.Fatal(err) }
		if err := validateInputsK3(&r); err != nil { t.Fatal(err) }
		var w Enygmak3Circuit; setWitness3Optimized(&w, &r)
		return &w, func(c *CompiledCircuit) error { var wi Enygmak3Circuit; setWitness3Optimized(&wi, &r); _, e := generateProofK3(&wi, c, &r); return e }
	case 4:
		var r Enygmak4Request
		if err := json.Unmarshal(b, &r); err != nil { t.Fatal(err) }
		if err := validateInputsK4(&r); err != nil { t.Fatal(err) }
		var w Enygmak4Circuit; setWitness4Optimized(&w, &r)
		return &w, func(c *CompiledCircuit) error { var wi Enygmak4Circuit; setWitness4Optimized(&wi, &r); _, e := generateProofK4(&wi, c, &r); return e }
	case 5:
		var r Enygmak5Request
		if err := json.Unmarshal(b, &r); err != nil { t.Fatal(err) }
		if err := validateInputsK5(&r); err != nil { t.Fatal(err) }
		var w Enygmak5Circuit; setWitness5Optimized(&w, &r)
		return &w, func(c *CompiledCircuit) error { var wi Enygmak5Circuit; setWitness5Optimized(&wi, &r); _, e := generateProofK5(&wi, c, &r); return e }
	case 6:
		var r Enygmak6Request
		if err := json.Unmarshal(b, &r); err != nil { t.Fatal(err) }
		if err := validateInputsK6(&r); err != nil { t.Fatal(err) }
		var w Enygmak6Circuit; setWitness6Optimized(&w, &r)
		return &w, func(c *CompiledCircuit) error { var wi Enygmak6Circuit; setWitness6Optimized(&wi, &r); _, e := generateProofK6(&wi, c, &r); return e }
	}
	panic("k")
}

// TestSetupArtifacts compiles every transfer circuit and writes r1cs, pk and vk
// into last_build/, which is what the HTTP service loads at startup.
func TestSetupArtifacts(t *testing.T) {
	os.MkdirAll("../../../../../last_build/keys", 0755)
	os.MkdirAll("../../../../../last_build/circuits", 0755)
	for _, k := range []int{2, 3, 4, 5, 6} {
		st := time.Now()
		ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, emptyCircuit(k))
		if err != nil { t.Fatal(err) }
		compile := time.Since(st)
		f, err := os.Create(fmt.Sprintf("../../../../../last_build/circuits/Enygmak%d.r1cs", k))
		if err != nil { t.Fatal(err) }
		if _, err := ccs.WriteTo(f); err != nil { t.Fatal(err) }
		f.Close()
		st = time.Now()
		pk, vk, err := groth16.Setup(ccs)
		if err != nil { t.Fatal(err) }
		setup := time.Since(st)
		primitives.SaveProvingKey(ecc.BN254, pk, fmt.Sprintf("../../../../../last_build/keys/Enygmak%dPk.key", k))
		primitives.SaveVerifyingKey(ecc.BN254, vk, fmt.Sprintf("../../../../../last_build/keys/Enygmak%dVk.key", k))
		fmt.Printf("RESULT artifacts k=%d constraints=%d compile=%v setup=%v\n", k, ccs.GetNbConstraints(), compile.Round(time.Millisecond), setup.Round(time.Millisecond))
	}
}

// TestProveAcrossK times proof generation for every k at one and two cores,
// verifying each proof against its verifying key first.
func TestProveAcrossK(t *testing.T) {
	n := benchRuns()
	fmt.Printf("RESULT env NumCPU=%d arch=%s go=%s cpu=%q\n", runtime.NumCPU(), runtime.GOARCH, runtime.Version(), cpuModel())
	for _, k := range []int{2, 3, 4, 5, 6} {
		ccs := loadCCS(t, k)
		pk, err := primitives.LoadProvingKey(ecc.BN254, fmt.Sprintf("../../../../../last_build/keys/Enygmak%dPk.key", k))
		if err != nil { t.Fatal(err) }
		vk, err := primitives.LoadVerifyingKey(ecc.BN254, fmt.Sprintf("../../../../../last_build/keys/Enygmak%dVk.key", k))
		if err != nil { t.Fatal(err) }
		w, proveOnce := buildWitness(t, k, vectorPath(k))

		full, err := frontend.NewWitness(w, ecc.BN254.ScalarField())
		if err != nil { t.Fatal(err) }
		proof, err := groth16.Prove(ccs, pk, full)
		if err != nil { t.Fatalf("k=%d prove: %v", k, err) }
		pub, _ := full.Public()
		vst := time.Now()
		verr := groth16.Verify(proof, vk, pub)
		vdur := time.Since(vst)
		src := "repo"
		if k != 2 && k != 6 { src = "generated" }
		fmt.Printf("RESULT verify k=%d vector=%s ok=%v verify=%v\n", k, src, verr == nil, vdur.Round(time.Microsecond))
		if verr != nil { t.Fatalf("k=%d verify failed", k) }

		compiled := &CompiledCircuit{R1CS: ccs, PK: pk}
		for _, procs := range benchProcs() {
			if procs > runtime.NumCPU() { continue }
			old := runtime.GOMAXPROCS(procs)
			var d []time.Duration
			for i := 0; i < n; i++ {
				st := time.Now()
				if err := proveOnce(compiled); err != nil { t.Fatal(err) }
				d = append(d, time.Since(st))
			}
			runtime.GOMAXPROCS(old)
			sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
			fmt.Printf("RESULT prove k=%d cores=%d n=%d min=%v median=%v max=%v\n",
				k, procs, n, d[0].Round(time.Millisecond), d[n/2].Round(time.Millisecond), d[n-1].Round(time.Millisecond))
		}
	}
}

func loadCCS(t *testing.T, k int) constraint.ConstraintSystem {
	f, err := os.Open(fmt.Sprintf("../../../../../last_build/circuits/Enygmak%d.r1cs", k))
	if err != nil { t.Fatal(err) }
	defer f.Close()
	cs := groth16.NewCS(ecc.BN254)
	if _, err := cs.ReadFrom(f); err != nil { t.Fatal(err) }
	return cs
}

// TestRejectsBadVectors feeds the repository's deliberately corrupted vectors back in.
func TestRejectsBadVectors(t *testing.T) {
	for _, tc := range []struct{ k int; path string }{{2, vecDir() + "/k2_bad.json"}, {6, vecDir() + "/k6_bad.json"}} {
		ccs := loadCCS(t, tc.k)
		pk, _ := primitives.LoadProvingKey(ecc.BN254, fmt.Sprintf("../../../../../last_build/keys/Enygmak%dPk.key", tc.k))
		compiled := &CompiledCircuit{R1CS: ccs, PK: pk}
		b, _ := os.ReadFile(tc.path)
		var err error
		if tc.k == 2 {
			var r Enygmak2Request; json.Unmarshal(b, &r)
			var w Enygmak2Circuit; setWitness2Optimized(&w, &r)
			_, err = generateProofK2(&w, compiled, &r)
		} else {
			var r Enygmak6Request; json.Unmarshal(b, &r)
			var w Enygmak6Circuit; setWitness6Optimized(&w, &r)
			_, err = generateProofK6(&w, compiled, &r)
		}
		fmt.Printf("RESULT bad_vector k=%d refused=%v err=%.70v\n", tc.k, err != nil, err)
		if err == nil { t.Fatalf("k=%d bad vector was accepted", tc.k) }
	}
}
