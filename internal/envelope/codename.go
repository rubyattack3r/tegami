package envelope

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// A codename such as "quiet-harbor" names a delivery without revealing its
// contents. It comes from the first two bytes of the random envelope ID, so it
// is public, needs no extra field, and the Go and browser code always agree.
// It tells deliveries apart; it is neither unique nor secret.

var codenameWords = func() (words struct {
	Adjectives []string `json:"adjectives"`
	Nouns      []string `json:"nouns"`
}) {
	if err := json.Unmarshal([]byte(asset("codenames.json")), &words); err != nil {
		panic(err)
	}
	if len(words.Adjectives) != 256 || len(words.Nouns) != 256 {
		panic("codenames.json must list 256 adjectives and 256 nouns")
	}
	return words
}()

// Codename returns the codename of the delivery with the given envelope ID.
func Codename(envelopeID string) (string, error) {
	id, err := base64.RawURLEncoding.DecodeString(envelopeID)
	if err != nil || len(id) < 2 {
		return "", errors.New("envelope ID is not valid")
	}
	return codenameWords.Adjectives[id[0]] + "-" + codenameWords.Nouns[id[1]], nil
}
