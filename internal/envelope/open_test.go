package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRoundTrip(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "board minutes.txt")
	plaintext := bytes.Repeat([]byte("minutes "), BlobChunkBytes/8+3)
	if err := os.WriteFile(inputPath, plaintext, 0o600); err != nil {
		t.Fatal(err)
	}
	password := []byte("correct horse battery staple")
	for _, test := range []struct {
		format Format
		pack   func(string, string, []byte) (string, error)
	}{
		{FormatHTML, PackHTML},
		{FormatBlob, PackBlob},
	} {
		t.Run(string(test.format), func(t *testing.T) {
			path := filepath.Join(directory, "delivery."+string(test.format))
			codename, err := test.pack(inputPath, path, password)
			if err != nil {
				t.Fatal(err)
			}
			delivery, err := ReadDelivery(path)
			if err != nil {
				t.Fatal(err)
			}
			if delivery.Format != test.format || delivery.Codename != codename || delivery.Size != int64(len(plaintext)) {
				t.Fatalf("delivery = %+v", delivery)
			}
			if _, err := delivery.Unlock([]byte("an incorrect password")); !errors.Is(err, ErrUnlock) {
				t.Fatalf("wrong password: %v", err)
			}
			unlocked, err := delivery.Unlock(password)
			if err != nil {
				t.Fatal(err)
			}
			defer unlocked.Close()
			var output bytes.Buffer
			if _, err := unlocked.WriteTo(&output); err != nil {
				t.Fatal(err)
			}
			if unlocked.Name != "board minutes.txt" || !bytes.Equal(output.Bytes(), plaintext) {
				t.Fatalf("opened %q with %d bytes", unlocked.Name, output.Len())
			}
		})
	}
}

func TestOpenRejectsDamagedAndForeignFiles(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "report.txt")
	if err := os.WriteFile(inputPath, []byte("private report\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	password := []byte("correct horse battery staple")
	blobPath := filepath.Join(directory, "delivery.tegami")
	if _, err := PackBlob(inputPath, blobPath, password); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, content []byte) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	damaged := bytes.Clone(blob)
	damaged[len(damaged)-1] ^= 1
	extended := append(bytes.Clone(blob), 0)
	for name, path := range map[string]string{"damaged": write("damaged.tegami", damaged), "extended": write("extended.tegami", extended)} {
		delivery, err := ReadDelivery(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		unlocked, err := delivery.Unlock(password)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := unlocked.WriteTo(&bytes.Buffer{}); !errors.Is(err, ErrUnlock) {
			t.Fatalf("%s: WriteTo = %v, want ErrUnlock", name, err)
		}
	}
	other := bytes.Clone(blob)
	copy(other, "TEGAMI3\x00")
	if _, err := ReadDelivery(write("other.tegami", other)); !errors.Is(err, ErrNotDelivery) {
		t.Fatalf("blob with another version: %v", err)
	}
	for _, content := range []string{"", "hello", "<html>no package</html>", `<script id="tegami-package" type="application/json">__TEGAMI_PACKAGE__</script>`} {
		if _, err := ReadDelivery(write("foreign.html", []byte(content))); !errors.Is(err, ErrNotDelivery) {
			t.Fatalf("ReadDelivery(%q) = %v, want ErrNotDelivery", content, err)
		}
	}
}

// HTML files made by Tegami 0.1.0 hold one payload in their package JSON.
// Only their own page opens them; readers point there instead.
func TestRejectVersionZeroOneHTML(t *testing.T) {
	header := headerFromPage(t, packedPage(t, []byte("report")))
	raw, err := json.Marshal(map[string]any{"schema": schemaV1, "id": header.ID, "size": header.Size, "commit": header.Commit, "name": header.Name, "payload": header.Name, "unlocks": []unlockSlot{header.Unlock}})
	if err != nil {
		t.Fatal(err)
	}
	codename, err := Codename(header.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := renderPage(codename, header.Size, raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "delivery.html")
	if err := os.WriteFile(path, page, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDelivery(path); !errors.Is(err, ErrNotDelivery) || !strings.Contains(err.Error(), "open the file in a browser") {
		t.Fatalf("ReadDelivery = %v, want ErrNotDelivery pointing to the browser", err)
	}
}

// Each record of an HTML delivery must authenticate, appear once, in order,
// directly after the previous one, and be the last when the size is reached.
func TestOpenRejectsDamagedHTMLRecords(t *testing.T) {
	page := packedPage(t, bytes.Repeat([]byte("x"), BlobChunkBytes+5))
	_, start, _ := findPackage(page)
	second := bytes.Index(page, []byte(recordStart(1)))
	end := second + bytes.Index(page[second:], []byte(recordEnd)) + len(recordEnd)
	first, last := page[start:second], page[second:end]
	if !bytes.HasPrefix(first, []byte(recordStart(0))) {
		t.Fatal("first record is not where expected")
	}
	splice := func(records ...[]byte) []byte {
		out := bytes.Clone(page[:start])
		for _, record := range records {
			out = append(out, record...)
		}
		return append(out, page[end:]...)
	}
	flipped := bytes.Clone(page)
	flipped[second+len(recordStart(1))+40] ^= 'A' ^ 'B'
	renamed := func(record []byte, from, to uint64) []byte {
		return bytes.Replace(record, []byte(recordStart(from)), []byte(recordStart(to)), 1)
	}
	extra := renamed(last, 1, 2)
	password := []byte("correct horse battery staple")
	directory := t.TempDir()
	for name, content := range map[string][]byte{
		"flipped":   flipped,
		"dropped":   splice(first),
		"swapped":   splice(renamed(last, 1, 0), renamed(first, 0, 1)),
		"repeated":  splice(first, renamed(first, 0, 1)),
		"extra":     splice(first, last, extra),
		"separated": splice(first, []byte("\n"), last),
		"truncated": page[:second+100],
	} {
		path := filepath.Join(directory, name+".html")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := openPath(path, password); !errors.Is(err, ErrUnlock) {
			t.Fatalf("%s: %v, want ErrUnlock", name, err)
		}
	}
	oversized := bytes.Replace(page, []byte(`"size":4194309`), []byte(`"size":524288001`), 1)
	path := filepath.Join(directory, "oversized.html")
	if err := os.WriteFile(path, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDelivery(path); !errors.Is(err, ErrNotDelivery) {
		t.Fatalf("oversized: %v, want ErrNotDelivery", err)
	}
}
