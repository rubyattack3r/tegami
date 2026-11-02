package envelope

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	MaximumBlobInputBytes int64 = 3 << 30
	BlobChunkBytes              = 4 << 20
	maxBlobHeaderBytes          = 64 << 10
)

var blobMagic = [8]byte{'T', 'E', 'G', 'A', 'M', 'I', '1', 0}

// blobHeader is the tegami/v1 header of a chunked delivery: a .tegami file or
// an HTML page with records. As in envelopes, the filename is sealed in Name
// with the content key.
type blobHeader struct {
	Schema    string           `json:"schema"`
	ID        string           `json:"id"`
	Size      int64            `json:"size"`
	Name      encryptedPayload `json:"name"`
	ChunkSize int              `json:"chunk_size"`
	Unlock    unlockSlot       `json:"unlock"`
	Commit    string           `json:"commit"`
}

// chunkedInput is a file ready to be encrypted in 4 MiB records.
type chunkedInput struct {
	file     *os.File
	header   blobHeader
	aead     cipher.AEAD
	codename string
}

// openChunkedInput checks the input and password, generates the content key,
// and seals the filename. The caller closes the input.
func openChunkedInput(inputPath string, password []byte, limit int64, tooLarge string) (*chunkedInput, error) {
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}
	prepared, err := prepareChunkedInput(input, inputPath, password, limit, tooLarge)
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	return prepared, nil
}

func prepareChunkedInput(input *os.File, inputPath string, password []byte, limit int64, tooLarge string) (*chunkedInput, error) {
	info, err := input.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect input: %w", err)
	}
	switch {
	case !info.Mode().IsRegular():
		return nil, errors.New("input must be a regular file; archive folders first, for example with tar or zip")
	case info.Size() < 1:
		return nil, errors.New("input is empty; there is nothing to encrypt")
	case info.Size() > limit:
		return nil, errors.New(tooLarge)
	}
	filename, err := portableFilename(filepath.Base(inputPath))
	if err != nil {
		return nil, err
	}

	id := make([]byte, idBytes)
	contentKey := make([]byte, keyBytes)
	if _, err := io.ReadFull(rand.Reader, id); err != nil {
		return nil, fmt.Errorf("generate envelope ID: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, contentKey); err != nil {
		return nil, fmt.Errorf("generate content key: %w", err)
	}
	defer clear(contentKey)
	identifier := base64.RawURLEncoding.EncodeToString(id)
	slot, err := wrapContentKey(identifier, contentKey, password)
	if err != nil {
		return nil, err
	}
	aead, err := newGCM(contentKey)
	if err != nil {
		return nil, err
	}
	name, err := sealName(aead, identifier, filename)
	if err != nil {
		return nil, err
	}
	codename, err := Codename(identifier)
	if err != nil {
		return nil, err
	}
	header := blobHeader{Schema: schemaV1, ID: identifier, Size: info.Size(), Name: name, ChunkSize: BlobChunkBytes, Unlock: slot, Commit: encodeCommitment(contentKey, identifier)}
	return &chunkedInput{file: input, header: header, aead: aead, codename: codename}, nil
}

func (c *chunkedInput) close() {
	_ = c.file.Close()
}

// encrypt reads the input in 4 MiB chunks and passes each record's index,
// nonce, and ciphertext to emit.
func (c *chunkedInput) encrypt(emit func(index uint64, nonce, ciphertext []byte) error) error {
	buffer := make([]byte, BlobChunkBytes)
	defer clear(buffer)
	for index, remaining := uint64(0), c.header.Size; remaining > 0; index++ {
		count := int(min(int64(BlobChunkBytes), remaining))
		if _, err := io.ReadFull(c.file, buffer[:count]); err != nil {
			return fmt.Errorf("read input: %w", err)
		}
		nonce := make([]byte, nonceBytes)
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return fmt.Errorf("generate chunk nonce: %w", err)
		}
		ciphertext := c.aead.Seal(nil, nonce, buffer[:count], blobChunkMetadata(c.header.ID, c.header.Size, index, count))
		if err := emit(index, nonce, ciphertext); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		remaining -= int64(count)
	}
	return nil
}

func packStreamingBlob(inputPath, outputPath string, password []byte) (string, error) {
	input, err := openChunkedInput(inputPath, password, MaximumBlobInputBytes, fmt.Sprintf("input is larger than %d GiB, the limit for deliveries; split it first", MaximumBlobInputBytes>>30))
	if err != nil {
		return "", err
	}
	defer input.close()
	header, err := json.Marshal(input.header)
	if err != nil {
		return "", fmt.Errorf("encode blob header: %w", err)
	}
	if len(header) > maxBlobHeaderBytes {
		return "", fmt.Errorf("blob header exceeds %d bytes", maxBlobHeaderBytes)
	}
	return input.codename, writeAtomically(outputPath, func(w io.Writer) error {
		if _, err := w.Write(blobMagic[:]); err != nil {
			return fmt.Errorf("write blob magic: %w", err)
		}
		if err := binary.Write(w, binary.BigEndian, uint32(len(header))); err != nil {
			return fmt.Errorf("write blob header length: %w", err)
		}
		if _, err := w.Write(header); err != nil {
			return fmt.Errorf("write blob header: %w", err)
		}
		return input.encrypt(func(_ uint64, nonce, ciphertext []byte) error {
			if err := binary.Write(w, binary.BigEndian, uint32(len(ciphertext))); err != nil {
				return err
			}
			if _, err := w.Write(nonce); err != nil {
				return err
			}
			_, err := w.Write(ciphertext)
			return err
		})
	})
}

func blobChunkMetadata(envelopeID string, size int64, index uint64, chunkSize int) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", schemaV1, envelopeID, size, index, chunkSize))
}
