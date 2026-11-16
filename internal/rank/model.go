package rank

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
)

// FeatureNames is the stage-2 input, in vector order (BASE.md "Stage 2"),
// stored with every model. rrf is added to the BASE.md list because it is
// the relevance signal when no reranker runs.
var FeatureNames = []string{
	"bm25", "cosine", "rerank", "rrf",
	"log_age_days", "log_days_since_use", "never_used",
	"type_event", "type_fact", "type_page", "type_page_item",
	"zone_external", "zone_internal", "zone_user",
	"utility_mean", "log_utility_count",
	"entity_match",
}

// features builds the input vector of c, given its utility row as of now
// (nil when the record has none).
func features(c Candidate, u *urow, now time.Time) []float64 {
	x := make([]float64, len(FeatureNames))
	x[0], x[1], x[2], x[3] = c.BM25, c.Cosine, c.Rerank, c.RRF
	x[4] = math.Log1p(math.Max(0, c.AgeSeconds) / 86400)
	x[6] = 1
	if u != nil && u.LastUsedAt != "" {
		if t, err := core.ParseTime(u.LastUsedAt); err == nil {
			x[5] = math.Log1p(math.Max(0, now.Sub(t).Seconds()) / 86400)
			x[6] = 0
		}
	}
	switch c.RecordType() {
	case core.RecEvent:
		x[7] = 1
	case core.RecFact:
		x[8] = 1
	case core.RecPage:
		x[9] = 1
	case core.RecPageItem:
		x[10] = 1
	}
	switch c.Zone {
	case core.ZoneUser:
		x[13] = 1
	case core.ZoneInternal:
		x[12] = 1
	default: // missing or unknown zone counts as external, the safe default
		x[11] = 1
	}
	x[14] = Neutral
	if u != nil {
		x[14] = u.Alpha / (u.Alpha + u.Beta)
		x[15] = math.Log1p(float64(u.NShown))
	}
	if c.HasEntityMatch() {
		x[16] = 1
	}
	return x
}

// Model kinds (ranker_models.kind).
const (
	KindLogReg = "logreg"
	KindMLP    = "mlp"
)

// Model is a trained stage-2 ranker. Inputs are standardised with the
// training mean and std stored in the model. It is serialised whole into
// ranker_models.weights.
type Model struct {
	ID       string    `json:"-"`
	Kind     string    `json:"kind"`
	Features []string  `json:"-"`
	Mean     []float64 `json:"mean"`
	Std      []float64 `json:"std"`
	// logreg: p = σ(w·x + b)
	W []float64 `json:"w,omitempty"`
	B float64   `json:"b"`
	// mlp: h = tanh(W1·x + B1), p = σ(W2·h + B2)
	W1 [][]float64 `json:"w1,omitempty"`
	B1 []float64   `json:"b1,omitempty"`
	W2 []float64   `json:"w2,omitempty"`
	B2 float64     `json:"b2"`
}

// Predict returns the probability that the candidate with raw features x is
// used in a successful task.
func (m *Model) Predict(x []float64) float64 {
	return m.forward(m.standardise(x), nil)
}

func (m *Model) standardise(x []float64) []float64 {
	z := make([]float64, len(m.Mean))
	for i := range z {
		if i < len(x) {
			z[i] = (x[i] - m.Mean[i]) / m.Std[i]
		}
	}
	return z
}

// forward computes p; if h is non-nil it receives the hidden activations.
func (m *Model) forward(z, h []float64) float64 {
	if m.Kind == KindMLP {
		if h == nil {
			h = make([]float64, len(m.W1))
		}
		s := m.B2
		for j, row := range m.W1 {
			a := m.B1[j]
			for i, w := range row {
				a += w * z[i]
			}
			h[j] = math.Tanh(a)
			s += m.W2[j] * h[j]
		}
		return sigmoid(s)
	}
	s := m.B
	for i, w := range m.W {
		s += w * z[i]
	}
	return sigmoid(s)
}

func sigmoid(s float64) float64 {
	s = math.Max(-35, math.Min(35, s))
	return 1 / (1 + math.Exp(-s))
}

// fitStandardisation computes per-feature mean and std (std 0 becomes 1).
func fitStandardisation(X [][]float64, d int) (mean, std []float64) {
	mean, std = make([]float64, d), make([]float64, d)
	n := float64(len(X))
	for _, x := range X {
		for i := 0; i < d; i++ {
			mean[i] += x[i]
		}
	}
	for i := range mean {
		mean[i] /= n
	}
	for _, x := range X {
		for i := 0; i < d; i++ {
			dv := x[i] - mean[i]
			std[i] += dv * dv
		}
	}
	for i := range std {
		std[i] = math.Sqrt(std[i] / n)
		if std[i] < 1e-12 {
			std[i] = 1
		}
	}
	return mean, std
}

// fit trains a model of the given kind on raw features X and labels y with
// full-batch gradient descent, a fixed number of iterations and L2 decay.
// Logistic regression starts from zero; the MLP starts from weights derived
// from a seeded SHA-256 stream (no math/rand), so training is deterministic.
func fit(kind string, X [][]float64, y []float64, cfg config.RankingConfig) (*Model, error) {
	if len(X) == 0 {
		return nil, core.Errf(core.CodeInvalidArgument, "no training examples")
	}
	d := len(X[0])
	m := &Model{Kind: kind, Features: append([]string(nil), FeatureNames...)}
	m.Mean, m.Std = fitStandardisation(X, d)
	Z := make([][]float64, len(X))
	for i, x := range X {
		Z[i] = m.standardise(x)
	}
	n := float64(len(Z))
	lr, l2 := cfg.LearningRate, cfg.L2
	switch kind {
	case KindLogReg:
		m.W = make([]float64, d)
		gw := make([]float64, d)
		for it := 0; it < cfg.TrainIterations; it++ {
			clear(gw)
			gb := 0.0
			for k, z := range Z {
				g := m.forward(z, nil) - y[k]
				for i := range gw {
					gw[i] += g * z[i]
				}
				gb += g
			}
			for i := range m.W {
				m.W[i] -= lr * (gw[i]/n + l2*m.W[i])
			}
			m.B -= lr * gb / n
		}
	case KindMLP:
		hu := cfg.HiddenUnits
		if hu <= 0 {
			return nil, core.Errf(core.CodeInvalidArgument, "ranking.hidden_units must be positive")
		}
		m.W1, m.B1, m.W2 = make([][]float64, hu), make([]float64, hu), make([]float64, hu)
		s1, s2 := 1/math.Sqrt(float64(d)), 1/math.Sqrt(float64(hu))
		for j := 0; j < hu; j++ {
			m.W1[j] = make([]float64, d)
			for i := 0; i < d; i++ {
				m.W1[j][i] = s1 * hashUniform(cfg.ModelSeed, "w1", j, i)
			}
			m.W2[j] = s2 * hashUniform(cfg.ModelSeed, "w2", j, 0)
		}
		gW1 := make([][]float64, hu)
		for j := range gW1 {
			gW1[j] = make([]float64, d)
		}
		gB1, gW2, h := make([]float64, hu), make([]float64, hu), make([]float64, hu)
		for it := 0; it < cfg.TrainIterations; it++ {
			for j := range gW1 {
				clear(gW1[j])
			}
			clear(gB1)
			clear(gW2)
			gB2 := 0.0
			for k, z := range Z {
				g := m.forward(z, h) - y[k]
				gB2 += g
				for j := 0; j < hu; j++ {
					gW2[j] += g * h[j]
					dh := g * m.W2[j] * (1 - h[j]*h[j])
					gB1[j] += dh
					for i := 0; i < d; i++ {
						gW1[j][i] += dh * z[i]
					}
				}
			}
			for j := 0; j < hu; j++ {
				for i := 0; i < d; i++ {
					m.W1[j][i] -= lr * (gW1[j][i]/n + l2*m.W1[j][i])
				}
				m.B1[j] -= lr * gB1[j] / n
				m.W2[j] -= lr * (gW2[j]/n + l2*m.W2[j])
			}
			m.B2 -= lr * gB2 / n
		}
	default:
		return nil, core.Errf(core.CodeInvalidArgument, "unknown ranker kind %q", kind)
	}
	return m, nil
}

// hashUniform maps (seed, layer, j, i) to a fixed value in [-1, 1).
func hashUniform(seed, layer string, j, i int) float64 {
	h := sha256.Sum256(fmt.Appendf(nil, "%s/%s/%d/%d", seed, layer, j, i))
	u := float64(binary.BigEndian.Uint64(h[:8])>>11) / (1 << 53)
	return 2*u - 1
}

// AUC is the area under the ROC curve of scores against binary labels, with
// tied scores sharing their average rank. It is 0.5 without both classes.
func AUC(scores, labels []float64) float64 {
	idx := make([]int, len(scores))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] < scores[idx[b]] })
	var pos, neg, rankSum float64
	for i := 0; i < len(idx); {
		j := i
		for j < len(idx) && scores[idx[j]] == scores[idx[i]] {
			j++
		}
		avg := float64(i+j+1) / 2 // ranks are 1-based: (i+1 .. j) averaged
		for k := i; k < j; k++ {
			if labels[idx[k]] > 0 {
				pos++
				rankSum += avg
			} else {
				neg++
			}
		}
		i = j
	}
	if pos == 0 || neg == 0 {
		return 0.5
	}
	return (rankSum - pos*(pos+1)/2) / (pos * neg)
}
