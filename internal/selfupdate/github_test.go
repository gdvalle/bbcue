package selfupdate

import (
	"net/http"
	"testing"
)

func TestHTTPClientsUseSeparateTimeoutPolicies(t *testing.T) {
	if apiHTTPClient.Timeout != apiRequestTimeout {
		t.Fatalf("apiHTTPClient.Timeout = %v, want %v", apiHTTPClient.Timeout, apiRequestTimeout)
	}
	if downloadHTTPClient.Timeout != 0 {
		t.Fatalf("downloadHTTPClient.Timeout = %v, want 0", downloadHTTPClient.Timeout)
	}

	apiTransport, ok := apiHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("apiHTTPClient transport type = %T, want *http.Transport", apiHTTPClient.Transport)
	}
	downloadTransport, ok := downloadHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("downloadHTTPClient transport type = %T, want *http.Transport", downloadHTTPClient.Transport)
	}

	if apiTransport.TLSHandshakeTimeout != connectTimeout {
		t.Fatalf("api transport TLSHandshakeTimeout = %v, want %v", apiTransport.TLSHandshakeTimeout, connectTimeout)
	}
	if apiTransport.ResponseHeaderTimeout != responseHeaderTimeout {
		t.Fatalf("api transport ResponseHeaderTimeout = %v, want %v", apiTransport.ResponseHeaderTimeout, responseHeaderTimeout)
	}
	if downloadTransport.TLSHandshakeTimeout != connectTimeout {
		t.Fatalf("download transport TLSHandshakeTimeout = %v, want %v", downloadTransport.TLSHandshakeTimeout, connectTimeout)
	}
	if downloadTransport.ResponseHeaderTimeout != responseHeaderTimeout {
		t.Fatalf("download transport ResponseHeaderTimeout = %v, want %v", downloadTransport.ResponseHeaderTimeout, responseHeaderTimeout)
	}
}
