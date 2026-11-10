// Package uploads contains bounded multipart reading and authenticated-download headers.
package uploads

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"aiq.local/core/internal/platform/httpx"
)

const MaxFileSize = 10 << 20

type Info struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Size      int       `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

func Read(w http.ResponseWriter, r *http.Request) (string, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxFileSize+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return "", nil, httpx.Invalid("file", "Выберите файл для загрузки.")
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return "", nil, httpx.Invalid("file", "Выберите один файл для загрузки.")
	}
	name := path.Base(strings.ReplaceAll(part.FileName(), `\`, "/"))
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 200 || name == "." || name == ".." || name == "/" || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", nil, httpx.Invalid("file", "Название файла: до 200 символов без управляющих символов.")
	}
	content, err := io.ReadAll(io.LimitReader(part, MaxFileSize+1))
	var tooLarge *http.MaxBytesError
	if len(content) > MaxFileSize || errors.As(err, &tooLarge) {
		return "", nil, httpx.E(413, "file_too_large", "Файл должен быть не больше 10 МБ.")
	}
	if err != nil || len(content) == 0 {
		return "", nil, httpx.Invalid("file", "Файл пуст или не удалось прочитать его содержимое.")
	}
	if _, err = reader.NextPart(); err != io.EOF {
		return "", nil, httpx.Invalid("file", "Загружайте по одному файлу.")
	}
	return name, content, nil
}

func Download(w http.ResponseWriter, name string, content []byte) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
