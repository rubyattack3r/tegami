package envelope

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestPackHTMLRoundTrip(t *testing.T) {
	password := []byte("correct horse battery staple")
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "quarterly notes.txt")
	outputPath := filepath.Join(directory, "delivery.html")
	plaintext := []byte("reviewed release notes\n")
	if err := os.WriteFile(inputPath, plaintext, 0o600); err != nil {
		t.Fatal(err)
	}
	codename, err := PackHTML(inputPath, outputPath, password)
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), string(plaintext)) || containsNonempty(page, password) {
		t.Fatal("output contains the plaintext or password")
	}
	if strings.Contains(string(page), "quarterly") {
		t.Fatal("output reveals the filename")
	}
	if strings.Contains(string(page), "Tegami") {
		t.Fatal("the delivery page names Tegami")
	}
	data := packageFromPage(t, page)
	if data.Size != int64(len(plaintext)) || data.Schema != schemaV1 || len(data.Unlocks) != 1 || data.Unlocks[0].Type != passwordSlot {
		t.Fatalf("package metadata = %#v", data)
	}
	if want, _ := Codename(data.ID); codename != want || !strings.Contains(string(page), "<title>"+codename+"</title>") {
		t.Fatalf("codename %q does not match the envelope ID", codename)
	}
	name, got, err := decrypt(data, password)
	if err != nil || name != "quarterly notes.txt" || string(got) != string(plaintext) {
		t.Fatalf("decrypt: %v, %q, %q", err, name, got)
	}
}

func containsNonempty(haystack, needle []byte) bool {
	return len(needle) != 0 && strings.Contains(string(haystack), string(needle))
}

func TestPackRejectsUnsafeInputs(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.bin")
	if err := os.WriteFile(inputPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PackHTML(inputPath, filepath.Join(directory, "short.html"), []byte("too short")); err == nil {
		t.Fatal("short password accepted")
	}
	if err := validatePassword([]byte("valid password\nsecond line")); err == nil {
		t.Fatal("multiline password accepted")
	}
}

func TestPackHTMLAndBlob(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "報告書.txt")
	plaintext := []byte("private report\n")
	password := []byte("correct horse battery staple")
	if err := os.WriteFile(inputPath, plaintext, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		pack func(string, string, []byte) (string, error)
		path string
	}{
		{name: "html", pack: PackHTML, path: filepath.Join(directory, "delivery.html")},
		{name: "blob", pack: PackBlob, path: filepath.Join(directory, "delivery.tegami")},
	} {
		t.Run(test.name, func(t *testing.T) {
			codename, err := test.pack(inputPath, test.path, password)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if containsNonempty(content, plaintext) || containsNonempty(content, password) {
				t.Fatal("output contains plaintext")
			}
			if strings.Contains(string(content), "報告書") {
				t.Fatal("output reveals the filename")
			}
			var id, name string
			var got []byte
			if test.name == "html" {
				data := packageFromPage(t, content)
				id = data.ID
				name, got, err = decrypt(data, password)
			} else {
				if !bytes.HasPrefix(content, blobMagic[:]) {
					t.Fatal("blob does not use the streaming format")
				}
				id, name, got, err = decryptBlob(content, password)
			}
			if err != nil || name != "報告書.txt" || string(got) != string(plaintext) {
				t.Fatalf("decrypt: %v, %q, %q", err, name, got)
			}
			if want, _ := Codename(id); codename != want {
				t.Fatalf("codename = %q, want %q", codename, want)
			}
		})
	}
}

func TestCodenames(t *testing.T) {
	for _, list := range [][]string{codenameWords.Adjectives, codenameWords.Nouns} {
		seen := map[string]bool{}
		for _, word := range list {
			if !regexp.MustCompile(`^[a-z]{3,8}$`).MatchString(word) || seen[word] {
				t.Fatalf("word %q is malformed or repeated", word)
			}
			seen[word] = true
		}
	}
	name, err := Codename(base64.RawURLEncoding.EncodeToString([]byte{0, 255, 7, 7}))
	if err != nil || name != codenameWords.Adjectives[0]+"-"+codenameWords.Nouns[255] {
		t.Fatalf("Codename = %q, %v", name, err)
	}
	if _, err := Codename("!"); err == nil {
		t.Fatal("invalid envelope ID accepted")
	}
	if !strings.Contains(studioPage, `"adjectives": ["`+codenameWords.Adjectives[0]) {
		t.Fatal("Studio does not embed the codename wordlist")
	}
}

func TestStudioIsDeterministic(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "first.html"), filepath.Join(directory, "second.html")
	if err := WriteStudio(first); err != nil {
		t.Fatal(err)
	}
	if err := WriteStudio(second); err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two Studio builds differ")
	}
	studio := string(a)
	for _, expected := range []string{
		`id="seal"`,
		`id="open"`,
		`<script id="tegami-package" type="application/json">__TEGAMI_PACKAGE__</script>`,
		`<title>Tegami</title>`,
		"\n<body>\n",
		"const chunkBytes = 4194304;",
		"showSaveFilePicker",
	} {
		if !strings.Contains(studio, expected) {
			t.Fatalf("Studio missing %q", expected)
		}
	}
	// The script builds HTML deliveries by replacing these exact strings.
	markup := regexp.MustCompile(`(?s)<script>.*?</script>`).ReplaceAllString(studio, "")
	if strings.Count(markup, "__TEGAMI_PACKAGE__") != 1 || strings.Count(markup, "<body>") != 1 || strings.Count(markup, "<title>Tegami</title>") != 1 {
		t.Fatal("Studio placeholders are ambiguous")
	}
}

func TestPagesAllowOnlyTheirInlineCode(t *testing.T) {
	data, err := seal("report.txt", []byte("private report\n"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := renderPage(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), "Tegami") {
		t.Fatal("delivery page names Tegami")
	}
	for name, content := range map[string]string{"studio": studioPage, "delivery": string(page)} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(content, "'nonce-") || strings.Contains(content, "nonce=") {
				t.Fatal("page uses a CSP nonce")
			}
			policy := regexp.MustCompile(`<meta http-equiv="Content-Security-Policy" content="([^"]+)">`).FindStringSubmatch(content)
			if len(policy) != 2 || !strings.Contains(policy[1], "connect-src 'none'") {
				t.Fatal("missing offline content security policy")
			}
			scripts := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(content, -1)
			styles := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindAllStringSubmatch(content, -1)
			if len(scripts) != 1 || len(styles) != 1 || strings.Count(content, "<script")-strings.Count(content, `<script id="tegami-`) != 1 || strings.Count(content, "<style") != 1 {
				t.Fatalf("unexpected inline code: %d scripts, %d styles", len(scripts), len(styles))
			}
			if want := contentSecurityPolicy(scripts[0][1], styles[0][1]); policy[1] != want {
				t.Fatalf("policy does not match inline code:\n got %s\nwant %s", policy[1], want)
			}
			if strings.Contains(content, " style=") || regexp.MustCompile(` on[a-z]+=`).MatchString(content) {
				t.Fatal("inline attributes are blocked by the policy")
			}
			if regexp.MustCompile(`(src|href)="(https?:)?//`).MatchString(content) || strings.Contains(styles[0][1], "url(") {
				t.Fatal("page references an external resource")
			}
		})
	}
}

func TestHumanSize(t *testing.T) {
	for size, want := range map[int64]string{1: "1 bytes", 1023: "1023 bytes", 1024: "1.0 KiB", 50 << 20: "50.0 MiB", 3 << 30: "3.0 GiB"} {
		if got := humanSize(size); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestStreamingBlobUsesFourMiBRecords(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "large.bin")
	plaintext := bytes.Repeat([]byte("x"), BlobChunkBytes+1)
	if err := os.WriteFile(inputPath, plaintext, 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(directory, "large.tegami")
	password := []byte("correct horse battery staple")
	if _, err := PackBlob(inputPath, outputPath, password); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) < 12 || binary.BigEndian.Uint32(content[8:12]) > maxBlobHeaderBytes {
		t.Fatal("invalid streaming header")
	}
	if _, _, got, err := decryptBlob(content, password); err != nil || !bytes.Equal(got, plaintext) {
		t.Fatalf("decrypt blob: %v", err)
	}
}

func TestWrongPasswordCannotDecrypt(t *testing.T) {
	data, err := seal("payload.bin", []byte("payload"), []byte("correct password"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decrypt(data, []byte("incorrect password")); err == nil {
		t.Fatal("wrong password decrypted")
	}
}

func decrypt(data packageData, password []byte) (string, []byte, error) {
	contentKey, err := unwrapKey(data.ID, data.Unlocks[0], password)
	if err != nil {
		return "", nil, err
	}
	if !checkCommitment(data.Commit, contentKey, data.ID) {
		return "", nil, errors.New("commitment does not match")
	}
	payload, err := newGCM(contentKey)
	if err != nil {
		return "", nil, err
	}
	padded, err := openSealedValue(payload, data.Name, nameMetadata(data.ID))
	if err != nil {
		return "", nil, err
	}
	name, ok := unpadName(padded)
	if !ok {
		return "", nil, errors.New("sealed name is not padded")
	}
	plaintext, err := openSealedValue(payload, data.Payload, payloadMetadata(data.ID, data.Size))
	return name, plaintext, err
}

func decryptBlob(content []byte, password []byte) (string, string, []byte, error) {
	if len(content) < 12 || !bytes.Equal(content[:8], blobMagic[:]) {
		return "", "", nil, io.ErrUnexpectedEOF
	}
	headerLength := int(binary.BigEndian.Uint32(content[8:12]))
	if headerLength < 1 || headerLength > maxBlobHeaderBytes || len(content) < 12+headerLength {
		return "", "", nil, io.ErrUnexpectedEOF
	}
	var header blobHeader
	if err := json.Unmarshal(content[12:12+headerLength], &header); err != nil {
		return "", "", nil, err
	}
	contentKey, err := unwrapKey(header.ID, header.Unlock, password)
	if err != nil {
		return "", "", nil, err
	}
	if !checkCommitment(header.Commit, contentKey, header.ID) {
		return "", "", nil, errors.New("commitment does not match")
	}
	payload, err := newGCM(contentKey)
	if err != nil {
		return "", "", nil, err
	}
	padded, err := openSealedValue(payload, header.Name, nameMetadata(header.ID))
	if err != nil {
		return "", "", nil, err
	}
	name, ok := unpadName(padded)
	if !ok {
		return "", "", nil, errors.New("sealed name is not padded")
	}
	offset, index := 12+headerLength, uint64(0)
	plaintext := make([]byte, 0, header.Size)
	for int64(len(plaintext)) < header.Size {
		if len(content)-offset < 16 {
			return "", "", nil, io.ErrUnexpectedEOF
		}
		length := int(binary.BigEndian.Uint32(content[offset : offset+4]))
		offset += 4
		if length < 16 || len(content)-offset < nonceBytes+length {
			return "", "", nil, io.ErrUnexpectedEOF
		}
		nonce := content[offset : offset+nonceBytes]
		offset += nonceBytes
		ciphertext := content[offset : offset+length]
		offset += length
		chunkSize := min(int64(BlobChunkBytes), header.Size-int64(len(plaintext)))
		chunk, err := payload.Open(nil, nonce, ciphertext, blobChunkMetadata(header.ID, header.Size, index, int(chunkSize)))
		if err != nil {
			return "", "", nil, err
		}
		plaintext = append(plaintext, chunk...)
		index++
	}
	if int64(len(plaintext)) != header.Size || offset != len(content) {
		return "", "", nil, io.ErrUnexpectedEOF
	}
	return header.ID, name, plaintext, nil
}

func unwrapKey(envelopeID string, slot unlockSlot, password []byte) ([]byte, error) {
	salt, err := base64.RawStdEncoding.DecodeString(slot.Salt)
	if err != nil {
		return nil, err
	}
	wrapping, err := newGCM(argon2.IDKey(password, salt, uint32(slot.Passes), uint32(slot.Memory), uint8(slot.Lanes), keyBytes))
	if err != nil {
		return nil, err
	}
	return openSealedValue(wrapping, encryptedPayload{Nonce: slot.Nonce, Ciphertext: slot.WrappedKey}, unlockMetadata(envelopeID))
}

func packageFromPage(t *testing.T, page []byte) packageData {
	t.Helper()
	match := regexp.MustCompile(`<script id="tegami-package" type="application/json">([^<]+)</script>`).FindSubmatch(page)
	if len(match) != 2 {
		t.Fatal("package JSON not found")
	}
	var data packageData
	if err := json.Unmarshal(match[1], &data); err != nil {
		t.Fatal(err)
	}
	return data
}
