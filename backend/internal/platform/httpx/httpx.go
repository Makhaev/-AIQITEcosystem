package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"
)

type Error struct {
	Status    int               `json:"-"`
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"field_errors"`
	RequestID string            `json:"request_id"`
}

func (e *Error) Error() string { return e.Code }
func E(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message, Fields: map[string]string{}}
}
func Invalid(field, message string) *Error {
	e := E(400, "validation_error", "Проверьте введённые данные.")
	e.Fields[field] = message
	return e
}
func Unauthorized() *Error {
	return E(401, "unauthorized", "Необходимо войти в систему.")
}
func Forbidden() *Error { return E(403, "forbidden", "Недостаточно прав.") }

type key int

const (
	requestKey key = iota
	actorKey
)

type Actor struct {
	ID                 int64
	Roles              []string
	MustChangePassword bool
	SessionHash        []byte
}

func (a Actor) Admin() bool {
	for _, r := range a.Roles {
		if r == "admin" {
			return true
		}
	}
	return false
}
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey, a)
}
func CurrentActor(ctx context.Context) Actor { a, _ := ctx.Value(actorKey).(Actor); return a }
func RequestID(ctx context.Context) string   { id, _ := ctx.Value(requestKey).(string); return id }

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	if !errors.As(err, &e) {
		slog.Error("request failed", "request_id", RequestID(r.Context()))
		e = E(500, "internal_error", "Ошибка сервера. Повторите позже.")
	}
	copy := *e
	copy.RequestID = RequestID(r.Context())
	JSON(w, copy.Status, &copy)
}
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	return DecodeLimit(w, r, dst, 32<<10)
}
func DecodeLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		return E(415, "unsupported_media_type", "Ожидается JSON.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return E(400, "invalid_json", "Некорректный JSON или превышен размер запроса.")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return E(400, "invalid_json", "Ожидается один JSON-объект.")
	}
	return nil
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func Observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			http.Error(w, "Service unavailable", 503)
			return
		}
		id := hex.EncodeToString(bytes)
		r = r.WithContext(context.WithValue(r.Context(), requestKey, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		rw := &responseWriter{ResponseWriter: w, status: 200}
		start := time.Now()
		defer func() {
			if recover() != nil {
				Fail(rw, r, errors.New("panic"))
			}
			// Never log bodies, query strings, cookies, emails, or token values.
			slog.Info("http", "request_id", id, "method", r.Method, "route", r.Pattern, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
		}()
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}
