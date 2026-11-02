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

// pageHeadBytes is how much of an HTML file readers search for the package
// element. Pages put it before their records, well within the first MiB.
const pageHeadBytes = 1 << 20

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
	path    string
	records int64 // offset of the first record
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
	// Treat the page as bytes and never parse its markup.
	page, err := readHead(file)
	if err != nil {
		return nil, err
	}
	return readPage(page, path)
}

// readHead reads up to pageHeadBytes from the start of file.
func readHead(file *os.File) ([]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("read delivery: %w", err)
	}
	page, err := io.ReadAll(io.LimitReader(file, pageHeadBytes))
	if err != nil {
		return nil, fmt.Errorf("read delivery: %w", err)
	}
	return page, nil
}

// readPage reads the delivery whose package element is in page.
func readPage(page []byte, path string) (*Delivery, error) {
	raw, records, ok := findPackage(page)
	if !ok {
		return nil, ErrNotDelivery
	}
	return readPackage(raw, path, int64(records))
}

// findPackage returns the package JSON and the offset just after its element.
func findPackage(page []byte) ([]byte, int, bool) {
	start := bytes.Index(page, packageElement)
	if start < 0 {
		return nil, 0, false
	}
	start += len(packageElement)
	end := bytes.Index(page[start:], []byte(recordEnd))
	if end < 0 {
		return nil, 0, false
	}
	return page[start : start+end], start + end + len(recordEnd), true
}

// readPackage reads the header in an HTML delivery's package element. HTML
// files made by Tegami 0.1.0 hold an older package that only their own page
// opens.
func readPackage(raw []byte, path string, records int64) (*Delivery, error) {
	var header struct {
		blobHeader
		Unlocks json.RawMessage `json:"unlocks"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, ErrNotDelivery
	}
	if header.Unlocks != nil {
		return nil, fmt.Errorf("%w: it was made by Tegami 0.1.0; open the file in a browser instead", ErrNotDelivery)
	}
	delivery, err := deliveryFromHeader(header.blobHeader, MaximumInputBytes)
	if err != nil {
		return nil, err
	}
	delivery.Format, delivery.path, delivery.records = FormatHTML, path, records
	return delivery, nil
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
	delivery, err := deliveryFromHeader(header, MaximumBlobInputBytes)
	if err != nil {
		return nil, err
	}
	delivery.Format, delivery.path, delivery.records = FormatBlob, path, int64(len(blobMagic))+4+int64(length)
	return delivery, nil
}

func deliveryFromHeader(header blobHeader, limit int64) (*Delivery, error) {
	if header.Schema != schemaV1 || header.Size < 1 || header.Size > limit || header.ChunkSize != BlobChunkBytes || !validSlot(header.Unlock) || !validMetadata(header.Unlock, header.Commit, header.Name) {
		return nil, ErrNotDelivery
	}
	codename, err := deliveryCodename(header.ID)
	if err != nil {
		return nil, err
	}
	return &Delivery{Codename: codename, Size: header.Size, id: header.ID, slot: header.Unlock, commit: header.Commit, name: header.Name}, nil
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

// WriteTo decrypts the delivery into w. Deliveries with records are decrypted
// and authenticated record by record, so on error w may hold partial output
// that must be discarded.
func (u *Unlocked) WriteTo(w io.Writer) (int64, error) {
	content, err := newGCM(u.key)
	if err != nil {
		return 0, err
	}
	d := u.delivery
	if d.Format == FormatBlob {
		return u.writeBlob(content, w)
	}
	return u.writePageRecords(content, w)
}

// writePageRecords decrypts the records of an HTML delivery. Each must follow
// the previous one directly, with exactly the length its chunk requires, and
// no further record may follow the last.
func (u *Unlocked) writePageRecords(content cipher.AEAD, w io.Writer) (int64, error) {
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
	encoded := make([]byte, base64.RawStdEncoding.EncodedLen(nonceBytes+BlobChunkBytes+16))
	record := make([]byte, nonceBytes+BlobChunkBytes+16)
	var written int64
	index := uint64(0)
	for ; written < d.Size; index++ {
		expected := min(int64(BlobChunkBytes), d.Size-written)
		length := nonceBytes + int(expected) + 16
		if !readExactly(reader, recordStart(index)) {
			return written, ErrUnlock
		}
		if _, err := io.ReadFull(reader, encoded[:base64.RawStdEncoding.EncodedLen(length)]); err != nil {
			return written, ErrUnlock
		}
		if n, err := base64.RawStdEncoding.Decode(record, encoded[:base64.RawStdEncoding.EncodedLen(length)]); err != nil || n != length {
			return written, ErrUnlock
		}
		if !readExactly(reader, recordEnd) {
			return written, ErrUnlock
		}
		ciphertext := record[nonceBytes:length]
		plaintext, err := content.Open(ciphertext[:0], record[:nonceBytes], ciphertext, blobChunkMetadata(d.id, d.Size, index, int(expected)))
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
	if next, _ := reader.Peek(len(recordStart(index))); string(next) == recordStart(index) {
		return written, ErrUnlock
	}
	return written, nil
}

func readExactly(reader io.Reader, want string) bool {
	got := make([]byte, len(want))
	_, err := io.ReadFull(reader, got)
	return err == nil && string(got) == want
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
