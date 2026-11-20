package typedb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewClientRejectsNonLoopbackHostAndMissingCredentials(t *testing.T) {
	t.Parallel()

	if _, err := NewClient("http://example.com:8000", "admin", "secret"); err == nil {
		t.Fatal("NewClient accepted a non-loopback host")
	}
	if _, err := NewClient("http://127.0.0.1:8000", "admin", ""); err == nil {
		t.Fatal("NewClient accepted an empty password")
	}
}

func TestQuerySignsInAgainWhenTokenExpires(t *testing.T) {
	t.Parallel()

	var signins, queries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/signin":
			n := signins.Add(1)
			_, _ = w.Write([]byte(`{"token":"token-` + string(rune('0'+n)) + `"}`))
		case "/v1/query":
			queries.Add(1)
			if r.Header.Get("Authorization") != "Bearer token-2" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"AUT3","message":"expired"}`))
				return
			}
			_, _ = w.Write([]byte(`{"queryType":"read","answerType":"conceptDocuments","answers":[{"a":1}]}`))
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Query(context.Background(), "ops", Read, "match $x isa thing;", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Answers) != 1 || signins.Load() != 2 || queries.Load() != 2 {
		t.Fatalf("answers=%d signins=%d queries=%d", len(response.Answers), signins.Load(), queries.Load())
	}
}

func TestQueryHidesTransportDetails(t *testing.T) {
	t.Parallel()

	client, err := NewClient("http://127.0.0.1:1", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), "ops", Read, "match $x isa thing;", false)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error leaks the server address: %v", err)
	}
}
