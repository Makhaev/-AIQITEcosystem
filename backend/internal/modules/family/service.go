package family

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"strings"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

func (s *Service) Home(ctx context.Context, id int64) (Home, error) { return s.Repo.Home(ctx, id) }
func (s *Service) ParentHome(ctx context.Context, id int64, f Filter) (Home, error) {
	return s.Repo.ParentHome(ctx, id, f)
}
func (s *Service) Events(ctx context.Context, id int64, f Filter) (Page[Event], error) {
	return s.Repo.Events(ctx, id, f)
}

func validText(v string, n int) bool {
	return strings.TrimSpace(v) != "" && utf8.RuneCountInString(v) <= n
}
func (s *Service) Students(c context.Context, f Filter) (Page[Student], error) {
	return s.Repo.Students(c, f)
}
func (s *Service) Lessons(c context.Context, id int64, f Filter) (Page[Lesson], error) {
	return s.Repo.Lessons(c, id, f)
}
func (s *Service) Records(c context.Context, id int64, f Filter) (Page[Record], error) {
	return s.Repo.Records(c, id, f)
}
func (s *Service) Record(c context.Context, id int64) (Record, error) { return s.Repo.Record(c, id) }
func (s *Service) Progress(c context.Context, id int64) (map[string]int, error) {
	return s.Repo.Progress(c, id)
}
func (s *Service) Reports(c context.Context, id int64, f Filter, staff bool) (Page[Report], error) {
	return s.Repo.Reports(c, id, f, staff)
}
func (s *Service) Report(c context.Context, id int64) (Report, error) { return s.Repo.Report(c, id) }
func (s *Service) Download(c context.Context, id, fid int64, report bool) (File, error) {
	return s.Repo.Download(c, id, fid, report)
}
func (s *Service) Links(c context.Context, id int64) ([]LinkAccess, error) {
	return s.Repo.Links(c, id)
}
func (s *Service) SetLink(c context.Context, id, pid int64, confirmed bool) error {
	return s.Repo.SetLink(c, id, pid, confirmed)
}
func (s *Service) Share(c context.Context, id int64, in Audience, file bool) error {
	return s.Repo.Share(c, id, in, file)
}
func (s *Service) SaveReport(c context.Context, sid, id int64, in ReportInput) (Report, error) {
	if err := validateReportComposition(in); err != nil {
		return Report{}, err
	}
	if (!in.Generate && (!validText(in.Title, 200) || !validText(in.Body, 20000))) || len(in.FileIDs) > 20 {
		return Report{}, httpx.Invalid("report", "Укажите заголовок до 200 символов, текст до 20000 символов и не более 20 файлов.")
	}
	seen := map[int64]bool{}
	for _, fid := range in.FileIDs {
		if fid < 1 || seen[fid] {
			return Report{}, httpx.Invalid("file_ids", "Некорректные или повторяющиеся файлы.")
		}
		seen[fid] = true
	}
	return s.Repo.SaveReport(c, sid, id, in)
}
func (s *Service) PublishReport(c context.Context, id int64, in Publication) (Report, error) {
	if in.Published == nil {
		return Report{}, httpx.Invalid("published", "Укажите состояние публикации.")
	}
	action := "publish"
	if !*in.Published {
		action = "withdraw"
	}
	return s.ReportAction(c, id, ReportAction{Action: action, Version: in.Version, Revision: in.Revision, Reason: in.Reason})
}
func (s *Service) StaffReport(c context.Context, id int64) (Report, error) {
	return s.Repo.StaffReport(c, id)
}
func (s *Service) ReportQueue(c context.Context, f Filter) (Page[Report], error) {
	return s.Repo.ReportQueue(c, f)
}
func (s *Service) ReportNotifications(c context.Context, id int64) ([]ReportNotification, error) {
	return s.Repo.ReportNotifications(c, id)
}
func (s *Service) ReportNotificationsPage(ctx context.Context, id int64, page, size int) (Page[ReportNotification], error) {
	return s.Repo.ReportNotificationsPage(ctx, id, page, size)
}
func (s *Service) Requests(c context.Context, id int64, f Filter, staff bool) (Page[Request], error) {
	return s.Repo.Requests(c, id, f, staff)
}
func (s *Service) CreateRequest(c context.Context, id int64, in RequestInput) (Request, error) {
	if err := validateRequestUploads(in.Files); err != nil {
		return Request{}, err
	}
	if !validText(in.Subject, 200) || !validText(in.Body, 5000) {
		return Request{}, httpx.Invalid("request", "Укажите тему до 200 символов и текст до 5000 символов.")
	}
	return s.Repo.CreateRequest(c, id, in)
}
func (s *Service) Reply(c context.Context, id int64, in ReplyInput) error {
	if !validText(in.Reply, 5000) {
		return httpx.Invalid("reply", "Укажите ответ до 5000 символов.")
	}
	return s.Repo.Reply(c, id, in)
}
