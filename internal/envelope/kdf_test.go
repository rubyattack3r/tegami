package envelope

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPasswordKeyMatchesMonocypher pins the Go key derivation to the output of
// the Monocypher build the browser pages use (wasm/argon2.c), so both sides
// derive the same key from the same password and salt.
func TestPasswordKeyMatchesMonocypher(t *testing.T) {
	salt := make([]byte, saltBytes)
	for index := range salt {
		salt[index] = byte(index)
	}
	got := hex.EncodeToString(passwordKey([]byte("correct horse battery staple"), salt))
	if want := "0d1a3c6523c8f06e4e0af9c515aa5b5448cfebd6838f2d52c3d8b6ef8ddc3c2e"; got != want {
		t.Fatalf("passwordKey = %s, want %s", got, want)
	}
}

func TestReadersRejectInvalidKDFBeforeUnlock(t *testing.T) {
	data, err := seal("report.txt", []byte("report"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*unlockSlot){
		"huge memory":       func(slot *unlockSlot) { slot.Memory = 4194304 },
		"huge passes":       func(slot *unlockSlot) { slot.Passes = 1000 },
		"zero lanes":        func(slot *unlockSlot) { slot.Lanes = 0 },
		"other lanes":       func(slot *unlockSlot) { slot.Lanes = 4 },
		"negative memory":   func(slot *unlockSlot) { slot.Memory = -1 },
		"PBKDF2 slot":       func(slot *unlockSlot) { slot.Memory, slot.Passes, slot.Lanes = 0, 0, 0 },
		"short salt":        func(slot *unlockSlot) { slot.Salt = "YQ" },
		"short wrapped key": func(slot *unlockSlot) { slot.WrappedKey = "YQ" },
	} {
		t.Run(name, func(t *testing.T) {
			slot := data.Unlocks[0]
			mutate(&slot)
			modified := data
			modified.Unlocks = []unlockSlot{slot}
			page, err := renderPage(modified)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := readPage(page, FormatHTML); !errors.Is(err, ErrNotDelivery) {
				t.Fatalf("HTML: %v, want ErrNotDelivery", err)
			}
			header, err := json.Marshal(blobHeader{Schema: schemaV1, ID: data.ID, Size: data.Size, Name: data.Name, ChunkSize: BlobChunkBytes, Unlock: slot, Commit: data.Commit})
			if err != nil {
				t.Fatal(err)
			}
			blob := append([]byte(nil), blobMagic[:]...)
			blob = binary.BigEndian.AppendUint32(blob, uint32(len(header)))
			blob = append(blob, header...)
			path := filepath.Join(t.TempDir(), "invalid.tegami")
			if err := os.WriteFile(path, blob, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadDelivery(path); !errors.Is(err, ErrNotDelivery) {
				t.Fatalf("blob: %v, want ErrNotDelivery", err)
			}
		})
	}
}

func TestNamePadding(t *testing.T) {
	for _, name := range []string{"a", "報告書.pdf", strings.Repeat("x", maxNameBytes)} {
		padded := padName(name)
		if len(padded) != maxNameBytes {
			t.Fatalf("padName(%q) has %d bytes", name, len(padded))
		}
		if got, ok := unpadName(padded); !ok || got != name {
			t.Fatalf("unpadName(padName(%q)) = %q, %v", name, got, ok)
		}
	}
	for _, padded := range [][]byte{make([]byte, maxNameBytes), append([]byte("a\x00b"), make([]byte, maxNameBytes-3)...), []byte("short")} {
		if _, ok := unpadName(padded); ok {
			t.Fatalf("unpadName accepted %q", padded)
		}
	}
	short, err := seal("a", []byte("x"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	long, err := seal(strings.Repeat("y", 120), []byte("x"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	if len(short.Name.Ciphertext) != len(long.Name.Ciphertext) {
		t.Fatal("sealed names reveal their length")
	}
}

// A delivery whose commitment does not match its content key must not open,
// and readers must refuse unlock parameters other than the fixed ones.
func TestCommitmentAndParametersAreChecked(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "report.txt")
	if err := os.WriteFile(inputPath, []byte("report\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	password := []byte("correct horse battery staple")
	path := filepath.Join(directory, "delivery.html")
	if _, err := PackHTML(inputPath, path, password); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data := packageFromPage(t, page)
	rewrite := func(name, from, to string) string {
		target := filepath.Join(directory, name)
		if err := os.WriteFile(target, []byte(strings.Replace(string(page), from, to, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		return target
	}

	other := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	delivery, err := ReadDelivery(rewrite("commit.html", `"commit":"`+data.Commit+`"`, `"commit":"`+other+`"`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := delivery.Unlock(password); !errors.Is(err, ErrUnlock) {
		t.Fatalf("mismatched commitment: %v, want ErrUnlock", err)
	}

	for name, change := range map[string][2]string{
		"memory": {`"memory":65536`, `"memory":4194304`},
		"passes": {`"passes":3`, `"passes":1000`},
		"lanes":  {`"lanes":1`, `"lanes":4`},
	} {
		if _, err := ReadDelivery(rewrite(name+".html", change[0], change[1])); !errors.Is(err, ErrNotDelivery) {
			t.Fatalf("%s changed: %v, want ErrNotDelivery", name, err)
		}
	}
}

// Both pages pin the embedded Argon2id module by its SHA-256 in their
// CSP-hashed script.
func TestPagesPinTheArgon2Module(t *testing.T) {
	sum := sha256.Sum256([]byte(asset("argon2.wasm")))
	data, err := seal("report.txt", []byte("report"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := renderPage(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"studio": studioPage, "delivery": string(page)} {
		if !strings.Contains(content, `wasmSHA256 = "`+hex.EncodeToString(sum[:])+`"`) || strings.Contains(content, "__TEGAMI_ARGON2_SHA256__") {
			t.Errorf("%s page does not pin argon2.wasm", name)
		}
	}
}
