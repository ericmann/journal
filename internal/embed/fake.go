package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
)

// Fake is a deterministic Embedder for tests: identical input text always yields
// the same vector (so a query equal to a stored body has distance ~0), and
// reranking scores by lexical token overlap. It performs no I/O.
type Fake struct {
	Dim int
	// Calls counts Embed invocations by total number of texts embedded; tests
	// use it to assert that a no-op re-index makes zero embed calls.
	EmbedTexts int
}

// NewFake returns a Fake embedder of the given dimension.
func NewFake(dim int) *Fake { return &Fake{Dim: dim} }

// Embed returns deterministic unit vectors seeded by the (instruction+text).
func (f *Fake) Embed(_ context.Context, texts []string, instruction string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = deterministicVector(instruction+"\x00"+t, f.Dim)
	}
	f.EmbedTexts += len(texts)
	return out, nil
}

// Rerank scores each doc by the fraction of query tokens it contains.
func (f *Fake) Rerank(_ context.Context, query string, docs []string) ([]float32, error) {
	qtokens := tokenize(query)
	scores := make([]float32, len(docs))
	for i, d := range docs {
		dset := map[string]bool{}
		for _, tok := range tokenize(d) {
			dset[tok] = true
		}
		if len(qtokens) == 0 {
			continue
		}
		var hit int
		for _, qt := range qtokens {
			if dset[qt] {
				hit++
			}
		}
		scores[i] = float32(hit) / float32(len(qtokens))
	}
	return scores, nil
}

func tokenize(s string) []string {
	return strings.Fields(strings.ToLower(s))
}

// deterministicVector hashes the seed into a fixed-length, L2-normalized vector.
func deterministicVector(seed string, dim int) []float32 {
	v := make([]float32, dim)
	var sum float64
	for i := 0; i < dim; i++ {
		h := sha256.Sum256([]byte(seed + "#" + itoa(i)))
		u := binary.BigEndian.Uint32(h[:4])
		// Map to [-1, 1).
		val := float64(u)/float64(math.MaxUint32)*2 - 1
		v[i] = float32(val)
		sum += val * val
	}
	if sum > 0 {
		norm := float32(math.Sqrt(sum))
		for i := range v {
			v[i] /= norm
		}
	}
	return v
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
