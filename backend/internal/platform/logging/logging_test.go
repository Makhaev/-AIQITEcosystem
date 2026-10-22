package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDiagnosticsExcludeRawErrorsAndTransportData(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	private := "postgres://person@example.test:secret-password@private-host/db?token=private-token"
	Failure("startup failed", fmt.Errorf("migration failed: %w", &pgconn.PgError{Code: "23505", Message: private, Detail: private, ConstraintName: private}))
	Failure("startup failed", errors.New(private))
	if n, err := (HTTPErrorWriter{}).Write([]byte(private)); err != nil || n != len(private) {
		t.Fatalf("transport writer: %d %v", n, err)
	}
	for i, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"person@example.test", "secret-password", "private-host", "private-token"} {
			if strings.Contains(line, secret) {
				t.Fatalf("private data in record %d", i)
			}
		}
		if i == 0 && (record["sqlstate"] != "23505" || record["kind"] != "database_error") {
			t.Fatalf("missing safe database diagnostic: %v", record)
		}
	}
}
