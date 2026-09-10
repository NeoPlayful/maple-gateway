package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeResponder struct {
	body string
	ok   bool
}

func (f fakeResponder) RespondPath(string) (string, bool) { return f.body, f.ok }

func TestChallengeHandlerIntercepts(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("proxied"))
	})
	h := challengeHandler{next: next, cr: fakeResponder{body: "keyauth", ok: true}}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "keyauth" {
		t.Fatalf("body = %q, want keyauth", rec.Body.String())
	}
}

func TestChallengeHandlerFallsThrough(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("proxied"))
	})
	h := challengeHandler{next: next, cr: fakeResponder{ok: false}}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/real", nil))
	if rec.Code != http.StatusTeapot || rec.Body.String() != "proxied" {
		t.Fatalf("non-challenge path should pass through: %d %q", rec.Code, rec.Body.String())
	}
}
