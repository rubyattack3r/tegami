package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPassword = "correct horse battery staple\n"

func TestPackAndOpenThroughPipes(t *testing.T) {
	directory := t.TempDir()
	passwordFile := filepath.Join(directory, "password")
	if err := os.WriteFile(passwordFile, []byte(testPassword), 0o600); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("quarterly numbers\n")
	for _, format := range []string{"html", "blob"} {
		t.Run(format, func(t *testing.T) {
			var delivery, codename bytes.Buffer
			if err := run([]string{"pack", "--input", "-", "--name", "q3.csv", "--output", "-", "--format", format, "--password-file", passwordFile}, bytes.NewReader(plaintext), &delivery, &codename); err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(delivery.Bytes(), []byte("q3.csv")) || !strings.Contains(codename.String(), "-") {
				t.Fatalf("delivery reveals its name or no codename was printed: %q", codename.String())
			}
			var opened bytes.Buffer
			if err := run([]string{"open", "--input", "-", "--output", "-", "--password-file", passwordFile}, bytes.NewReader(delivery.Bytes()), &opened, nil); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(opened.Bytes(), plaintext) {
				t.Fatalf("opened %q", opened.Bytes())
			}
		})
	}
}

func TestPackAndOpenFilesWithJSON(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "board minutes.txt")
	if err := os.WriteFile(inputPath, []byte("minutes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deliveryPath := filepath.Join(directory, "delivery.html")
	var packed bytes.Buffer
	if err := run([]string{"pack", "--input", inputPath, "--output", deliveryPath, "--format", "html", "--json"}, strings.NewReader(testPassword), &packed, nil); err != nil {
		t.Fatal(err)
	}
	var packResult packResult
	if err := json.Unmarshal(packed.Bytes(), &packResult); err != nil || packResult.Codename == "" || packResult.Format != "html" || packResult.Size != 8 || packResult.Output != deliveryPath {
		t.Fatalf("pack JSON = %s, %v", packed.Bytes(), err)
	}
	outputDirectory := filepath.Join(directory, "out")
	if err := os.Mkdir(outputDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	var opened bytes.Buffer
	if err := run([]string{"open", "--input", deliveryPath, "--output", outputDirectory, "--json"}, strings.NewReader(testPassword), &opened, nil); err != nil {
		t.Fatal(err)
	}
	var openResult openResult
	want := filepath.Join(outputDirectory, "board minutes.txt")
	if err := json.Unmarshal(opened.Bytes(), &openResult); err != nil || openResult.Name != "board minutes.txt" || openResult.Codename != packResult.Codename || openResult.Output != want {
		t.Fatalf("open JSON = %s, %v", opened.Bytes(), err)
	}
	if content, err := os.ReadFile(want); err != nil || string(content) != "minutes\n" {
		t.Fatalf("opened file = %q, %v", content, err)
	}
	err := run([]string{"open", "--input", deliveryPath, "--output", outputDirectory}, strings.NewReader(testPassword), &bytes.Buffer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") || exitCode(err) != exitFailure {
		t.Fatalf("overwrite without --force: %v", err)
	}
	if err := run([]string{"open", "--input", deliveryPath, "--output", outputDirectory, "--force"}, strings.NewReader(testPassword), &bytes.Buffer{}, nil); err != nil {
		t.Fatalf("overwrite with --force: %v", err)
	}
}

func TestExitCodes(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "report.txt")
	if err := os.WriteFile(inputPath, []byte("report\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deliveryPath := filepath.Join(directory, "delivery.html")
	if err := run([]string{"pack", "--input", inputPath, "--output", deliveryPath}, strings.NewReader(testPassword), &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(directory, "delivery.zip")
	if err := os.WriteFile(zipPath, []byte("PK\x03\x04 a zipped delivery"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		args  []string
		stdin string
		code  int
	}{
		{"unknown command", []string{"unpack"}, "", exitUsage},
		{"missing flags", []string{"pack"}, "", exitUsage},
		{"stdin without password file", []string{"pack", "--input", "-", "--name", "x", "--output", "x"}, "", exitUsage},
		{"short password", []string{"pack", "--input", inputPath, "--output", filepath.Join(directory, "short.html")}, "too short\n", exitFailure},
		{"wrong password", []string{"open", "--input", deliveryPath, "--output", "-"}, "an incorrect password\n", exitUnlock},
		{"not a delivery", []string{"open", "--input", inputPath, "--output", "-"}, testPassword, exitNotDelivery},
		{"inspect not a delivery", []string{"inspect", "--input", inputPath}, "", exitNotDelivery},
		{"two password lines", []string{"open", "--input", deliveryPath, "--output", "-"}, testPassword + testPassword, exitUsage},
		{"removed unlock flag", []string{"pack", "--input", inputPath, "--output", deliveryPath, "--unlock", "user-agent"}, testPassword, exitUsage},
		{"removed zip format", []string{"pack", "--input", inputPath, "--output", zipPath, "--format", "zip"}, testPassword, exitUsage},
		{"zip input", []string{"open", "--input", zipPath, "--output", "-"}, testPassword, exitNotDelivery},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := run(test.args, strings.NewReader(test.stdin), &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || exitCode(err) != test.code {
				t.Fatalf("exit code for %v = %d, want %d", err, exitCode(err), test.code)
			}
		})
	}
}

func TestInspectAndPassword(t *testing.T) {
	var generated bytes.Buffer
	if err := run([]string{"password"}, nil, &generated, nil); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "salaries.csv")
	if err := os.WriteFile(inputPath, []byte("name,salary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deliveryPath := filepath.Join(directory, "delivery.tegami")
	var codename bytes.Buffer
	if err := run([]string{"pack", "--input", inputPath, "--output", deliveryPath, "--format", "blob"}, bytes.NewReader(generated.Bytes()), &codename, nil); err != nil {
		t.Fatal(err)
	}
	var inspected bytes.Buffer
	if err := run([]string{"inspect", "--input", deliveryPath, "--json"}, nil, &inspected, nil); err != nil {
		t.Fatal(err)
	}
	var result inspectResult
	if err := json.Unmarshal(inspected.Bytes(), &result); err != nil || result.Codename != strings.TrimSpace(codename.String()) || result.Format != "blob" || result.Size != 12 {
		t.Fatalf("inspect JSON = %s, %v", inspected.Bytes(), err)
	}
	inspected.Reset()
	if err := run([]string{"inspect", "--input", deliveryPath}, nil, &inspected, nil); err != nil || strings.Contains(inspected.String(), "salaries") {
		t.Fatalf("inspect = %q, %v", inspected.String(), err)
	}
	var opened bytes.Buffer
	if err := run([]string{"open", "--input", deliveryPath, "--output", "-"}, bytes.NewReader(generated.Bytes()), &opened, nil); err != nil || opened.String() != "name,salary\n" {
		t.Fatalf("open with the generated password = %q, %v", opened.String(), err)
	}
}

// A delivery damaged in its last chunk must not leave any output behind, even
// though every earlier chunk decrypts correctly.
func TestDamagedLastChunkLeavesNoOutput(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "footage.bin")
	if err := os.WriteFile(inputPath, bytes.Repeat([]byte("frame "), 1_500_000), 0o600); err != nil {
		t.Fatal(err)
	}
	deliveryPath := filepath.Join(directory, "delivery.tegami")
	if err := run([]string{"pack", "--input", inputPath, "--output", deliveryPath, "--format", "blob"}, strings.NewReader(testPassword), &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(deliveryPath)
	if err != nil {
		t.Fatal(err)
	}
	content[len(content)-1] ^= 1
	if err := os.WriteFile(deliveryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	outputDirectory := filepath.Join(directory, "out")
	if err := os.Mkdir(outputDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"open", "--input", deliveryPath, "--output", outputDirectory}, strings.NewReader(testPassword), &bytes.Buffer{}, nil)
	if exitCode(err) != exitUnlock {
		t.Fatalf("open damaged delivery: %v", err)
	}
	if entries, _ := os.ReadDir(outputDirectory); len(entries) != 0 {
		t.Fatalf("damaged delivery left %d files behind", len(entries))
	}
	var stdout bytes.Buffer
	if err := run([]string{"open", "--input", deliveryPath, "--output", "-"}, strings.NewReader(testPassword), &stdout, nil); exitCode(err) != exitUnlock || stdout.Len() != 0 {
		t.Fatalf("open to stdout: %v with %d bytes written", err, stdout.Len())
	}
	target := filepath.Join(outputDirectory, "footage.bin")
	if err := os.WriteFile(target, []byte("keep existing output"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"open", "--input", deliveryPath, "--output", outputDirectory, "--force"}, strings.NewReader(testPassword), &bytes.Buffer{}, nil)
	if exitCode(err) != exitUnlock {
		t.Fatalf("force open damaged delivery: %v", err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "keep existing output" {
		t.Fatalf("damaged delivery replaced output: %q, %v", content, err)
	}
	if entries, err := os.ReadDir(outputDirectory); err != nil || len(entries) != 1 {
		t.Fatalf("damaged delivery left temporary output: %v, %v", entries, err)
	}
}
