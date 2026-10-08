package enygma

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	primitives "github.com/raylsnetwork/rayls-sovereign-gnark-api/primitives"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
)

func TestSizes(t *testing.T) {
	for _, k := range []int{2, 3, 4, 5, 6} {
		ccs := loadCCS(t, k)
		pk, _ := primitives.LoadProvingKey(ecc.BN254, fmt.Sprintf("../../../../../last_build/keys/Enygmak%dPk.key", k))
		w, _ := buildWitness(t, k, vectorPath(k))
		full, _ := frontend.NewWitness(w, ecc.BN254.ScalarField())
		p, err := groth16.Prove(ccs, pk, full)
		if err != nil { t.Fatal(err) }
		var buf bytes.Buffer
		n, _ := p.WriteTo(&buf)
		pub, _ := full.Public()
		pb, _ := pub.MarshalBinary()
		fi, _ := os.Stat(fmt.Sprintf("../../../../../last_build/keys/Enygmak%dPk.key", k))
		dom := pk.(*groth16_bn254.ProvingKey).Domain.Cardinality
		fmt.Printf("RESULT size k=%d constraints=%d domain=%d proof=%dB public=%dB pk=%dB\n",
			k, ccs.GetNbConstraints(), dom, n, len(pb), fi.Size())
	}
}
