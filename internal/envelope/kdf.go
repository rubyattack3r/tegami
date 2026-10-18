package envelope

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for password slots: 64 MiB, three passes, one lane.
// They are fixed and written into every slot; readers reject any other values,
// so a delivery cannot make a recipient spend unbounded memory or time. The
// browser pages use the same values with Monocypher (see wasm/).
const (
	argon2Memory = 64 * 1024 // KiB
	argon2Passes = 3
	argon2Lanes  = 1
)

// maxNameBytes is the longest filename. Sealed names are padded to this length,
// so their ciphertext does not reveal how long the real name is.
const maxNameBytes = 180

// passwordKey derives the key that wraps a delivery's content key.
func passwordKey(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argon2Passes, argon2Memory, argon2Lanes, keyBytes)
}

func slotKey(slot unlockSlot, password, salt []byte) ([]byte, error) {
	if !validSlot(slot) {
		return nil, ErrNotDelivery
	}
	return passwordKey(password, salt), nil
}

// commitment binds a delivery to its one content key. AES-GCM alone does not:
// a crafted ciphertext can decrypt under two keys to two different files.
func commitment(contentKey []byte, envelopeID string) []byte {
	mac := hmac.New(sha256.New, contentKey)
	_, _ = mac.Write(commitMetadata(envelopeID))
	return mac.Sum(nil)
}

func commitMetadata(envelopeID string) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00commit", schemaV1, envelopeID))
}

func encodeCommitment(contentKey []byte, envelopeID string) string {
	return base64.RawStdEncoding.EncodeToString(commitment(contentKey, envelopeID))
}

// checkCommitment reports whether encoded commits to contentKey.
func checkCommitment(encoded string, contentKey []byte, envelopeID string) bool {
	expected, err := base64.RawStdEncoding.DecodeString(encoded)
	return err == nil && hmac.Equal(expected, commitment(contentKey, envelopeID))
}

// padName pads a portable filename with NUL bytes to maxNameBytes. Portable
// names never contain NUL, so the padding is unambiguous.
func padName(name string) []byte {
	padded := make([]byte, maxNameBytes)
	copy(padded, name)
	return padded
}

// unpadName reverses padName, rejecting anything padName cannot produce.
func unpadName(padded []byte) (string, bool) {
	if len(padded) != maxNameBytes {
		return "", false
	}
	name := bytes.TrimRight(padded, "\x00")
	if bytes.IndexByte(name, 0) >= 0 {
		return "", false
	}
	if _, err := portableFilename(string(name)); err != nil {
		return "", false
	}
	return string(name), true
}
