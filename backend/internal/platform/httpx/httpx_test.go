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

func TestUploadTransportLimitPreservesOrdinaryJSONErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		upload bool
		status int
		code   string
	}{
		{"ordinary JSON", (32 << 10) + 1, false, 400, "invalid_json"},
		{"file transport", (16 << 20) + 1, true, 413, "payload_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"`+strings.Repeat("x", tc.size)+`"}`))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			var in struct {
				Name string `json:"name"`
			}
			var err error
			if tc.upload {
				err = DecodeUpload(w, r, &in)
			} else {
				err = Decode(w, r, &in)
			}
			var failure *Error
			if !errors.As(err, &failure) || failure.Status != tc.status || failure.Code != tc.code {
				t.Fatalf("unexpected body limit contract: %v", err)
			}
		})
	}
}

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
