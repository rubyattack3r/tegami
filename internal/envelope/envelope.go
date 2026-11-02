package envelope

import (
	"bufio"
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
	// MaximumInputBytes is the largest file an HTML delivery holds.
	MaximumInputBytes    = 500 << 20
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

type pageData struct {
	Codename     string
	Size         string
	Package      template.JS
	Introduction string
	Action       string
}

// PackHTML writes a self-contained HTML delivery and returns its codename. The
// page carries the records of a .tegami file, each in its own inert element
// after the header, so browsers can open it one record at a time.
func PackHTML(inputPath, outputPath string, password []byte) (string, error) {
	input, err := openChunkedInput(inputPath, password, MaximumInputBytes, fmt.Sprintf("input is larger than %d MiB, the limit for html; use --format blob for files up to %d GiB", MaximumInputBytes>>20, MaximumBlobInputBytes>>30))
	if err != nil {
		return "", err
	}
	defer input.close()
	header, err := json.Marshal(input.header)
	if err != nil {
		return "", fmt.Errorf("encode package: %w", err)
	}
	page, err := renderPage(input.codename, input.header.Size, header)
	if err != nil {
		return "", err
	}
	_, records, ok := findPackage(page)
	if !ok {
		return "", errors.New("render output: package element not found")
	}
	return input.codename, writeAtomically(outputPath, func(w io.Writer) error {
		if _, err := w.Write(page[:records]); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		err := input.encrypt(func(index uint64, nonce, ciphertext []byte) error {
			if _, err := io.WriteString(w, recordStart(index)); err != nil {
				return err
			}
			encoder := base64.NewEncoder(base64.RawStdEncoding, w)
			if _, err := encoder.Write(nonce); err != nil {
				return err
			}
			if _, err := encoder.Write(ciphertext); err != nil {
				return err
			}
			if err := encoder.Close(); err != nil {
				return err
			}
			_, err := io.WriteString(w, recordEnd)
			return err
		})
		if err != nil {
			return err
		}
		if _, err := w.Write(page[records:]); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	})
}

// recordStart opens the element holding record index of an HTML delivery.
// Records follow the package element directly and in order; each holds the
// record's nonce and ciphertext in Base64 without padding.
func recordStart(index uint64) string {
	return fmt.Sprintf("\n<script id=\"tegami-r%d\" type=\"application/octet-stream\">", index)
}

const recordEnd = "</script>"

// PackBlob writes a streaming .tegami delivery and returns its codename.
func PackBlob(inputPath, outputPath string, password []byte) (string, error) {
	return packStreamingBlob(inputPath, outputPath, password)
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

// renderPage renders the CLI delivery page around the package JSON.
func renderPage(codename string, size int64, packageJSON []byte) ([]byte, error) {
	view := pageData{
		Codename: codename,
		Size:     humanSize(size),
		Package:  template.JS(packageJSON),
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
	return writeAtomically(outputPath, func(w io.Writer) error {
		if _, err := w.Write(content); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	})
}

// writeAtomically writes an output through a private temporary file and
// renames it into place only once write succeeds.
func writeAtomically(outputPath string, write func(io.Writer) error) error {
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
	buffered := bufio.NewWriterSize(temporary, 1<<20)
	if err := write(buffered); err != nil {
		return err
	}
	if err := buffered.Flush(); err != nil {
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
