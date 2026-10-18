package envelope

import (
	"crypto/rand"
	"fmt"
	"io"
)

// passwordAlphabet leaves out l, o, 0, and 1, which are easy to misread. The
// Studio generates passwords from the same alphabet.
const passwordAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// GeneratePassword returns 20 characters from a 32-character alphabet, 100
// random bits, in groups of five such as "k7mxq-3vbtn-wr9ha-e2pjd".
func GeneratePassword() (string, error) {
	random := make([]byte, 20)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	defer clear(random)
	password := make([]byte, 0, len(random)+len(random)/5-1)
	for index, value := range random {
		if index > 0 && index%5 == 0 {
			password = append(password, '-')
		}
		password = append(password, passwordAlphabet[value&31])
	}
	return string(password), nil
}
