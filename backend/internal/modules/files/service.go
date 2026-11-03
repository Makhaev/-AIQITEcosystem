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
	kind, hash, err := Validate(&in)
	if err != nil {
		return File{}, err
	}
	return s.Repo.Create(ctx, in, kind, hash)
}

// Validate is shared by staff uploads and atomic student submissions.
func Validate(in *Input) (string, string, error) {
	if in.StudentID <= 0 {
		return "", "", httpx.Invalid("student_id", "Укажите ученика.")
	}
	return ValidateContent(in)
}

// ValidateContent also validates private correspondence attachments without a pupil.
func ValidateContent(in *Input) (string, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 200 || strings.ContainsAny(in.Name, "/\\") || strings.IndexFunc(in.Name, unicode.IsControl) >= 0 {
		return "", "", httpx.Invalid("name", "Укажите ученика и имя файла без пути (до 200 символов).")
	}
	if len(in.Data) == 0 || len(in.Data) > MaxSize {
		return "", "", httpx.Invalid("data", "Размер файла: от 1 байта до 10 МиБ.")
	}
	kind := http.DetectContentType(in.Data)
	if kind == "text/plain; charset=utf-8" && !utf8.Valid(in.Data) {
		return "", "", httpx.Invalid("data", "Текстовый файл должен быть в UTF-8.")
	}
	switch kind {
	case "application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp", "audio/mpeg", "audio/wave", "audio/ogg", "video/mp4", "video/webm", "application/ogg", "text/plain; charset=utf-8":
	default:
		return "", "", httpx.E(415, "unsupported_file_type", "Разрешены PDF, текст UTF-8, PNG/JPEG/GIF/WebP, MP3/WAV/OGG, MP4/WebM.")
	}
	sum := sha256.Sum256(in.Data)
	return kind, hex.EncodeToString(sum[:]), nil
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
