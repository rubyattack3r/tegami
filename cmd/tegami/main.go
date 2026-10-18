package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rubyattack3r/tegami/internal/envelope"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `usage:
  tegami pack --input FILE|- --output FILE|- [--format html|blob] [--password-file FILE] [--name NAME] [--json]
  tegami open --input FILE|- [--output FILE|DIR|-] [--password-file FILE] [--force] [--json]
  tegami inspect --input FILE|- [--json]
  tegami password
  tegami studio [--output tegami.html]
  tegami version

The password is read as one line from standard input, or from --password-file.
Exit status: 0 success, 1 failure, 2 usage error, 3 wrong password or damaged
delivery, 4 not a Tegami delivery.
Documentation: https://github.com/rubyattack3r/tegami#readme`

// Exit statuses, so scripts can tell failures apart.
const (
	exitFailure     = 1
	exitUsage       = 2
	exitUnlock      = 3
	exitNotDelivery = 4
)

type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

func usageError(format string, args ...any) error {
	return exitError{exitUsage, fmt.Errorf(format+"; run 'tegami help' for usage", args...)}
}

func main() {
	err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "tegami:", err)
	os.Exit(exitCode(err))
}

func exitCode(err error) int {
	var exit exitError
	switch {
	case errors.As(err, &exit):
		return exit.code
	case errors.Is(err, envelope.ErrUnlock):
		return exitUnlock
	case errors.Is(err, envelope.ErrNotDelivery):
		return exitNotDelivery
	}
	return exitFailure
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("missing command")
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		_, err := fmt.Fprintln(stdout, usage)
		return err
	case "version", "--version":
		_, err := fmt.Fprintln(stdout, "tegami", version)
		return err
	case "studio":
		flags := newFlags("studio")
		output := flags.String("output", "tegami.html", "")
		if err := parse(flags, args[1:]); err != nil {
			return err
		}
		if *output == "" {
			return usageError("--output must not be empty")
		}
		return envelope.WriteStudio(*output)
	case "pack":
		return pack(args[1:], stdin, stdout, stderr)
	case "open":
		return open(args[1:], stdin, stdout)
	case "inspect":
		return inspect(args[1:], stdin, stdout)
	case "password":
		if err := parse(newFlags("password"), args[1:]); err != nil {
			return err
		}
		password, err := envelope.GeneratePassword()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, password)
		return err
	}
	return usageError("unknown command %q", args[0])
}

func newFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

func parse(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		return usageError("%s", err)
	}
	if flags.NArg() != 0 {
		return usageError("unexpected argument %q", flags.Arg(0))
	}
	return nil
}

type packResult struct {
	Codename string `json:"codename"`
	Format   string `json:"format"`
	Size     int64  `json:"size"`
	Output   string `json:"output"`
}

func pack(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := newFlags("pack")
	input := flags.String("input", "", "")
	output := flags.String("output", "", "")
	format := flags.String("format", string(envelope.FormatHTML), "")
	passwordFile := flags.String("password-file", "", "")
	name := flags.String("name", "", "")
	asJSON := flags.Bool("json", false, "")
	if err := parse(flags, args); err != nil {
		return err
	}
	switch {
	case *input == "" || *output == "":
		return usageError("pack needs --input and --output")
	case *input == "-" && *passwordFile == "":
		return usageError("--input - needs --password-file, because standard input carries the file")
	case *input == "-" && *name == "":
		return usageError("--input - needs --name, the filename to seal inside the delivery")
	case *input != "-" && *name != "":
		return usageError("--name only applies with --input -")
	case *output == "-" && *asJSON:
		return usageError("--json cannot be combined with --output -")
	}
	var packer func(string, string, []byte) (string, error)
	switch envelope.Format(*format) {
	case envelope.FormatHTML:
		packer = envelope.PackHTML
	case envelope.FormatBlob:
		packer = envelope.PackBlob
	default:
		return usageError("--format must be html or blob")
	}
	password, err := readPassword(*passwordFile, stdin)
	if err != nil {
		return err
	}
	defer clear(password)

	workspace, err := os.MkdirTemp("", "tegami-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	inputPath := *input
	if inputPath == "-" {
		if inputPath, err = spool(stdin, workspace, *name, envelope.MaximumBlobInputBytes); err != nil {
			return err
		}
	}
	info, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	outputPath := *output
	if outputPath == "-" {
		outputPath = filepath.Join(workspace, "delivery")
	}
	codename, err := packer(inputPath, outputPath, password)
	if err != nil {
		return err
	}
	switch {
	case *output == "-":
		// Standard output carries the delivery, so the codename goes to stderr.
		if err := copyFile(stdout, outputPath); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stderr, codename)
	case *asJSON:
		err = json.NewEncoder(stdout).Encode(packResult{Codename: codename, Format: *format, Size: info.Size(), Output: outputPath})
	default:
		_, err = fmt.Fprintln(stdout, codename)
	}
	return err
}

type openResult struct {
	Codename string `json:"codename"`
	Name     string `json:"name"`
	Format   string `json:"format"`
	Size     int64  `json:"size"`
	Output   string `json:"output"`
}

func open(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("open")
	input := flags.String("input", "", "")
	output := flags.String("output", "", "")
	passwordFile := flags.String("password-file", "", "")
	force := flags.Bool("force", false, "")
	asJSON := flags.Bool("json", false, "")
	if err := parse(flags, args); err != nil {
		return err
	}
	switch {
	case *input == "":
		return usageError("open needs --input")
	case *input == "-" && *passwordFile == "":
		return usageError("--input - needs --password-file, because standard input carries the delivery")
	case *output == "-" && *asJSON:
		return usageError("--json cannot be combined with --output -")
	}

	workspace, err := os.MkdirTemp("", "tegami-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	delivery, err := readDelivery(*input, workspace, stdin)
	if err != nil {
		return err
	}
	password, err := readPassword(*passwordFile, stdin)
	if err != nil {
		return err
	}
	defer clear(password)
	unlocked, err := delivery.Unlock(password)
	if err != nil {
		return err
	}
	defer unlocked.Close()

	// The recovered name comes from the delivery, so it never overwrites an
	// existing file without --force.
	target := *output
	switch target {
	case "-":
		target = filepath.Join(workspace, "plaintext")
	case "":
		target = unlocked.Name
	default:
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			target = filepath.Join(target, unlocked.Name)
		}
	}
	if *output != "-" && !*force {
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("%s already exists; use --force to replace it", target)
		}
	}
	if err := writeAtomically(target, unlocked); err != nil {
		return err
	}
	switch {
	case *output == "-":
		return copyFile(stdout, target)
	case *asJSON:
		return json.NewEncoder(stdout).Encode(openResult{Codename: delivery.Codename, Name: unlocked.Name, Format: string(delivery.Format), Size: delivery.Size, Output: target})
	}
	_, err = fmt.Fprintln(stdout, target)
	return err
}

type inspectResult struct {
	Codename string `json:"codename"`
	Format   string `json:"format"`
	Size     int64  `json:"size"`
}

// inspect shows what anyone holding a delivery can see without the password.
func inspect(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("inspect")
	input := flags.String("input", "", "")
	asJSON := flags.Bool("json", false, "")
	if err := parse(flags, args); err != nil {
		return err
	}
	if *input == "" {
		return usageError("inspect needs --input")
	}
	workspace, err := os.MkdirTemp("", "tegami-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	delivery, err := readDelivery(*input, workspace, stdin)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(inspectResult{Codename: delivery.Codename, Format: string(delivery.Format), Size: delivery.Size})
	}
	_, err = fmt.Fprintf(stdout, "codename  %s\nformat    %s\nsize      %d bytes\n", delivery.Codename, delivery.Format, delivery.Size)
	return err
}

// readDelivery reads the delivery at input, or from stdin when input is "-".
func readDelivery(input, workspace string, stdin io.Reader) (*envelope.Delivery, error) {
	if input == "-" {
		// Large enough for any delivery: a 3 GiB blob plus its record overhead.
		path, err := spool(stdin, workspace, "delivery", envelope.MaximumBlobInputBytes+envelope.MaximumBlobInputBytes/64)
		if err != nil {
			return nil, err
		}
		input = path
	}
	return envelope.ReadDelivery(input)
}

// readPassword reads one line, the password, from path or, when path is
// empty, from stdin.
func readPassword(path string, stdin io.Reader) ([]byte, error) {
	source := stdin
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open password file: %w", err)
		}
		defer func() { _ = file.Close() }()
		source = file
	}
	password, err := io.ReadAll(io.LimitReader(source, envelope.MaximumPasswordBytes+3))
	if err != nil {
		return nil, fmt.Errorf("read password: %w", err)
	}
	// Trim in place, so the caller can clear every byte that was read.
	line := bytes.TrimSuffix(bytes.TrimSuffix(password, []byte("\n")), []byte("\r"))
	if bytes.IndexByte(line, '\n') >= 0 {
		clear(password)
		return nil, usageError("expected one line: the password")
	}
	return line, nil
}

// spool copies standard input to a private file named name, so the packers and
// readers can work from a path.
func spool(stdin io.Reader, directory, name string, limit int64) (string, error) {
	if name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return "", usageError("--name must be a plain filename")
	}
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	n, err := io.Copy(file, io.LimitReader(stdin, limit+1))
	if err != nil {
		return "", fmt.Errorf("read standard input: %w", err)
	}
	if n > limit {
		return "", fmt.Errorf("standard input is larger than %d bytes", limit)
	}
	return path, file.Close()
}

// writeAtomically decrypts into a temporary file beside target and renames it
// into place, so a failed or damaged delivery never leaves partial output.
func writeAtomically(target string, unlocked *envelope.Unlocked) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".tegami-*")
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	complete := false
	defer func() {
		_ = temporary.Close()
		if !complete {
			_ = os.Remove(temporary.Name())
		}
	}()
	if _, err := unlocked.WriteTo(temporary); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return fmt.Errorf("publish output: %w", err)
	}
	complete = true
	return nil
}

func copyFile(w io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = io.Copy(w, file)
	return err
}
