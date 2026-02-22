package flow

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	cuflow "cuelang.org/go/tools/flow"
)

// runExec implements tool/exec.Run.
// Mirrors cuelang.org/go/pkg/tool/exec.execCmd.Run.
func runExec(t *cuflow.Task, dir string) error {
	v := t.Value()

	cmd, err := buildCommand(t.Context(), v, dir)
	if err != nil {
		return fmt.Errorf("exec.Run: %w", err)
	}

	update := map[string]interface{}{}

	// Capture stdout if the field is constrained to string/bytes (non-null).
	captureOut := false
	if sv := v.LookupPath(cue.ParsePath("stdout")); sv.Exists() && !sv.IsNull() {
		captureOut = true
	}
	captureErr := false
	if sv := v.LookupPath(cue.ParsePath("stderr")); sv.Exists() && !sv.IsNull() {
		captureErr = true
	}

	// Wire stdin if specified.
	if sv := v.LookupPath(cue.ParsePath("stdin")); sv.Exists() && !sv.IsNull() {
		s, err := sv.String()
		if err == nil {
			cmd.Stdin = strings.NewReader(s)
		}
	}

	if captureOut {
		var stdout []byte
		stdout, err = cmd.Output()
		update["stdout"] = string(stdout)
	} else {
		err = cmd.Run()
	}
	update["success"] = err == nil

	if err != nil && captureErr {
		if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
			update["stderr"] = string(exit.Stderr)
		} else {
			update["stderr"] = err.Error()
		}
	}

	mustSucceed := true
	if ms := v.LookupPath(cue.ParsePath("mustSucceed")); ms.Exists() {
		if b, bErr := ms.Bool(); bErr == nil {
			mustSucceed = b
		}
	}

	if err != nil && mustSucceed {
		return fmt.Errorf("exec.Run: command failed: %w", err)
	}

	return t.Fill(update)
}

// buildCommand constructs an exec.Cmd from a CUE task value.
func buildCommand(ctx context.Context, v cue.Value, workDir string) (*exec.Cmd, error) {
	cmdVal := v.LookupPath(cue.ParsePath("cmd"))
	if !cmdVal.Exists() {
		return nil, fmt.Errorf("missing cmd field")
	}

	var bin string
	var args []string

	switch cmdVal.IncompleteKind() {
	case cue.StringKind:
		s, err := cmdVal.String()
		if err != nil {
			return nil, err
		}
		parts := strings.Fields(s)
		if len(parts) == 0 {
			return nil, fmt.Errorf("empty command")
		}
		bin, args = parts[0], parts[1:]

	case cue.ListKind:
		iter, err := cmdVal.List()
		if err != nil {
			return nil, err
		}
		for iter.Next() {
			s, err := iter.Value().String()
			if err != nil {
				return nil, err
			}
			if bin == "" {
				bin = s
			} else {
				args = append(args, s)
			}
		}
		if bin == "" {
			return nil, fmt.Errorf("empty command list")
		}

	default:
		return nil, fmt.Errorf("cmd must be a string or list")
	}

	cmd := exec.CommandContext(ctx, bin, args...)

	// Default to the provided workDir (the bb.cue directory).
	cmd.Dir = workDir

	// If the task specifies a directory, resolve it relative to workDir.
	if explicitDir, err := v.LookupPath(cue.ParsePath("dir")).String(); err == nil {
		if filepath.IsAbs(explicitDir) {
			cmd.Dir = explicitDir
		} else {
			cmd.Dir = filepath.Join(workDir, explicitDir)
		}
	}

	// Environment variables (struct form: {KEY: "value"}).
	env := v.LookupPath(cue.ParsePath("env"))
	if env.Exists() {
		for iter, _ := env.Fields(); iter.Next(); {
			key := iter.Selector().Unquoted()
			s, err := iter.Value().String()
			if err != nil {
				continue
			}
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, s))
		}
	}

	return cmd, nil
}
