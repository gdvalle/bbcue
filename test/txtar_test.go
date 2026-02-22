package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestScript(t *testing.T) {
	srv := newTestHTTPServer()
	t.Cleanup(srv.Close)

	p := testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		RequireExplicitExec: true,
		RequireUniqueNames:  true,
		Setup: func(env *testscript.Env) error {
			env.Setenv("BBCUE_HTTP_URL", srv.URL)
			return nil
		},
	}
	testscript.Run(t, p)
}

// newTestHTTPServer returns an httptest.Server with routes used by
// the tool_http_*.txtar integration tests.
func newTestHTTPServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"greeting": "hello"})
	})

	mux.HandleFunc("/hello.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "hello world")
	})

	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		w.Write(body)
	})

	mux.HandleFunc("/headers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Response", "resp-value")
		json.NewEncoder(w).Encode(map[string]string{
			"received": r.Header.Get("X-Custom-Request"),
		})
	})

	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hello.txt", http.StatusFound)
	})

	return httptest.NewServer(mux)
}
