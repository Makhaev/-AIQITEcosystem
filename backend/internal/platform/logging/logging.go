// Package logging keeps runtime diagnostics structured and excludes raw errors,
// which can contain connection strings, SQL values or personal data.
package logging

import (
	"context"
	"errors"
	"log/slog"
	"regexp"

	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5/pgconn"
)

var sqlState = regexp.MustCompile(`^[0-9A-Z]{5}$`)

func Failure(message string, err error) {
	attributes := []any{"kind", "internal_error"}
	var pg *pgconn.PgError
	var app *httpx.Error
	switch {
	case errors.As(err, &pg):
		attributes[1] = "database_error"
		if sqlState.MatchString(pg.Code) {
			attributes = append(attributes, "sqlstate", pg.Code)
		}
	case errors.As(err, &app):
		attributes[1] = "request_rejected"
		attributes = append(attributes, "status", app.Status)
	case errors.Is(err, context.DeadlineExceeded):
		attributes[1] = "timeout"
	}
	slog.Error(message, attributes...)
}

// HTTPErrorWriter replaces net/http's unstructured transport logs. The supplied
// text may contain addresses or TLS details and must never be forwarded.
type HTTPErrorWriter struct{}

func (HTTPErrorWriter) Write(p []byte) (int, error) {
	slog.Warn("http transport failure", "kind", "transport_error")
	return len(p), nil
}
