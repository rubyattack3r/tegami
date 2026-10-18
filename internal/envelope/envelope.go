package envelope

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	MaximumInputBytes    = 50 << 20
	MinimumPasswordBytes = 12
	MaximumPasswordBytes = 4096
	keyBytes             = 32
	saltBytes            = 16
	nonceBytes           = 12
	idBytes              = 16
)

// schemaV1 is the only schema, written and read by every format.
const schemaV1 = "tegami/v1"

// passwordSlot is the only unlock slot type. It is part of the slot's
// additional authenticated data.
const passwordSlot = "password"

type Format string

const (
	FormatHTML Format = "html"
	FormatBlob Format = "blob"
)

type encryptedPayload struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type unlockSlot struct {
	Type       string `json:"type"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	WrappedKey string `json:"wrapped_key"`
	Memory     int    `json:"memory"`
	Passes     int    `json:"passes"`
	Lanes      int    `json:"lanes"`
}

// packageData is the tegami/v1 envelope. The filename is sealed in Name with
// the content key; only the codename and size are visible.
type packageData struct {
	Schema   string           `json:"schema"`
	ID       string           `json:"id"`
	Size     int64            `json:"size"`
	Name     encryptedPayload `json:"name"`
	Payload  encryptedPayload `json:"payload"`
	Unlocks  []unlockSlot     `json:"unlocks"`
	Commit   string           `json:"commit"`
	codename string
}

type pageData struct {
	Codename     string
	Size         string
	Package      template.JS
	Introduction string
	Action       string
}

// PackHTML writes a single-page HTML delivery and returns its codename.
func PackHTML(inputPath, outputPath string, password []byte) (string, error) {
	data, err := packageInput(inputPath, password)
	if err != nil {
		return "", err
	}
	page, err := renderPage(data)
	if err != nil {
		return "", err
	}
	return data.codename, writeOutput(outputPath, page)
}

// PackBlob writes a streaming .tegami delivery and returns its codename.
func PackBlob(inputPath, outputPath string, password []byte) (string, error) {
	return packStreamingBlob(inputPath, outputPath, password)
}

func packageInput(inputPath string, password []byte) (packageData, error) {
	if err := validatePassword(password); err != nil {
		return packageData{}, err
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return packageData{}, fmt.Errorf("open input: %w", err)
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil {
		return packageData{}, fmt.Errorf("inspect input: %w", err)
	}
	switch {
	case !info.Mode().IsRegular():
		return packageData{}, errors.New("input must be a regular file; archive folders first, for example with tar or zip")
	case info.Size() < 1:
		return packageData{}, errors.New("input is empty; there is nothing to encrypt")
	case info.Size() > MaximumInputBytes:
		return packageData{}, fmt.Errorf("input is larger than %d MiB, the limit for html; use --format blob for files up to %d GiB", MaximumInputBytes>>20, MaximumBlobInputBytes>>30)
	}
	plaintext, err := io.ReadAll(io.LimitReader(input, MaximumInputBytes+1))
	defer clear(plaintext)
	if err != nil {
		return packageData{}, fmt.Errorf("read input: %w", err)
	}
	filename, err := portableFilename(filepath.Base(inputPath))
	if err != nil {
		return packageData{}, err
	}
	data, err := seal(filename, plaintext, password)
	if err != nil {
		return packageData{}, err
	}
	return data, nil
}

func seal(filename string, plaintext []byte, password []byte) (packageData, error) {
	id := make([]byte, idBytes)
	payloadNonce := make([]byte, nonceBytes)
	contentKey := make([]byte, keyBytes)
	if _, err := io.ReadFull(rand.Reader, id); err != nil {
		return packageData{}, fmt.Errorf("generate envelope ID: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, payloadNonce); err != nil {
		return packageData{}, fmt.Errorf("generate payload nonce: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, contentKey); err != nil {
		return packageData{}, fmt.Errorf("generate content key: %w", err)
	}
	defer clear(contentKey)

	envelopeID := base64.RawURLEncoding.EncodeToString(id)
	payloadCipher, err := newGCM(contentKey)
	if err != nil {
		return packageData{}, err
	}
	payloadCiphertext := payloadCipher.Seal(nil, payloadNonce, plaintext, payloadMetadata(envelopeID, int64(len(plaintext))))
	name, err := sealName(payloadCipher, envelopeID, filename)
	if err != nil {
		return packageData{}, err
	}
	codename, err := Codename(envelopeID)
	if err != nil {
		return packageData{}, err
	}
	slot, err := wrapContentKey(envelopeID, contentKey, password)
	if err != nil {
		return packageData{}, err
	}
	return packageData{
		Schema: schemaV1,
		ID:     envelopeID,
		Size:   int64(len(plaintext)),
		Name:   name,
		Payload: encryptedPayload{
			Nonce:      base64.RawStdEncoding.EncodeToString(payloadNonce),
			Ciphertext: base64.RawStdEncoding.EncodeToString(payloadCiphertext),
		},
		Unlocks:  []unlockSlot{slot},
		Commit:   encodeCommitment(contentKey, envelopeID),
		codename: codename,
	}, nil
}

func wrapContentKey(envelopeID string, contentKey []byte, password []byte) (unlockSlot, error) {
	salt := make([]byte, saltBytes)
	nonce := make([]byte, nonceBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return unlockSlot{}, fmt.Errorf("generate unlock salt: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return unlockSlot{}, fmt.Errorf("generate unlock nonce: %w", err)
	}
	key := passwordKey(password, salt)
	defer clear(key)
	aead, err := newGCM(key)
	if err != nil {
		return unlockSlot{}, err
	}
	wrappedKey := aead.Seal(nil, nonce, contentKey, unlockMetadata(envelopeID))
	return unlockSlot{
		Type:       passwordSlot,
		Salt:       base64.RawStdEncoding.EncodeToString(salt),
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		WrappedKey: base64.RawStdEncoding.EncodeToString(wrappedKey),
		Memory:     argon2Memory,
		Passes:     argon2Passes,
		Lanes:      argon2Lanes,
	}, nil
}

func renderPage(data packageData) ([]byte, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode package: %w", err)
	}
	view := pageData{
		Codename: data.codename,
		Size:     humanSize(data.Size),
		Package:  template.JS(encoded),
	}
	// The page is rendered in English; its script switches to the reader's
	// language from the embedded catalogs.
	view.Action = text("en", "open.buttonDownload")
	view.Introduction = text("en", "envelope.intro")
	var page bytes.Buffer
	if err := pageTemplate.Execute(&page, view); err != nil {
		return nil, fmt.Errorf("render output: %w", err)
	}
	return []byte(assemblePage(strings.Replace(page.String(), i18nMarker, pageCatalog, 1), pageScript, pageStyle)), nil
}

func writeOutput(outputPath string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".tegami-*")
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	temporaryPath := temporary.Name()
	complete := false
	defer func() {
		_ = temporary.Close()
		if !complete {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return fmt.Errorf("protect output: %w", err)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish output: %w", err)
	}
	complete = true
	return nil
}

func validatePassword(password []byte) error {
	if !utf8.Valid(password) || len(password) < MinimumPasswordBytes || len(password) > MaximumPasswordBytes || bytes.IndexByte(password, 0) >= 0 || strings.ContainsAny(string(password), "\r\n") {
		return fmt.Errorf("password must be one UTF-8 line from %d to %d bytes", MinimumPasswordBytes, MaximumPasswordBytes)
	}
	return nil
}

func portableFilename(name string) (string, error) {
	if name == "" || name == "." || name == ".." || len(name) > maxNameBytes || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\\x00\r\n") {
		return "", fmt.Errorf("input filename must be 1 to %d bytes of UTF-8 without /, \\, or line breaks; rename the file", maxNameBytes)
	}
	return name, nil
}

func payloadMetadata(envelopeID string, size int64) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%d", schemaV1, envelopeID, size))
}

func nameMetadata(envelopeID string) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00name", schemaV1, envelopeID))
}

// sealName encrypts a padded filename with the content key, so it is hidden,
// length included, until the delivery is unlocked.
func sealName(aead cipher.AEAD, envelopeID, filename string) (encryptedPayload, error) {
	nonce := make([]byte, nonceBytes)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return encryptedPayload{}, fmt.Errorf("generate name nonce: %w", err)
	}
	return encryptedPayload{
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(aead.Seal(nil, nonce, padName(filename), nameMetadata(envelopeID))),
	}, nil
}

func unlockMetadata(envelopeID string) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%s", schemaV1, envelopeID, passwordSlot))
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize encryption: %w", err)
	}
	return aead, nil
}
