package ai_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/ai"
)

func TestVectorRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		v    []float32
	}{
		{name: "empty", v: []float32{}},
		{name: "single", v: []float32{1.5}},
		{name: "mixed signs", v: []float32{-1.25, 0, 3.5, -0.0, 42}},
		{name: "small magnitudes", v: []float32{1e-20, -1e-20}},
		{name: "large magnitudes", v: []float32{1e20, -1e20}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded := ai.EncodeVector(tc.v)
			require.Len(t, encoded, len(tc.v)*4)

			decoded, err := ai.DecodeVector(encoded, len(tc.v))
			require.NoError(t, err)
			require.Equal(t, tc.v, decoded)
		})
	}
}

func TestNormalizeVector(t *testing.T) {
	tests := []struct {
		name string
		v    []float32
	}{
		{name: "unit axis", v: []float32{3, 0, 0}},
		{name: "mixed", v: []float32{1, -2, 3, -4}},
		{name: "large magnitudes do not overflow", v: []float32{1e30, 1e30}},
		{name: "tiny magnitudes", v: []float32{1e-20, -1e-20}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := make([]float32, len(tc.v))
			copy(original, tc.v)

			normalized, err := ai.NormalizeVector(tc.v)
			require.NoError(t, err)
			require.Len(t, normalized, len(tc.v))

			var norm float64
			for _, x := range normalized {
				norm += float64(x) * float64(x)
			}
			require.InDelta(t, 1.0, math.Sqrt(norm), 1e-6)

			// The input must not be mutated.
			require.Equal(t, original, tc.v)
		})
	}

	t.Run("direction is preserved", func(t *testing.T) {
		normalized, err := ai.NormalizeVector([]float32{3, 4})
		require.NoError(t, err)
		require.InDelta(t, 0.6, normalized[0], 1e-6)
		require.InDelta(t, 0.8, normalized[1], 1e-6)
	})
}

func TestNormalizeVectorRejects(t *testing.T) {
	tests := []struct {
		name string
		v    []float32
	}{
		{name: "NaN", v: []float32{1, float32(math.NaN())}},
		{name: "positive infinity", v: []float32{float32(math.Inf(1)), 1}},
		{name: "negative infinity", v: []float32{float32(math.Inf(-1)), 1}},
		{name: "zero norm", v: []float32{0, 0, 0}},
		{name: "empty", v: []float32{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ai.NormalizeVector(tc.v)
			require.Error(t, err)
		})
	}
}

func TestDecodeVectorRejects(t *testing.T) {
	tests := []struct {
		name       string
		b          []byte
		dimensions int
	}{
		{name: "byte length mismatch", b: ai.EncodeVector([]float32{1, 2, 3}), dimensions: 2},
		{name: "odd byte length", b: []byte{0, 0, 0, 0, 0}, dimensions: 1},
		{name: "empty with nonzero dimensions", b: []byte{}, dimensions: 1},
		{name: "NaN value", b: []byte{0, 0, 0xc0, 0x7f}, dimensions: 1},
		{name: "infinite value", b: []byte{0, 0, 0x80, 0x7f}, dimensions: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ai.DecodeVector(tc.b, tc.dimensions)
			require.Error(t, err)
		})
	}
}

func TestVectorEncodingVersion(t *testing.T) {
	require.Equal(t, int32(1), ai.VectorEncodingVersion)
}
