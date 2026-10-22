package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObserveUsesRouteTemplateAndExcludesPrivateData(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /objects/{id}", func(w http.ResponseWriter, r *http.Request) {
		Fail(w, r, errors.New("private-email@example.test private-password"))
	})
	request := httptest.NewRequest("GET", "/objects/private-object?email=private-email@example.test", strings.NewReader("private-body"))
	request.Header.Set("Cookie", "session=private-token")
	response := httptest.NewRecorder()
	Observe(mux).ServeHTTP(response, request)
	var result Error
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 500 || result.Code != "internal_error" || result.Fields == nil || len(result.RequestID) != 32 || response.Header().Get("X-Request-ID") != result.RequestID {
		t.Fatalf("invalid error contract: %+v", result)
	}
	if !strings.Contains(logs.String(), `"route":"GET /objects/{id}"`) {
		t.Fatalf("missing route template: %s", logs.String())
	}
	for _, private := range []string{"private-object", "private-email", "private-password", "private-body", "private-token"} {
		if strings.Contains(logs.String()+response.Body.String(), private) {
			t.Fatalf("private data exposed: %s", private)
		}
	}
}
