package selfupdate

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunHelpReturnsSuccess(t *testing.T) {
	exitCode, err := Run([]string{"--help"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if exitCode != exitSuccess {
		t.Fatalf("Run returned exit code %d, want %d", exitCode, exitSuccess)
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	exitCode, err := Run([]string{"--unknown"})
	if err == nil {
		t.Fatal("Run returned nil error for unknown flag")
	}
	if exitCode != exitGeneralError {
		t.Fatalf("Run returned exit code %d, want %d", exitCode, exitGeneralError)
	}
}

func TestVerifyChecksum(t *testing.T) {
	sum := sha256.Sum256([]byte("bbcue"))
	expected := fmt.Sprintf("%x  bbcue-linux-amd64", sum)

	if err := VerifyChecksum(sum[:], expected); err != nil {
		t.Fatalf("VerifyChecksum returned error: %v", err)
	}
}

func TestSameReleaseRejectsDirtyBuild(t *testing.T) {
	if SameRelease("f1a0279ca854f4391a58c6984919681092764935", true, "v0.0.0-20260523181604-f1a0279") {
		t.Fatal("SameRelease returned true for a dirty build")
	}
}

func TestCheckWritableAllowsReplacingReadOnlyTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission semantics differ on windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "bbcue")
	if err := os.WriteFile(target, []byte("bbcue"), 0o555); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := CheckWritable(target); err != nil {
		t.Fatalf("CheckWritable returned error for replaceable target: %v", err)
	}
}
