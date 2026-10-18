package envelope

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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
