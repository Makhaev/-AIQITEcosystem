package files

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

func (s *Service) Create(ctx context.Context, in Input) (File, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.StudentID <= 0 || in.Name == "" || utf8.RuneCountInString(in.Name) > 200 || strings.ContainsAny(in.Name, "/\\") || strings.IndexFunc(in.Name, unicode.IsControl) >= 0 {
		return File{}, httpx.Invalid("name", "Укажите ученика и имя файла без пути (до 200 символов).")
	}
	if len(in.Data) == 0 || len(in.Data) > MaxSize {
		return File{}, httpx.Invalid("data", "Размер файла: от 1 байта до 10 МиБ.")
	}
	kind := http.DetectContentType(in.Data)
	if kind == "text/plain; charset=utf-8" && !utf8.Valid(in.Data) {
		return File{}, httpx.Invalid("data", "Текстовый файл должен быть в UTF-8.")
	}
	switch kind {
	case "application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp", "audio/mpeg", "audio/wave", "audio/ogg", "video/mp4", "video/webm", "application/ogg", "text/plain; charset=utf-8":
	default:
		return File{}, httpx.E(415, "unsupported_file_type", "Разрешены PDF, текст UTF-8, PNG/JPEG/GIF/WebP, MP3/WAV/OGG, MP4/WebM.")
	}
	sum := sha256.Sum256(in.Data)
	return s.Repo.Create(ctx, in, kind, hex.EncodeToString(sum[:]))
}
func (s *Service) Get(ctx context.Context, id int64) (File, error) { return s.Repo.Get(ctx, id) }
func (s *Service) List(ctx context.Context, id int64, page int) (Page, error) {
	return s.Repo.List(ctx, id, page)
}
func (s *Service) Grant(ctx context.Context, id int64, in Grant) error {
	if in.UserID <= 0 {
		return httpx.Invalid("user_id", "Укажите сотрудника.")
	}
	return s.Repo.Grant(ctx, id, in)
}
func (s *Service) Revoke(ctx context.Context, id int64) error { return s.Repo.Revoke(ctx, id) }
