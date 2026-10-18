package envelope

import (
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

// blobHeader is the tegami/v1 streaming header. As in envelopes, the filename
// is sealed in Name with the content key.
type blobHeader struct {
	Schema    string           `json:"schema"`
	ID        string           `json:"id"`
	Size      int64            `json:"size"`
	Name      encryptedPayload `json:"name"`
	ChunkSize int              `json:"chunk_size"`
	Unlock    unlockSlot       `json:"unlock"`
	Commit    string           `json:"commit"`
}

func packStreamingBlob(inputPath, outputPath string, password []byte) (string, error) {
	var codename string
	err := writeStreamingBlob(inputPath, outputPath, password, &codename)
	return codename, err
}

func writeStreamingBlob(inputPath, outputPath string, password []byte, codename *string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("inspect input: %w", err)
	}
	switch {
	case !info.Mode().IsRegular():
		return errors.New("input must be a regular file; archive folders first, for example with tar or zip")
	case info.Size() < 1:
		return errors.New("input is empty; there is nothing to encrypt")
	case info.Size() > MaximumBlobInputBytes:
		return fmt.Errorf("input is larger than %d GiB, the limit for deliveries; split it first", MaximumBlobInputBytes>>30)
	}
	filename, err := portableFilename(filepath.Base(inputPath))
	if err != nil {
		return err
	}

	id := make([]byte, idBytes)
	contentKey := make([]byte, keyBytes)
	if _, err := io.ReadFull(rand.Reader, id); err != nil {
		return fmt.Errorf("generate envelope ID: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, contentKey); err != nil {
		return fmt.Errorf("generate content key: %w", err)
	}
	defer clear(contentKey)
	identifier := base64.RawURLEncoding.EncodeToString(id)
	slot, err := wrapContentKey(identifier, contentKey, password)
	if err != nil {
		return err
	}
	aead, err := newGCM(contentKey)
	if err != nil {
		return err
	}
	name, err := sealName(aead, identifier, filename)
	if err != nil {
		return err
	}
	if *codename, err = Codename(identifier); err != nil {
		return err
	}
	header, err := json.Marshal(blobHeader{Schema: schemaV1, ID: identifier, Size: info.Size(), Name: name, ChunkSize: BlobChunkBytes, Unlock: slot, Commit: encodeCommitment(contentKey, identifier)})
	if err != nil {
		return fmt.Errorf("encode blob header: %w", err)
	}
	if len(header) > maxBlobHeaderBytes {
		return fmt.Errorf("blob header exceeds %d bytes", maxBlobHeaderBytes)
	}

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
	if _, err := temporary.Write(blobMagic[:]); err != nil {
		return fmt.Errorf("write blob magic: %w", err)
	}
	if err := binary.Write(temporary, binary.BigEndian, uint32(len(header))); err != nil {
		return fmt.Errorf("write blob header length: %w", err)
	}
	if _, err := temporary.Write(header); err != nil {
		return fmt.Errorf("write blob header: %w", err)
	}
	buffer := make([]byte, BlobChunkBytes)
	for index, remaining := uint64(0), info.Size(); remaining > 0; index++ {
		count := BlobChunkBytes
		if remaining < int64(count) {
			count = int(remaining)
		}
		if _, err := io.ReadFull(input, buffer[:count]); err != nil {
			return fmt.Errorf("read input: %w", err)
		}
		nonce := make([]byte, nonceBytes)
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return fmt.Errorf("generate chunk nonce: %w", err)
		}
		ciphertext := aead.Seal(nil, nonce, buffer[:count], blobChunkMetadata(identifier, info.Size(), index, count))
		if err := binary.Write(temporary, binary.BigEndian, uint32(len(ciphertext))); err != nil {
			return fmt.Errorf("write chunk length: %w", err)
		}
		if _, err := temporary.Write(nonce); err != nil {
			return fmt.Errorf("write chunk nonce: %w", err)
		}
		if _, err := temporary.Write(ciphertext); err != nil {
			return fmt.Errorf("write encrypted chunk: %w", err)
		}
		remaining -= int64(count)
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

func blobChunkMetadata(envelopeID string, size int64, index uint64, chunkSize int) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", schemaV1, envelopeID, size, index, chunkSize))
}
