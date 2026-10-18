// browsercheck serves real pages and fixtures for the dependency-free Chromium
// integration checks. Run `make browser-check`, then open the printed URL, or
// `make browser-test` to run them in headless Chromium and exit with the result.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rubyattack3r/tegami/internal/envelope"
)

const password = "correct horse battery staple\n"
const message = "Browser and CLI round trip.\n"

// report is what the check page posts to /done when every check has run.
type report struct {
	Failures int    `json:"failures"`
	Report   string `json:"report"`
}

func main() {
	address := flag.String("addr", "127.0.0.1:8767", "listen address")
	chromium := flag.String("chromium", "", "run the checks in this headless Chromium and exit with their result")
	timeout := flag.Duration("timeout", 5*time.Minute, "how long a headless run may take")
	flag.Parse()
	if *chromium != "" {
		if err := runHeadless(*chromium, *timeout); err != nil {
			log.Fatal(err)
		}
		return
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatal(err)
	}
	handler, cleanup, err := setup(make(chan report, 1))
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()
	fmt.Printf("Open http://%s/ in Chromium. All fixtures use a public test password.\n", listener.Addr())
	log.Fatal(http.Serve(listener, handler))
}

// runHeadless serves the checks on a free local port, loads them in headless
// Chromium, and returns an error unless every check passes.
func runHeadless(chromium string, timeout time.Duration) error {
	done := make(chan report, 1)
	handler, cleanup, err := setup(done)
	if err != nil {
		return err
	}
	defer cleanup()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	profile, err := os.MkdirTemp("", "tegami-chromium-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(profile) }()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// --no-sandbox lets Chromium run in CI containers; the pages are local test fixtures.
	browser := exec.CommandContext(ctx, chromium, "--headless=new", "--no-sandbox", "--disable-gpu", "--no-first-run", "--user-data-dir="+profile, "http://"+listener.Addr().String()+"/")
	if err := browser.Start(); err != nil {
		return fmt.Errorf("start Chromium: %w", err)
	}
	defer func() {
		_ = browser.Process.Kill()
		_ = browser.Wait()
	}()
	select {
	case result := <-done:
		fmt.Println(result.Report)
		if result.Failures != 0 {
			return fmt.Errorf("%d browser checks failed", result.Failures)
		}
		return nil
	case <-ctx.Done():
		return errors.New("browser checks did not finish in time")
	}
}

func setup(done chan<- report) (http.Handler, func(), error) {
	directory, err := os.MkdirTemp("", "tegami-browser-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	handler, err := fixtures(directory, done)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return handler, cleanup, nil
}

// fixtures writes the CLI-made fixtures and Studio into directory and returns
// the handler that serves them, checks browser-made deliveries, and accepts
// the final report.
func fixtures(directory string, done chan<- report) (http.Handler, error) {
	input := filepath.Join(directory, "報告書.txt")
	if err := os.WriteFile(input, []byte(message), 0o600); err != nil {
		return nil, err
	}
	large := filepath.Join(directory, "footage.bin")
	if err := os.WriteFile(large, bytes.Repeat([]byte{42}, envelope.BlobChunkBytes+7), 0o600); err != nil {
		return nil, err
	}
	for _, format := range []string{"html", "blob"} {
		name := "cli.html"
		source := input
		if format == "blob" {
			name, source = "cli.tegami", large
		}
		command := exec.Command("./bin/tegami", "pack", "--input", source, "--output", filepath.Join(directory, name), "--format", format)
		command.Stdin = strings.NewReader(password)
		if output, err := command.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("create %s fixture: %w: %s", format, err, output)
		}
	}
	if err := envelope.WriteStudio(filepath.Join(directory, "tegami.html")); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/fixtures/", http.StripPrefix("/fixtures/", http.FileServer(http.Dir(directory))))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("internal/envelope/assets"))))
	mux.HandleFunc("POST /result/{kind}", func(w http.ResponseWriter, r *http.Request) {
		if err := checkResult(directory, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("POST /done", func(w http.ResponseWriter, r *http.Request) {
		var result report
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&result); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case done <- result:
		default:
		}
	})
	mux.Handle("/", http.FileServer(http.Dir("scripts/browsercheck")))
	return mux, nil
}

// Browser-created deliveries must open through the actual CLI, including its
// atomic-output path. The password is supplied through stdin, never arguments.
func checkResult(directory string, r *http.Request) error {
	kind := r.PathValue("kind")
	if kind != "html" && kind != "blob" && kind != "zip" {
		return fmt.Errorf("unknown test")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "browser-"+kind)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	var output bytes.Buffer
	command := exec.Command("./bin/tegami", "open", "--input", path, "--output", "-")
	command.Stdin = strings.NewReader(password)
	command.Stdout = &output
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		return fmt.Errorf("CLI open: %w: %s", err, diagnostics.Bytes())
	}
	if kind == "zip" {
		archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
		if err != nil {
			return err
		}
		if len(archive.File) != 2 {
			return fmt.Errorf("ZIP has %d entries, want 2", len(archive.File))
		}
		for index, entry := range archive.File {
			if entry.Name != []string{"one.txt", "two.txt"}[index] {
				return fmt.Errorf("unexpected ZIP name %q", entry.Name)
			}
			reader, err := entry.Open()
			if err != nil {
				return err
			}
			content, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || string(content) != message {
				return fmt.Errorf("ZIP content mismatch: %v", err)
			}
		}
	} else {
		want := []byte(message)
		if kind == "blob" {
			want = bytes.Repeat([]byte{42}, envelope.BlobChunkBytes+7)
		}
		if sha256.Sum256(output.Bytes()) != sha256.Sum256(want) {
			return fmt.Errorf("plaintext mismatch")
		}
	}
	command = exec.Command("./bin/tegami", "open", "--input", path, "--output", directory, "--force", "--json")
	command.Stdin = strings.NewReader(password)
	metadata, err := command.Output()
	if err != nil {
		return err
	}
	var result struct{ Name string }
	if err := json.Unmarshal(metadata, &result); err != nil {
		return err
	}
	wantName := map[string]string{"html": "報告書.txt", "blob": "footage.bin", "zip": "files.zip"}[kind]
	if result.Name != wantName {
		return fmt.Errorf("filename = %q, want %q", result.Name, wantName)
	}
	return nil
}
