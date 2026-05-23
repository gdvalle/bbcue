package selfupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/gdvalle/bbcue/internal/version"
	flag "github.com/spf13/pflag"
)

const usage = `Usage: bbcue self-update [flags]

Update bbcue to the latest GitHub release, or to a specific release.

Flags:
`

const (
	exitSuccess          = 0
	exitGeneralError     = 1
	exitUpdateAvailable  = 1
	exitOperationalError = 2
	exitPermissionDenied = 3
)

// Run executes the self-update subcommand with the given arguments.
// It returns the exit code the caller should use and any error that should
// be printed to stderr.
func Run(args []string) (int, error) {
	fs := flag.NewFlagSet("bbcue self-update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}

	checkOnly := fs.BoolP("check", "c", false, "Only check if an update is available")
	dryRun := fs.BoolP("dry-run", "n", false, "Show what would happen without making changes")
	force := fs.BoolP("force", "f", false, "Skip version check and always download")
	releaseTag := fs.String("release", "", "Download a specific release tag (without v prefix)")
	rollback := fs.Bool("rollback", false, "Restore the previous binary from backup")
	showVersion := fs.BoolP("version", "v", false, "Print version and build information")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return exitSuccess, nil
		}
		return exitGeneralError, err
	}

	if *showVersion {
		return exitSuccess, version.Print(os.Stdout)
	}

	// Validate mutually exclusive flags.
	if *checkOnly && *force {
		return exitGeneralError, fmt.Errorf("--check and --force are mutually exclusive")
	}
	if *dryRun && *rollback {
		return exitGeneralError, fmt.Errorf("--dry-run and --rollback are mutually exclusive")
	}
	if *dryRun && *checkOnly {
		return exitGeneralError, fmt.Errorf("--dry-run and --check are mutually exclusive")
	}
	if *dryRun && *force {
		return exitGeneralError, fmt.Errorf("--dry-run and --force are mutually exclusive")
	}
	if *rollback && *force {
		return exitGeneralError, fmt.Errorf("--rollback and --force are mutually exclusive")
	}
	if *rollback && *checkOnly {
		return exitGeneralError, fmt.Errorf("--rollback and --check are mutually exclusive")
	}
	if *rollback && *releaseTag != "" {
		return exitGeneralError, fmt.Errorf("--rollback and --release are mutually exclusive")
	}

	// --rollback mode.
	if *rollback {
		return runRollback()
	}

	// --check mode (no filesystem access needed).
	if *checkOnly {
		return runCheck(*releaseTag)
	}

	// Resolve target binary path (needed for dry-run and normal update).
	targetPath, err := resolveTarget()
	if err != nil {
		return exitGeneralError, err
	}

	// --dry-run mode.
	if *dryRun {
		return runDryRun(targetPath, *releaseTag)
	}

	// Normal update mode.
	return runUpdate(targetPath, *releaseTag, *force)
}

// resolveTarget returns the absolute, symlink-resolved path to the binary.
func resolveTarget() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine executable path: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}

// runRollback restores the .old backup binary.
func runRollback() (int, error) {
	target, err := resolveTarget()
	if err != nil {
		return exitGeneralError, err
	}

	currentRev, currentModified, _ := CurrentRevision()
	currentDisplay := formatCurrentRev(currentRev, currentModified)

	if err := Rollback(target); err != nil {
		return exitGeneralError, err
	}

	if currentDisplay != "" {
		fmt.Printf("Rolled back from %s. Previous version restored — takes effect on next invocation.\n", currentDisplay)
	} else {
		fmt.Println("Previous version restored — takes effect on next invocation.")
	}
	return exitSuccess, nil
}

// runCheck compares the current version against a release.
// Exits 0 if current, 1 if update available, 2 on error.
func runCheck(releaseTag string) (int, error) {
	currentRev, currentModified, err := CurrentRevision()
	if err != nil {
		return exitOperationalError, fmt.Errorf("cannot determine current version: %s\nUse --force to proceed without version info.", err)
	}
	currentDisplay := formatCurrentRev(currentRev, currentModified)

	var rel *Release
	if releaseTag != "" {
		tag := normalizeTag(releaseTag)
		rel, err = FetchRelease(tag)
	} else {
		rel, err = FetchLatestRelease()
	}
	if err != nil {
		return exitOperationalError, err
	}

	if SameRelease(currentRev, currentModified, rel.TagName) {
		fmt.Printf("%s is the latest release\n", currentDisplay)
		return exitSuccess, nil
	}

	if releaseTag != "" {
		fmt.Printf("Current: %s. Release %s is a different version.\n",
			currentDisplay, rel.TagName)
		return exitSuccess, nil
	}

	fmt.Printf("Current: %s. Latest: %s.\n", currentDisplay, formatRev(ExtractShortSHA(rel.TagName)))
	return exitUpdateAvailable, nil
}

// runDryRun prints what would happen without making changes.
func runDryRun(targetPath string, releaseTag string) (int, error) {
	currentRev, currentModified, err := CurrentRevision()
	var currentDisplay string
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot determine current version: %s\n", err)
	} else {
		currentDisplay = formatCurrentRev(currentRev, currentModified)
	}

	var rel *Release
	if releaseTag != "" {
		tag := normalizeTag(releaseTag)
		rel, err = FetchRelease(tag)
	} else {
		rel, err = FetchLatestRelease()
	}
	if err != nil {
		return exitOperationalError, err
	}

	assetName := CurrentAsset()
	asset := rel.FindAsset(assetName)
	if asset == nil {
		return exitOperationalError, fmt.Errorf("platform %s/%s is not supported by release %s (asset %s not found)",
			runtime.GOOS, runtime.GOARCH, rel.TagName, assetName)
	}

	target := ExtractShortSHA(rel.TagName)
	url := DownloadURL(rel.TagName, assetName)

	fmt.Printf("Current version:  %s\n", currentDisplay)
	fmt.Printf("Target version:   %s  (%s)\n", target, rel.TagName)
	fmt.Printf("Platform asset:   %s\n", assetName)
	fmt.Printf("Download URL:     %s\n", url)
	fmt.Printf("Target path:      %s\n", targetPath)
	fmt.Printf("Would replace:    %s\n", targetPath)
	return exitSuccess, nil
}

// runUpdate performs the full update flow.
func runUpdate(targetPath, releaseTag string, force bool) (int, error) {
	// Permission pre-check.
	if err := CheckWritable(targetPath); err != nil {
		return exitPermissionDenied, err
	}

	// Resolve current version.
	currentRev, currentModified, err := CurrentRevision()
	if err != nil && !force {
		return exitOperationalError, fmt.Errorf("warning: cannot determine current version: %s\nUse --force to proceed without version info.", err)
	}
	currentDisplay := formatCurrentRev(currentRev, currentModified)

	// Fetch release info from GitHub.
	var rel *Release
	if releaseTag != "" {
		tag := normalizeTag(releaseTag)
		rel, err = FetchRelease(tag)
	} else {
		rel, err = FetchLatestRelease()
	}
	if err != nil {
		return exitOperationalError, err
	}

	// Find the platform asset.
	assetName := CurrentAsset()
	asset := rel.FindAsset(assetName)
	if asset == nil {
		return exitOperationalError, fmt.Errorf("platform %s/%s is not supported by release %s (asset %s not found)",
			runtime.GOOS, runtime.GOARCH, rel.TagName, assetName)
	}

	// Version comparison (unless --force).
	if !force && currentRev != "" {
		if SameRelease(currentRev, currentModified, rel.TagName) {
			fmt.Printf("%s is already the latest release\n", currentDisplay)
			return exitSuccess, nil
		}
		if releaseTag != "" {
			// User specified a release; may be a downgrade.
			fmt.Fprintf(os.Stderr, "Current: %s. Release %s is a different version.\n", currentDisplay, rel.TagName)
			if !confirmDowngrade() {
				return exitSuccess, nil
			}
		}
	}

	// Acquire advisory lock.
	lockFile, err := Lock(targetPath)
	if err != nil {
		return exitGeneralError, err
	}
	defer Unlock(lockFile, targetPath)

	// Set up paths.
	_, oldPath, newPath := Paths(targetPath)
	downloadURL := DownloadURL(rel.TagName, assetName)

	// Create cancellable context for download.
	ctx, cancelDownload := context.WithCancel(context.Background())
	defer cancelDownload()

	// Track cleanup state for signal handling.
	var cleanupMu sync.Mutex
	cleanup := func() {
		cancelDownload()
		os.Remove(newPath)
		Unlock(lockFile, targetPath)
	}

	stopSignals := WithSignalHandling(func() {
		cleanupMu.Lock()
		defer cleanupMu.Unlock()
		cleanup()
	})
	defer stopSignals()

	fmt.Printf("Downloading %s (%s)…\n", assetName, rel.TagName)

	// Download to temp file.
	if err := DownloadBinary(ctx, downloadURL, asset.Size, newPath); err != nil {
		return exitGeneralError, fmt.Errorf("download failed: %w", err)
	}

	// Download checksum (best-effort).
	checksum, checksumErr := DownloadChecksum(ctx, rel.TagName, assetName)
	if checksumErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not verify download integrity: %s\n", checksumErr)
	} else if checksum == "" {
		fmt.Fprintln(os.Stderr, "warning: no checksum available for integrity verification")
	}

	// Verify the downloaded binary.
	if err := VerifyBinary(newPath, checksum); err != nil {
		os.Remove(newPath)
		return exitGeneralError, fmt.Errorf("verification failed: %w", err)
	}

	// Update cleanup: from here until Install, restore from backup on signal.
	cleanupMu.Lock()
	cleanup = func() {
		// Restore the backup if we've already renamed the original.
		if _, err := os.Stat(oldPath); err == nil {
			os.Rename(oldPath, targetPath)
		}
		os.Remove(newPath)
		Unlock(lockFile, targetPath)
	}
	cleanupMu.Unlock()

	// Backup current binary (point of no return).
	if err := Backup(targetPath); err != nil {
		os.Remove(newPath)
		return exitGeneralError, err
	}

	// Install new binary.
	if err := Install(newPath, targetPath); err != nil {
		// Try to restore from backup.
		if _, statErr := os.Stat(oldPath); statErr == nil {
			os.Rename(oldPath, targetPath)
		}
		return exitGeneralError, fmt.Errorf("installation failed (backup restored): %w", err)
	}

	// Only now drop the restore logic — Install succeeded.
	cleanupMu.Lock()
	cleanup = func() {
		Unlock(lockFile, targetPath)
	}
	cleanupMu.Unlock()

	// Success.
	targetSHA := ExtractShortSHA(rel.TagName)
	if currentDisplay != "" {
		fmt.Printf("bbcue updated: %s → %s\n", currentDisplay, targetSHA)
	} else {
		fmt.Printf("bbcue updated to %s\n", targetSHA)
	}
	Unlock(lockFile, targetPath)
	return exitSuccess, nil
}

// normalizeTag strips a leading "v" and re-adds it.
func normalizeTag(tag string) string {
	if tag == "" {
		return ""
	}
	// Strip leading "v" if present.
	if tag[0] == 'v' {
		tag = tag[1:]
	}
	return "v" + tag
}

// formatRev returns a display-friendly short revision string.
func formatRev(rev string) string {
	if rev == "" {
		return ""
	}
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

func formatCurrentRev(rev string, modified bool) string {
	display := formatRev(rev)
	if display != "" && modified {
		display += "+dirty"
	}
	return display
}

// confirmDowngrade prompts the user to confirm a potential downgrade.
// Returns true if the user confirms (answers "y" or "yes").
func confirmDowngrade() bool {
	fmt.Fprint(os.Stderr, "Proceed with downgrade? [y/N] ")
	var answer string
	fmt.Scanln(&answer)
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}
