package webauthn

import "github.com/fxamacker/cbor/v2"

// COSEAlg returns the COSE algorithm identifier embedded in a serialized
// COSE_Key (RFC 8152 §7.1, key index 3). Examples: ES256 = -7, RS256 = -257,
// EdDSA = -8. Returns 0 when the blob is malformed or the alg parameter is
// missing.
//
// Why this exists: go-webauthn's `protocol.Attestation.PublicKeyAlgorithm`
// field is declared but never assigned by the library, so reading it always
// returns the zero value. The COSE_Key bytes on `webauthn.Credential.PublicKey`
// carry the real value; this helper extracts it.
func COSEAlg(coseKey []byte) int32 {
	var m map[int]cbor.RawMessage
	if err := cbor.Unmarshal(coseKey, &m); err != nil {
		return 0
	}
	raw, ok := m[3]
	if !ok {
		return 0
	}
	var alg int64
	if err := cbor.Unmarshal(raw, &alg); err != nil {
		return 0
	}
	return int32(alg)
}
