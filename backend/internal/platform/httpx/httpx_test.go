package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObserveLogsRouteWithoutSecrets(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /files/{id}", func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Error("missing request ID in handler")
		}
		w.WriteHeader(http.StatusForbidden)
	})
	req := httptest.NewRequest("GET", "/files/private-file-name?token=private-token", nil)
	req.Header.Set("Cookie", "session=private-cookie")
	w := httptest.NewRecorder()
	Observe(mux).ServeHTTP(w, req)
	var entry struct {
		Route     string `json:"route"`
		Status    int    `json:"status"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Route != "GET /files/{id}" || entry.Status != 403 || entry.RequestID != w.Header().Get("X-Request-ID") || entry.RequestID == "" {
		t.Fatalf("incorrect request log: %+v", entry)
	}
	for _, secret := range []string{"private-file-name", "private-token", "private-cookie"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("request log leaked %s", secret)
		}
	}
}
