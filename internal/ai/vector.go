package ai

import (
	"encoding/binary"
	"math"

	"github.com/pkg/errors"
)

// VectorEncodingVersion identifies the little-endian float32 vector encoding
// stored in ai_index_chunk.
const VectorEncodingVersion int32 = 1

// NormalizeVector returns an L2-normalized copy of v. It rejects non-finite
// values and zero-norm vectors.
func NormalizeVector(v []float32) ([]float32, error) {
	// Accumulate in float64 so large dimensions and magnitudes do not overflow.
	var sumSquares float64
	for _, x := range v {
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.Errorf("vector contains a non-finite value")
		}
		sumSquares += f * f
	}
	norm := math.Sqrt(sumSquares)
	if norm == 0 {
		return nil, errors.Errorf("cannot normalize a zero-norm vector")
	}

	normalized := make([]float32, len(v))
	for i, x := range v {
		normalized[i] = float32(float64(x) / norm)
	}
	return normalized, nil
}

// EncodeVector encodes v as little-endian float32 bytes (len(v)*4 bytes).
func EncodeVector(v []float32) []byte {
	encoded := make([]byte, len(v)*4)
	for i, x := range v {
		binary.LittleEndian.PutUint32(encoded[i*4:], math.Float32bits(x))
	}
	return encoded
}

// DecodeVector decodes little-endian float32 bytes. It rejects byte lengths
// not equal to 4*dimensions and non-finite values.
func DecodeVector(b []byte, dimensions int) ([]float32, error) {
	if len(b) != dimensions*4 {
		return nil, errors.Errorf("vector byte length %d does not match %d dimensions", len(b), dimensions)
	}

	v := make([]float32, dimensions)
	for i := range v {
		x := math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return nil, errors.Errorf("vector contains a non-finite value")
		}
		v[i] = x
	}
	return v, nil
}
