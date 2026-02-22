package flow

import (
	"fmt"
	"os"
	"strings"

	cuflow "cuelang.org/go/tools/flow"
)

// runGetenv implements tool/os.Getenv.
// Mirrors cuelang.org/go/pkg/tool/os.getenvCmd.Run.
// Each field (except $id) names an environment variable to look up.
// Missing variables are filled with null.
func runGetenv(t *cuflow.Task) error {
	v := t.Value()

	iter, err := v.Fields()
	if err != nil {
		return fmt.Errorf("os.Getenv: %w", err)
	}

	update := map[string]interface{}{}
	for iter.Next() {
		name := iter.Selector().Unquoted()
		if strings.HasPrefix(name, "$") {
			continue
		}
		str, ok := os.LookupEnv(name)
		if !ok {
			update[name] = nil
			continue
		}
		update[name] = str
	}

	return t.Fill(update)
}

// runEnviron implements tool/os.Environ.
// Mirrors cuelang.org/go/pkg/tool/os.environCmd.Run.
// Returns all environment variables as string fields. Fields declared
// in the task value constrain the expected variables; missing ones are null.
func runEnviron(t *cuflow.Task) error {
	v := t.Value()

	update := map[string]interface{}{}

	// Populate from the actual environment.
	for _, kv := range os.Environ() {
		name, val, _ := strings.Cut(kv, "=")
		update[name] = val
	}

	// For any declared fields not in the environment, set null.
	iter, err := v.Fields()
	if err != nil {
		return fmt.Errorf("os.Environ: %w", err)
	}
	for iter.Next() {
		name := iter.Selector().Unquoted()
		if strings.HasPrefix(name, "$") {
			continue
		}
		if _, ok := update[name]; !ok {
			update[name] = nil
		}
	}

	return t.Fill(update)
}
