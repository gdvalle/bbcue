package flow

import (
	"context"
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/tools/flow"
)

// bbcuePath is the CUE path to the bbcue output field.
var bbcuePath = cue.ParsePath("bbcue")

// Run executes a CUE flow controller on the given value, running any
// tool tasks (e.g. file.Read) that bbcue depends on.
//
// The flow is configured with Root=bbcue and InferTasks=true. The bbcue
// struct itself is treated as a no-op sentinel task so the flow engine
// traces its dependencies and discovers (only) the tool tasks that bbcue
// references. Tasks not reachable from bbcue are left unresolved.
func Run(ctx context.Context, val cue.Value, dir string) (cue.Value, error) {
	cfg := &flow.Config{
		Root:       bbcuePath,
		InferTasks: true,
	}

	c := flow.New(cfg, val, func(v cue.Value) (flow.Runner, error) {
		return lookupTask(v, dir)
	})
	if err := c.Run(ctx); err != nil {
		return cue.Value{}, fmt.Errorf("flow run: %w", err)
	}

	return c.Value(), nil
}

// lookupTask identifies CUE values that are tool tasks and returns a Runner
// for supported tasks. It also returns a no-op runner for the bbcue struct
// itself, which acts as a sentinel so the flow engine traces its
// dependencies and discovers inferred tasks outside Root.
//
// The task identification mirrors the isTask/taskKey pattern from
// cuelang.org/go/cmd/cue/cmd/custom.go.
func lookupTask(v cue.Value, dir string) (flow.Runner, error) {
	// Treat bbcue as a sentinel task so InferTasks discovers its deps.
	if v.Path().String() == "bbcue" {
		return flow.RunnerFunc(func(*flow.Task) error { return nil }), nil
	}

	kind, ok := taskID(v)
	if !ok {
		return nil, nil
	}

	switch kind {
	case "tool/file.Read":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runFileRead(t, dir)
		}), nil
	case "tool/file.Append":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runFileAppend(t, dir)
		}), nil
	case "tool/file.Create":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runFileCreate(t, dir)
		}), nil
	case "tool/file.Glob":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runFileGlob(t, dir)
		}), nil
	case "tool/file.Mkdir":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runFileMkdir(t, dir)
		}), nil
	case "tool/file.MkdirAll":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runFileMkdir(t, dir)
		}), nil
	case "tool/file.MkdirTemp":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runFileMkdirTemp(t)
		}), nil
	case "tool/file.RemoveAll":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runFileRemoveAll(t, dir)
		}), nil
	case "tool/exec.Run":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return runExec(t, dir)
		}), nil
	case "tool/os.Getenv":
		return flow.RunnerFunc(runGetenv), nil
	case "tool/os.Environ":
		return flow.RunnerFunc(runEnviron), nil
	case "tool/http.Do":
		return flow.RunnerFunc(runHTTPDo), nil
	default:
		return nil, fmt.Errorf("unsupported tool task: %s", kind)
	}
}

// taskID extracts the tool task $id from a CUE value. The value must be a
// struct with a $id field whose string value starts with "tool/", matching
// the convention used by CUE's built-in tool packages.
func taskID(v cue.Value) (string, bool) {
	if v.IncompleteKind() != cue.StructKind {
		return "", false
	}

	id := v.LookupPath(cue.MakePath(cue.Str("$id")))
	if !id.Exists() {
		return "", false
	}

	kind, err := id.String()
	if err != nil {
		return "", false
	}

	if !strings.HasPrefix(kind, "tool/") {
		return "", false
	}
	return kind, true
}
