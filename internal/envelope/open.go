package envelope

import (
	"bufio"
	"bytes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Errors returned when opening a delivery. A wrong password and a damaged
// delivery are indistinguishable by design, so both return ErrUnlock.
var (
	ErrNotDelivery = errors.New("not a Tegami delivery")
	ErrUnlock      = errors.New("the password is incorrect, or the delivery is damaged")
)

// maxPageBytes bounds the HTML page read for an envelope: base64 of the
// largest payload plus the page around it.
const maxPageBytes = 128 << 20

var packageElement = []byte(`<script id="tegami-package" type="application/json">`)

// Delivery is a tegami/v1 delivery read from disk. Only its public metadata
// is available until it is unlocked.
type Delivery struct {
	Format   Format
	Codename string
	Size     int64

	id      string
	slot    unlockSlot
	commit  string
	name    encryptedPayload
	payload encryptedPayload // HTML deliveries
	path    string           // blob deliveries
	records int64            // offset of the first blob record
}

// ReadDelivery reads the public metadata of an HTML or .tegami delivery.
func ReadDelivery(path string) (*Delivery, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open delivery: %w", err)
	}
	defer func() { _ = file.Close() }()
	prefix := make([]byte, len(blobMagic))
	n, err := io.ReadFull(file, prefix)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read delivery: %w", err)
	}
	prefix = prefix[:n]
	switch {
	case bytes.Equal(prefix, blobMagic[:]):
		return readBlob(file, path)
	case bytes.HasPrefix(prefix, []byte("PK\x03\x04")):
		return nil, fmt.Errorf("%w: this is a ZIP file; extract it and open the HTML delivery inside", ErrNotDelivery)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("read delivery: %w", err)
	}
	page, err := io.ReadAll(io.LimitReader(file, maxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read delivery: %w", err)
	}
	if len(page) > maxPageBytes {
		return nil, ErrNotDelivery
	}
	return readPage(page, FormatHTML)
}

func readPage(page []byte, format Format) (*Delivery, error) {
	start := bytes.Index(page, packageElement)
	if start < 0 {
		return nil, ErrNotDelivery
	}
	content := page[start+len(packageElement):]
	end := bytes.Index(content, []byte("</script>"))
	if end < 0 {
		return nil, ErrNotDelivery
	}
	var data packageData
	if err := json.Unmarshal(content[:end], &data); err != nil {
		return nil, ErrNotDelivery
	}
	if data.Schema != schemaV1 || len(data.Unlocks) != 1 || data.Size < 1 || data.Size > MaximumInputBytes || !validSlot(data.Unlocks[0]) || !validMetadata(data.Unlocks[0], data.Commit, data.Name) || !validSealed(data.Payload, int(data.Size)+16) {
		return nil, ErrNotDelivery
	}
	codename, err := deliveryCodename(data.ID)
	if err != nil {
		return nil, err
	}
	return &Delivery{Format: format, Codename: codename, Size: data.Size, id: data.ID, slot: data.Unlocks[0], commit: data.Commit, name: data.Name, payload: data.Payload}, nil
}

func readBlob(file *os.File, path string) (*Delivery, error) {
	var length uint32
	if err := binary.Read(file, binary.BigEndian, &length); err != nil || length < 1 || length > maxBlobHeaderBytes {
		return nil, ErrNotDelivery
	}
	raw := make([]byte, length)
	if _, err := io.ReadFull(file, raw); err != nil {
		return nil, ErrNotDelivery
	}
	var header blobHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, ErrNotDelivery
	}
	if header.Schema != schemaV1 || header.Size < 1 || header.Size > MaximumBlobInputBytes || header.ChunkSize != BlobChunkBytes || !validSlot(header.Unlock) || !validMetadata(header.Unlock, header.Commit, header.Name) {
		return nil, ErrNotDelivery
	}
	codename, err := deliveryCodename(header.ID)
	if err != nil {
		return nil, err
	}
	return &Delivery{Format: FormatBlob, Codename: codename, Size: header.Size, id: header.ID, slot: header.Unlock, commit: header.Commit, name: header.Name, path: path, records: int64(len(blobMagic)) + 4 + int64(length)}, nil
}

func validSlot(slot unlockSlot) bool {
	return slot.Type == passwordSlot && slot.Memory == argon2Memory && slot.Passes == argon2Passes && slot.Lanes == argon2Lanes
}

func validEncoded(value string, length int) bool {
	if len(value) != base64.RawStdEncoding.EncodedLen(length) {
		return false
	}
	raw, err := base64.RawStdEncoding.DecodeString(value)
	return err == nil && len(raw) == length
}

func validSealed(value encryptedPayload, length int) bool {
	return validEncoded(value.Nonce, nonceBytes) && validEncoded(value.Ciphertext, length)
}

func validMetadata(slot unlockSlot, commit string, name encryptedPayload) bool {
	if !validEncoded(slot.Salt, saltBytes) || !validEncoded(slot.Nonce, nonceBytes) || !validEncoded(slot.WrappedKey, keyBytes+16) {
		return false
	}
	return validEncoded(commit, 32) && validSealed(name, maxNameBytes+16)
}

func deliveryCodename(envelopeID string) (string, error) {
	if len(envelopeID) != base64.RawURLEncoding.EncodedLen(idBytes) {
		return "", ErrNotDelivery
	}
	if id, err := base64.RawURLEncoding.DecodeString(envelopeID); err != nil || len(id) != idBytes {
		return "", ErrNotDelivery
	}
	return Codename(envelopeID)
}

// Unlock checks the password and reveals the delivery's filename.
func (d *Delivery) Unlock(password []byte) (*Unlocked, error) {
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(d.slot.Salt)
	if err != nil {
		return nil, ErrNotDelivery
	}
	key, err := slotKey(d.slot, password, salt)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	wrapping, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	contentKey, err := openSealedValue(wrapping, encryptedPayload{Nonce: d.slot.Nonce, Ciphertext: d.slot.WrappedKey}, unlockMetadata(d.id))
	if err != nil || len(contentKey) != keyBytes {
		clear(contentKey)
		return nil, ErrUnlock
	}
	// Only the one key the delivery commits to may open it.
	if !checkCommitment(d.commit, contentKey, d.id) {
		clear(contentKey)
		return nil, ErrUnlock
	}
	content, err := newGCM(contentKey)
	if err != nil {
		clear(contentKey)
		return nil, err
	}
	padded, err := openSealedValue(content, d.name, nameMetadata(d.id))
	if err != nil {
		clear(contentKey)
		return nil, ErrUnlock
	}
	name, ok := unpadName(padded)
	clear(padded)
	if !ok {
		clear(contentKey)
		return nil, ErrUnlock
	}
	return &Unlocked{Name: name, delivery: d, key: contentKey}, nil
}

// Unlocked is a delivery whose password has been checked.
type Unlocked struct {
	Name     string
	delivery *Delivery
	key      []byte
}

// Close erases the content key.
func (u *Unlocked) Close() {
	clear(u.key)
}

// WriteTo decrypts the delivery into w. Blob deliveries are decrypted and
// authenticated record by record, so on error w may hold partial output that
// must be discarded.
func (u *Unlocked) WriteTo(w io.Writer) (int64, error) {
	content, err := newGCM(u.key)
	if err != nil {
		return 0, err
	}
	d := u.delivery
	if d.Format != FormatBlob {
		plaintext, err := openSealedValue(content, d.payload, payloadMetadata(d.id, d.Size))
		if err != nil || int64(len(plaintext)) != d.Size {
			return 0, ErrUnlock
		}
		defer clear(plaintext)
		n, err := w.Write(plaintext)
		return int64(n), err
	}
	return u.writeBlob(content, w)
}

func (u *Unlocked) writeBlob(content cipher.AEAD, w io.Writer) (int64, error) {
	d := u.delivery
	file, err := os.Open(d.path)
	if err != nil {
		return 0, fmt.Errorf("open delivery: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Seek(d.records, io.SeekStart); err != nil {
		return 0, fmt.Errorf("read delivery: %w", err)
	}
	reader := bufio.NewReaderSize(file, 1<<20)
	ciphertext := make([]byte, BlobChunkBytes+16)
	nonce := make([]byte, nonceBytes)
	var written int64
	for index := uint64(0); written < d.Size; index++ {
		var length uint32
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return written, ErrUnlock
		}
		if length < 17 || length > BlobChunkBytes+16 {
			return written, ErrUnlock
		}
		if _, err := io.ReadFull(reader, nonce); err != nil {
			return written, ErrUnlock
		}
		if _, err := io.ReadFull(reader, ciphertext[:length]); err != nil {
			return written, ErrUnlock
		}
		expected := min(int64(BlobChunkBytes), d.Size-written)
		plaintext, err := content.Open(ciphertext[:0], nonce, ciphertext[:length], blobChunkMetadata(d.id, d.Size, index, int(expected)))
		if err != nil || int64(len(plaintext)) != expected {
			return written, ErrUnlock
		}
		n, err := w.Write(plaintext)
		clear(plaintext)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		return written, ErrUnlock
	}
	return written, nil
}

func openSealedValue(aead cipher.AEAD, sealed encryptedPayload, metadata []byte) ([]byte, error) {
	nonce, err := base64.RawStdEncoding.DecodeString(sealed.Nonce)
	if err != nil || len(nonce) != nonceBytes {
		return nil, ErrUnlock
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(sealed.Ciphertext)
	if err != nil {
		return nil, ErrUnlock
	}
	return aead.Open(nil, nonce, ciphertext, metadata)
}
