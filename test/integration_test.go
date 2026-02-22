package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testBinDir is the directory containing the compiled bbcue binary.
// It is prepended to PATH so that testscript and other tests can
// invoke "bbcue" directly.
var testBinDir string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "bbcue-test-bin")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	testBinDir = tmp
	binPath := filepath.Join(tmp, "bbcue")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/bbcue")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Dir = findModuleRoot()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("failed to build bbcue: " + err.Error())
	}

	// Prepend the binary directory to PATH so bbcue is found by testscript exec.
	os.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	os.Exit(m.Run())
}

func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find go.mod")
		}
		dir = parent
	}
}

func TestVersion(t *testing.T) {
	binPath := filepath.Join(testBinDir, "bbcue")
	cmd := exec.Command(binPath, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bbcue --version failed: %v\n%s", err, out)
	}

	output := string(out)

	// Must contain the binary name and version header.
	if !strings.Contains(output, "bbcue version") {
		t.Errorf("expected 'bbcue version' in output, got:\n%s", output)
	}

	// Must report the CUE library version.
	if !strings.Contains(output, "cue library") {
		t.Errorf("expected 'cue library' in output, got:\n%s", output)
	}

	// Must report Go version.
	if !strings.Contains(output, "go") {
		t.Errorf("expected go version in output, got:\n%s", output)
	}

	// Must report GOOS and GOARCH.
	if !strings.Contains(output, "GOOS") {
		t.Errorf("expected GOOS in output, got:\n%s", output)
	}
	if !strings.Contains(output, "GOARCH") {
		t.Errorf("expected GOARCH in output, got:\n%s", output)
	}
}

func TestReadmeFresh(t *testing.T) {
	root := findModuleRoot()

	// Read the current README.md before regenerating.
	readmePath := filepath.Join(root, "README.md")
	before, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}

	// Run the generator to produce a fresh README.
	cmd := exec.Command("go", "run", "./internal/genreadme")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go run ./internal/genreadme failed: %v", err)
	}

	// Compare the freshly generated README with what was on disk before.
	after, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading regenerated README.md: %v", err)
	}

	if !bytes.Equal(before, after) {
		t.Error("README.md is out of date; run 'go run ./internal/genreadme' and commit the result")
	}
}
