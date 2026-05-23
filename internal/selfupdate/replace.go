package selfupdate

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// lockSuffix is appended to the target path for the advisory lock file.
const lockSuffix = ".selfupdate.lock"

// oldSuffix is appended for the backup of the previous binary.
const oldSuffix = ".selfupdate.old"

// newSuffix is appended for the temp download file.
const newSuffix = ".selfupdate.new"

// Paths returns the lock, backup, and temp paths derived from the target binary path.
func Paths(target string) (lockPath, oldPath, newPath string) {
	return target + lockSuffix, target + oldSuffix, target + newSuffix
}

// Lock creates an advisory lock file for the given target path.
// Returns the open file handle and any error. If the lock file already
// exists, another self-update is in progress.
func Lock(target string) (*os.File, error) {
	lockPath, _, _ := Paths(target)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another self-update is already in progress (lock file %s exists)", lockPath)
		}
		return nil, err
	}
	return f, nil
}

// Unlock closes and removes the advisory lock file.
func Unlock(f *os.File, target string) {
	if f != nil {
		lockPath, _, _ := Paths(target)
		f.Close()
		os.Remove(lockPath)
	}
}

// CheckWritable verifies the target binary can be replaced by the current
// process. Because the update path uses rename operations, the relevant check
// is whether the containing directory is writable, not whether the running
// binary itself can be opened for writing.
func CheckWritable(target string) error {
	return checkDirWritable(filepath.Dir(target))
}

// checkDirWritable checks if we can create files in the given directory.
func checkDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".bbcue-write-test-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf(
				"cannot write to %s: permission denied\n"+
					"(try running with sudo)", dir)
		}
		return err
	}
	testFile := f.Name()
	f.Close()
	os.Remove(testFile)
	return nil
}

// Backup renames the target binary to .old, creating a rollback point.
func Backup(target string) error {
	_, oldPath, _ := Paths(target)

	// Remove existing .old backup if present.
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("removing old backup %s: %w", oldPath, err)
		}
	}

	if err := os.Rename(target, oldPath); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf(
				"cannot rename %s: permission denied\n"+
					"(try running with sudo)", target)
		}
		return err
	}
	return nil
}

// Install renames the temp .new binary to the target path (atomic).
func Install(newPath, target string) error {
	if err := os.Rename(newPath, target); err != nil {
		return fmt.Errorf("installing new binary: %w", err)
	}
	return nil
}

// Rollback restores the .old backup to the target path.
func Rollback(target string) error {
	_, oldPath, _ := Paths(target)

	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return fmt.Errorf("no rollback backup found at %s", oldPath)
	}

	if err := os.Rename(oldPath, target); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf(
				"cannot restore backup: permission denied\n" +
					"(try running with sudo)")
		}
		return err
	}
	return nil
}

// VerifyBinary performs post-download verification on the downloaded binary
// with a single read pass and optional SHA-256 checksum verification.
func VerifyBinary(path, checksum string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reading downloaded binary: %w", err)
	}
	defer f.Close()

	if checksum != "" {
		hasher := sha256.New()
		if _, err := io.Copy(hasher, f); err != nil {
			return fmt.Errorf("reading downloaded binary: %w", err)
		}
		if err := VerifyChecksum(hasher.Sum(nil), checksum); err != nil {
			return fmt.Errorf("integrity check failed: %w\n"+
				"This may indicate a corrupt download or a tampered release.\n"+
				"Try running the update again.", err)
		}
		return nil
	}

	if _, err := io.Copy(io.Discard, f); err != nil {
		return fmt.Errorf("reading downloaded binary: %w", err)
	}

	return nil
}

// WithSignalHandling sets up SIGINT/SIGTERM handling. When a signal is
// received, cleanup is called, then the process exits with the appropriate
// code (130 for SIGINT, 143 for SIGTERM). Returns a stop function to remove
// signal handlers when done.
func WithSignalHandling(cleanup func()) (stop func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		select {
		case sig := <-sigCh:
			cleanup()
			code := 130
			if sig == syscall.SIGTERM {
				code = 143
			}
			os.Exit(code)
		case <-done:
		}
	}()

	return func() {
		signal.Stop(sigCh)
		close(done)
	}
}
