package search

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"

	internalai "github.com/usememos/memos/internal/ai"
)

// endpointIdentity returns the sanitized identity of a provider endpoint for
// fingerprinting: scheme, host, and path only, with scheme and host
// lowercased. Credentials, query values, and fragments never take part, so
// the identity never carries secrets. It returns "" when the endpoint does
// not parse as an absolute URL.
func endpointIdentity(rawEndpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawEndpoint))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + parsed.Path
}

// computeIndexFingerprint derives the embedding generation fingerprint from
// everything two generations must share to produce interchangeable vectors:
// the provider type and sanitized endpoint identity, the model and
// configured dimensions, and the current corpus-projection, chunker,
// normalization, and vector-encoding versions. The endpoint identity is
// sanitized, so the fingerprint never derives from credentials or secret
// query values.
func computeIndexFingerprint(providerType internalai.ProviderType, identity, model string, dimensions int32) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(providerType),
		identity,
		model,
		strconv.Itoa(int(dimensions)),
		strconv.Itoa(int(ProjectionVersion)),
		strconv.Itoa(int(ChunkerVersion)),
		strconv.Itoa(int(NormalizationVersion)),
		strconv.Itoa(int(internalai.VectorEncodingVersion)),
	}, "\n")))
	return hex.EncodeToString(sum[:])
}
