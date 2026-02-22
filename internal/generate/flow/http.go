package flow

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"

	"cuelang.org/go/cue"
	cuflow "cuelang.org/go/tools/flow"
)

// runHTTPDo implements tool/http.Do.
// Mirrors cuelang.org/go/pkg/tool/http.httpCmd.Run.
func runHTTPDo(t *cuflow.Task) error {
	v := t.Value()

	req, err := buildHTTPRequest(v)
	if err != nil {
		return fmt.Errorf("http.Do: %w", err)
	}

	client, err := buildHTTPClient(v)
	if err != nil {
		return fmt.Errorf("http.Do: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http.Do: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("http.Do: reading response body: %w", err)
	}

	return t.Fill(map[string]interface{}{
		"response": map[string]interface{}{
			"status":     resp.Status,
			"statusCode": resp.StatusCode,
			"body":       string(b),
			"header":     resp.Header,
			"trailer":    resp.Trailer,
		},
	})
}

// buildHTTPRequest constructs an http.Request from a CUE task value,
// reading method, url, request.body, request.header, and request.trailer.
func buildHTTPRequest(v cue.Value) (*http.Request, error) {
	method, err := v.LookupPath(cue.ParsePath("method")).String()
	if err != nil {
		return nil, fmt.Errorf("getting method: %w", err)
	}

	u, err := v.LookupPath(cue.ParsePath("url")).String()
	if err != nil {
		return nil, fmt.Errorf("getting url: %w", err)
	}

	var body io.Reader
	var header, trailer http.Header

	if obj := v.LookupPath(cue.MakePath(cue.Str("request"))); obj.Exists() {
		if bv := obj.LookupPath(cue.MakePath(cue.Str("body"))); bv.Exists() {
			body, err = bv.Reader()
			if err != nil {
				return nil, fmt.Errorf("reading request body: %w", err)
			}
		}
		if header, err = parseHTTPHeaders(obj, "header"); err != nil {
			return nil, err
		}
		if trailer, err = parseHTTPHeaders(obj, "trailer"); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	if header != nil {
		req.Header = header
	}
	req.Trailer = trailer
	return req, nil
}

// buildHTTPClient constructs an http.Client from a CUE task value,
// reading tls.verify, tls.caCert, and followRedirects.
func buildHTTPClient(v cue.Value) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{}

	if err := configureTLS(v, transport); err != nil {
		return nil, err
	}

	client := &http.Client{Transport: transport}

	if err := configureRedirects(v, client); err != nil {
		return nil, err
	}

	return client, nil
}

// configureTLS reads tls.verify and tls.caCert from the CUE value and
// configures the transport accordingly.
func configureTLS(v cue.Value, transport *http.Transport) error {
	if tv := v.LookupPath(cue.MakePath(cue.Str("tls"), cue.Str("verify"))); tv.Exists() {
		verify, err := tv.Bool()
		if err != nil {
			return fmt.Errorf("invalid tls.verify: %w", err)
		}
		if !verify {
			transport.TLSClientConfig.InsecureSkipVerify = true
		}
	}

	caCertVal := v.LookupPath(cue.MakePath(cue.Str("tls"), cue.Str("caCert")))
	if !caCertVal.Exists() {
		return nil
	}

	caCert, err := caCertVal.Bytes()
	if err != nil {
		return fmt.Errorf("invalid tls.caCert: %w", err)
	}

	pool := x509.NewCertPool()
	for {
		block, rest := pem.Decode(caCert)
		if block == nil {
			break
		}
		if block.Type == "PUBLIC KEY" {
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return fmt.Errorf("failed to parse caCert: %w", err)
			}
			pool.AddCert(c)
		}
		caCert = rest
	}
	transport.TLSClientConfig.RootCAs = pool
	return nil
}

// configureRedirects reads followRedirects from the CUE value and
// configures the client to either follow or not follow redirects.
func configureRedirects(v cue.Value, client *http.Client) error {
	followRedirects := true
	if fr := v.LookupPath(cue.MakePath(cue.Str("followRedirects"))); fr.Exists() {
		b, err := fr.Bool()
		if err != nil {
			return fmt.Errorf("invalid followRedirects: %w", err)
		}
		followRedirects = b
	}

	if !followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return nil
}

// parseHTTPHeaders reads an HTTP header struct from a CUE value.
// Mirrors cuelang.org/go/pkg/tool/http.parseHeaders.
// Supports both single string values and [string, ...string] list values.
func parseHTTPHeaders(obj cue.Value, label string) (http.Header, error) {
	m := obj.LookupPath(cue.MakePath(cue.Str(label)))
	if !m.Exists() {
		return nil, nil
	}

	iter, err := m.Fields()
	if err != nil {
		return nil, err
	}

	h := http.Header{}
	for iter.Next() {
		key := iter.Selector().Unquoted()
		val := iter.Value()

		// Handle single string value.
		if s, err := val.String(); err == nil {
			h.Add(key, s)
			continue
		}

		// Each header value is a list of strings [string, ...string].
		list, err := val.List()
		if err != nil {
			return nil, err
		}
		for list.Next() {
			str, err := list.Value().String()
			if err != nil {
				return nil, err
			}
			h.Add(key, str)
		}
	}
	return h, nil
}
