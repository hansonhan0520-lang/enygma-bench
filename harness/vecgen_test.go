package enygma

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/iden3/go-iden3-crypto/poseidon"
	common "github.com/raylsnetwork/rayls-sovereign-gnark-api/pkg/circuits/enygma/enygma-payments/common"
)

var L, _ = new(big.Int).SetString(common.JubJubPrimeSubGroupStr, 10)

func bi(s string) *big.Int { v, ok := new(big.Int).SetString(s, 10); if !ok { panic("bad int " + s) }; return v }

func pos(in ...*big.Int) *big.Int { h, err := poseidon.Hash(in); if err != nil { panic(err) }; return h }

func posMod(in ...*big.Int) *big.Int { return new(big.Int).Mod(pos(in...), L) }

// Vec is the JSON shape of a transfer request, k-agnostic.
type Vec struct {
	SenderID     string     `json:"sender_id"`
	SecretKey    string     `json:"secret_key"`
	PrevBalance  string     `json:"previous_sender_balance"`
	PrevRandom   string     `json:"previous_sender_random_value"`
	BlockNumber  string     `json:"block_number"`
	SenderTxVal  string     `json:"sender_tx_value"`
	Nullifier    string     `json:"nullifier"`
	Anonymity    []string   `json:"anonymity_set"`
	SharedSec    []string   `json:"shared_secrets"`
	HashedSec    []string   `json:"hashed_shared_secrets"`
	PublicKeys   []string   `json:"public_keys"`
	MessageTags  []string   `json:"message_tags"`
	PrevCommits  [][]string `json:"previous_commits"`
	TxCommits    [][]string `json:"tx_commits"`
	TxValues     []string   `json:"tx_values"`
	TxRandoms    []string   `json:"tx_random_values"`
}

// derive fills every field the circuit constrains, from the seeds plus the
// free per-participant inputs (other parties' shared secrets, public keys and
// previous commitments). This mirrors common-checks.go exactly.
func derive(v *Vec, senderIdx, receiverIdx int) {
	k := len(v.Anonymity)
	blk := bi(v.BlockNumber)
	sk := bi(v.SecretKey)
	prevR := bi(v.PrevRandom)
	prevV := bi(v.PrevBalance)
	amount := bi(v.SenderTxVal)

	// CheckSecretKnowledge: sender's shared secret = Poseidon(prevR, sk) mod l
	v.SharedSec[senderIdx] = posMod(prevR, sk).String()
	// CheckPublicKeyKnowledge: Poseidon(sk, sk) mod l
	v.PublicKeys[senderIdx] = posMod(sk, sk).String()
	// CheckPreviousCommitmentKnowledge
	pc, err := computePedersenCommitment(prevV.String(), prevR.String())
	if err != nil { panic(err) }
	v.PrevCommits[senderIdx] = []string{pc.X, pc.Y}

	// CheckHashArrayOfSecrets and CheckMessageTags
	hashTag := pos(big.NewInt(12))
	hashRandom := pos(big.NewInt(21))
	v.HashedSec = make([]string, k)
	v.MessageTags = make([]string, k)
	hmod := make([]*big.Int, k)
	for i := 0; i < k; i++ {
		s := bi(v.SharedSec[i])
		v.HashedSec[i] = posMod(s, s).String()
		v.MessageTags[i] = posMod(hashTag, s, blk).String()
		hmod[i] = posMod(hashRandom, s, blk)
	}
	// CheckNullifierKnowledge: not reduced
	v.Nullifier = pos(bi(v.HashedSec[senderIdx]), blk).String()

	// CheckRandomFactors: receivers get l - h, sender gets sum(receiver h) mod l
	sum := new(big.Int)
	v.TxRandoms = make([]string, k)
	for i := 0; i < k; i++ {
		if i != senderIdx { sum.Add(sum, hmod[i]) }
	}
	sum.Mod(sum, L)
	for i := 0; i < k; i++ {
		if i == senderIdx {
			v.TxRandoms[i] = sum.String()
		} else {
			v.TxRandoms[i] = new(big.Int).Sub(L, hmod[i]).String()
		}
	}

	// tx values: sender holds -amount mod l, receiver holds amount
	v.TxValues = make([]string, k)
	for i := range v.TxValues { v.TxValues[i] = "0" }
	v.TxValues[senderIdx] = new(big.Int).Mod(new(big.Int).Neg(amount), L).String()
	if receiverIdx >= 0 && receiverIdx < k { v.TxValues[receiverIdx] = amount.String() }

	// tx commits
	v.TxCommits = make([][]string, k)
	for i := 0; i < k; i++ {
		c, err := computePedersenCommitment(v.TxValues[i], v.TxRandoms[i])
		if err != nil { panic(err) }
		v.TxCommits[i] = []string{c.X, c.Y}
	}
}

func load(path string) *Vec {
	b, err := os.ReadFile(path)
	if err != nil { panic(err) }
	var v Vec
	if err := json.Unmarshal(b, &v); err != nil { panic(err) }
	return v_ptr(v)
}
func v_ptr(v Vec) *Vec { return &v }

func cmpField(t *testing.T, name string, got, want []string) bool {
	ok := len(got) == len(want)
	if ok { for i := range got { if got[i] != want[i] { ok = false; break } } }
	if !ok { t.Errorf("MISMATCH %s\n got=%v\nwant=%v", name, got, want) }
	return ok
}

// TestGeneratorReproducesPublishedVectors derives every constrained field from
// the seeds of each published vector and checks it against the published value.
func TestGeneratorReproducesPublishedVectors(t *testing.T) {
	for _, tc := range []struct{ path string; recv int }{
		{vecDir()+"/k6.json", 1}, {vecDir()+"/k2.json", -1},
	} {
		want := load(tc.path)
		got := load(tc.path)
		// wipe every derived field so nothing can leak through
		k := len(got.Anonymity)
		got.SharedSec[0] = "0"; got.PublicKeys[0] = "0"; got.PrevCommits[0] = []string{"0", "1"}
		got.HashedSec = make([]string, k); got.MessageTags = make([]string, k)
		got.TxValues = make([]string, k); got.TxRandoms = make([]string, k)
		got.TxCommits = make([][]string, k); got.Nullifier = "0"
		derive(got, 0, tc.recv)

		all := true
		all = cmpField(t, tc.path+" shared_secrets", got.SharedSec, want.SharedSec) && all
		all = cmpField(t, tc.path+" public_keys", got.PublicKeys, want.PublicKeys) && all
		all = cmpField(t, tc.path+" hashed_shared_secrets", got.HashedSec, want.HashedSec) && all
		all = cmpField(t, tc.path+" message_tags", got.MessageTags, want.MessageTags) && all
		all = cmpField(t, tc.path+" tx_values", got.TxValues, want.TxValues) && all
		all = cmpField(t, tc.path+" tx_random_values", got.TxRandoms, want.TxRandoms) && all
		all = cmpField(t, tc.path+" nullifier", []string{got.Nullifier}, []string{want.Nullifier}) && all
		for i := 0; i < k; i++ {
			all = cmpField(t, fmt.Sprintf("%s tx_commits[%d]", tc.path, i), got.TxCommits[i], want.TxCommits[i]) && all
			all = cmpField(t, fmt.Sprintf("%s previous_commits[%d]", tc.path, i), got.PrevCommits[i], want.PrevCommits[i]) && all
		}
		fmt.Printf("RESULT regenerate %s k=%d all_fields_match=%v\n", tc.path, k, all)
	}
}

// TestGenerateMidVectors builds k=3,4,5 from the published k=6 vector's seeds
// and its first-k free inputs, using the same derivation.
func TestGenerateMidVectors(t *testing.T) {
	src := load(vecDir()+"/k6.json")
	for _, k := range []int{3, 4, 5} {
		v := &Vec{
			SenderID: src.SenderID, SecretKey: src.SecretKey,
			PrevBalance: src.PrevBalance, PrevRandom: src.PrevRandom,
			BlockNumber: src.BlockNumber, SenderTxVal: src.SenderTxVal,
			Anonymity: append([]string{}, src.Anonymity[:k]...),
			SharedSec: append([]string{}, src.SharedSec[:k]...),
			PublicKeys: append([]string{}, src.PublicKeys[:k]...),
		}
		v.PrevCommits = make([][]string, k)
		for i := 0; i < k; i++ { v.PrevCommits[i] = append([]string{}, src.PrevCommits[i]...) }
		derive(v, 0, 1)
		b, _ := json.MarshalIndent(v, "", " ")
		os.WriteFile(fmt.Sprintf("%s/gen_k%d.json", vecDir(), k), b, 0644)
		fmt.Printf("RESULT generated k=%d participants=%d sender_tx_value=%s\n", k, len(v.Anonymity), v.SenderTxVal)
	}
}
