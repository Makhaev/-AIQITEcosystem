package family

import (
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/notify"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"unicode/utf8"
)

func requestResource(staff bool) access.Resource {
	if staff {
		return access.FamilyRequests
	}
	return access.Family
}

const requestColumns = `q.id,q.student_id,COALESCE(s.name,''),q.created_by,u.name,q.subject,q.body,q.reply,q.revision,q.assignee_id,COALESCE(au.name,''),q.status,q.created_at,q.answered_at,q.closed_at`
const requestJoins = ` FROM family_requests q JOIN users u ON u.id=q.created_by LEFT JOIN students s ON s.id=q.student_id LEFT JOIN users au ON au.id=q.assignee_id`

func scanRequest(row pgx.Row) (Request, error) {
	var v Request
	err := row.Scan(&v.ID, &v.StudentID, &v.StudentName, &v.AuthorID, &v.AuthorName, &v.Subject, &v.Body, &v.Reply, &v.Revision, &v.AssigneeID, &v.AssigneeName, &v.Status, &v.CreatedAt, &v.AnsweredAt, &v.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = missing()
	}
	return v, err
}
func requestAccess(ctx context.Context, tx pgx.Tx, a httpx.Actor, v Request, staff bool) error {
	if staff {
		if a.Admin() {
			return nil
		}
		if v.StudentID != nil {
			return staffStudent(ctx, tx, a, *v.StudentID)
		}
		return missing()
	}
	if v.AuthorID != a.ID {
		return missing()
	}
	if v.StudentID != nil {
		_, _, err := audience(ctx, tx, a, *v.StudentID)
		return err
	}
	return generalRequestAuthor(ctx, tx, a)
}
func generalRequestAuthor(ctx context.Context, tx pgx.Tx, a httpx.Actor) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT $2 AND EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id WHERE pa.user_id=$1 AND p.status='active')`, a.ID, has(a, "parent")).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return missing()
	}
	return nil
}
func requestAssignee(ctx context.Context, tx pgx.Tx, sid *int64, uid *int64) (bool, error) {
	if uid == nil {
		return false, nil
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=$1 AND NOT u.blocked AND EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=u.id AND (ur.role='admin' OR (ur.role='curator' AND EXISTS(SELECT 1 FROM students WHERE id=$2 AND curator_has_student(u.id,id))))))`, *uid, sid).Scan(&ok)
	return ok, err
}
func requestActions(a httpx.Actor, v Request, staff bool) []string {
	actions := []string{}
	responsible := staff && (a.Admin() || v.AssigneeID != nil && *v.AssigneeID == a.ID)
	if v.Status == "closed" {
		if !staff || responsible {
			actions = append(actions, "reopen")
		}
		return actions
	}
	if staff {
		actions = append(actions, "assign")
	}
	if !staff || responsible {
		actions = append(actions, "message")
	}
	if responsible && v.Status == "assigned" {
		actions = append(actions, "start")
	}
	if responsible && v.Status == "in_progress" {
		actions = append(actions, "answer")
	}
	if v.Status == "answered" && (!staff || responsible) {
		actions = append(actions, "close")
	}
	return actions
}
func requestRead(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64, staff bool, detail bool) (Request, error) {
	v, err := scanRequest(tx.QueryRow(ctx, `SELECT `+requestColumns+requestJoins+` WHERE q.id=$1`, id))
	if err != nil {
		return v, err
	}
	if err = requestAccess(ctx, tx, a, v, staff); err != nil {
		return Request{}, err
	}
	v.Actions = requestActions(a, v, staff)
	if !detail {
		return v, nil
	}
	v.Messages = []RequestMessage{}
	v.Events = []RequestEvent{}
	rows, err := tx.Query(ctx, `SELECT m.id,m.author_id,COALESCE(u.name,''),m.body,m.kind,m.created_at FROM family_request_messages m LEFT JOIN users u ON u.id=m.author_id WHERE m.request_id=$1 ORDER BY m.id`, id)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var m RequestMessage
		m.Files = []File{}
		if err = rows.Scan(&m.ID, &m.AuthorID, &m.AuthorName, &m.Body, &m.Kind, &m.CreatedAt); err != nil {
			rows.Close()
			return v, err
		}
		v.Messages = append(v.Messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	for i := range v.Messages {
		rows, err = tx.Query(ctx, `SELECT id,name,media_type,octet_length(data) FROM family_request_files WHERE message_id=$1 ORDER BY id`, v.Messages[i].ID)
		if err != nil {
			return v, err
		}
		for rows.Next() {
			var f File
			if err = rows.Scan(&f.ID, &f.Name, &f.MediaType, &f.Size); err != nil {
				rows.Close()
				return v, err
			}
			prefix := "/api/v1/family/requests"
			if staff {
				prefix = "/api/v1/family-requests"
			}
			f.URL = fmt.Sprintf("%s/%d/files/%d", prefix, id, f.ID)
			v.Messages[i].Files = append(v.Messages[i].Files, f)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return v, err
		}
	}
	rows, err = tx.Query(ctx, `SELECT e.action,e.from_status,e.to_status,e.actor_id,COALESCE(u.name,''),e.assignee_id,e.reason,e.created_at FROM family_request_events e LEFT JOIN users u ON u.id=e.actor_id WHERE e.request_id=$1 ORDER BY e.id`, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var e RequestEvent
		if err = rows.Scan(&e.Action, &e.From, &e.To, &e.ActorID, &e.ActorName, &e.AssigneeID, &e.Reason, &e.CreatedAt); err != nil {
			return v, err
		}
		v.Events = append(v.Events, e)
	}
	return v, rows.Err()
}
func (r *Repository) RequestCard(ctx context.Context, id int64, staff bool) (Request, error) {
	tx, a, err := access.Begin(ctx, r.Pool, requestResource(staff), access.Read, false)
	if err != nil {
		return Request{}, err
	}
	defer tx.Rollback(ctx)
	v, err := requestRead(ctx, tx, a, id, staff, true)
	if err != nil {
		return Request{}, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) Requests(ctx context.Context, sid int64, f Filter, staff bool) (Page[Request], error) {
	out := Page[Request]{Items: []Request{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	tx, a, err := access.Begin(ctx, r.Pool, requestResource(staff), access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if sid > 0 {
		if staff {
			err = staffStudent(ctx, tx, a, sid)
		} else {
			_, _, err = audience(ctx, tx, a, sid)
		}
		if err != nil {
			return out, err
		}
	}
	// The family branch is always author-scoped, including mixed staff/family roles.
	where := ` WHERE ($1::bigint=0 OR q.student_id=$1) AND strpos(lower(q.subject),lower($2))>0 AND ($3='' OR q.status=$3) AND (`
	args := []any{sid, f.Search, f.Status, a.ID}
	if staff {
		where += `($5 OR ($6 AND curator_has_student($4,s.id))))`
		args = append(args, a.Admin(), has(a, "curator"))
	} else {
		where += `q.created_by=$4 AND ((q.student_id IS NULL AND $5 AND EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id WHERE pa.user_id=$4 AND p.status='active')) OR (s.status='active' AND (($6 AND EXISTS(SELECT 1 FROM student_accounts WHERE user_id=$4 AND student_id=s.id)) OR ($5 AND EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id JOIN student_parents sp ON sp.parent_id=p.id JOIN family_access fa ON fa.parent_id=sp.parent_id AND fa.student_id=sp.student_id WHERE pa.user_id=$4 AND sp.student_id=s.id AND p.status='active' AND fa.confirmed))))))`
		args = append(args, has(a, "parent"), has(a, "student"))
	}
	if err = tx.QueryRow(ctx, `SELECT count(*)`+requestJoins+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT `+requestColumns+requestJoins+where+` ORDER BY q.id DESC LIMIT $8 OFFSET $7`, append(args, (f.Page-1)*out.PageSize, out.PageSize)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanRequest(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		v.Actions = requestActions(a, v, staff)
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func validateRequestUploads(uploads []RequestUpload) error {
	if len(uploads) > 3 {
		return httpx.Invalid("files", "Не более трёх файлов в сообщении.")
	}
	total := 0
	for _, u := range uploads {
		total += len(u.Data)
		in := files.Input{Name: u.Name, Data: u.Data}
		if _, _, err := files.ValidateContent(&in); err != nil {
			return err
		}
	}
	if total > 10<<20 {
		return httpx.Invalid("files", "Суммарный размер вложений сообщения — до 10 МиБ.")
	}
	return nil
}
func requestMessage(ctx context.Context, tx pgx.Tx, id, author int64, body, kind string, uploads []RequestUpload) error {
	var mid int64
	if err := tx.QueryRow(ctx, `INSERT INTO family_request_messages(request_id,author_id,body,kind,created_at) VALUES($1,$2,$3,$4,clock_timestamp()) RETURNING id`, id, author, body, kind).Scan(&mid); err != nil {
		return err
	}
	for _, u := range uploads {
		in := files.Input{Name: u.Name, Data: u.Data}
		media, _, err := files.ValidateContent(&in)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO family_request_files(message_id,name,media_type,data) VALUES($1,$2,$3,$4)`, mid, in.Name, media, in.Data); err != nil {
			return err
		}
	}
	return nil
}
func requestEvent(ctx context.Context, tx pgx.Tx, a httpx.Actor, v Request, action, from, reason string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO family_request_events(request_id,action,from_status,to_status,actor_id,assignee_id,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp())`, v.ID, action, from, v.Status, a.ID, v.AssigneeID, reason); err != nil {
		return err
	}
	return audit(ctx, tx, a, "family.request_"+action, "family_request", v.ID, "status", "revision", "assignee_id")
}
func (r *Repository) CreateRequest(ctx context.Context, sid int64, in RequestInput) (Request, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, true)
	if err != nil {
		return Request{}, err
	}
	defer tx.Rollback(ctx)
	if sid > 0 {
		if in.StudentID != nil && *in.StudentID != sid {
			return Request{}, httpx.Invalid("student_id", "Ребёнок не соответствует адресу запроса.")
		}
		in.StudentID = &sid
	}
	if in.StudentID != nil {
		if *in.StudentID < 1 {
			return Request{}, httpx.Invalid("student_id", "Некорректный ребёнок.")
		}
		_, _, err = audience(ctx, tx, a, *in.StudentID)
	} else {
		err = generalRequestAuthor(ctx, tx, a)
	}
	if err != nil {
		return Request{}, err
	}
	var id int64
	if err = tx.QueryRow(ctx, `INSERT INTO family_requests(student_id,created_by,subject,body) VALUES($1,$2,$3,$4) RETURNING id`, in.StudentID, a.ID, in.Subject, in.Body).Scan(&id); err != nil {
		return Request{}, err
	}
	if err = requestMessage(ctx, tx, id, a.ID, in.Body, "message", in.Files); err != nil {
		return Request{}, err
	}
	v, err := requestRead(ctx, tx, a, id, false, true)
	if err != nil {
		return Request{}, err
	}
	if err = requestEvent(ctx, tx, a, v, "create", "", ""); err != nil {
		return Request{}, err
	}
	v, err = requestRead(ctx, tx, a, id, false, true)
	if err != nil {
		return Request{}, err
	}
	return v, tx.Commit(ctx)
}
func (s *Service) RequestCard(ctx context.Context, id int64, staff bool) (Request, error) {
	return s.Repo.RequestCard(ctx, id, staff)
}
func (s *Service) ChangeRequest(ctx context.Context, id int64, in RequestChange, staff bool) (Request, error) {
	if in.Revision < 1 || utf8.RuneCountInString(in.Reason) > 5000 {
		return Request{}, httpx.Invalid("revision", "Укажите текущую редакцию; причина — до 5000 символов.")
	}
	switch in.Action {
	case "assign", "start", "close":
		if in.Body != "" || len(in.Files) > 0 {
			return Request{}, httpx.Invalid("body", "Для сообщения используйте отдельное действие.")
		}
	case "reopen":
		if !validText(in.Reason, 5000) {
			return Request{}, httpx.Invalid("reason", "Укажите причину повторного открытия.")
		}
		if in.Body != "" || len(in.Files) > 0 {
			return Request{}, httpx.Invalid("body", "Для сообщения используйте отдельное действие.")
		}
	case "message", "answer":
		if !validText(in.Body, 5000) {
			return Request{}, httpx.Invalid("body", "Введите сообщение до 5000 символов.")
		}
	default:
		return Request{}, httpx.Invalid("action", "Неизвестное действие.")
	}
	if err := validateRequestUploads(in.Files); err != nil {
		return Request{}, err
	}
	return s.Repo.ChangeRequest(ctx, id, in, staff)
}
func (r *Repository) ChangeRequest(ctx context.Context, id int64, in RequestChange, staff bool) (Request, error) {
	tx, a, err := access.Begin(ctx, r.Pool, requestResource(staff), access.Read, true)
	if err != nil {
		return Request{}, err
	}
	defer tx.Rollback(ctx)
	v, err := requestRead(ctx, tx, a, id, staff, false)
	if err != nil {
		return Request{}, err
	}
	if v.Revision != in.Revision {
		return Request{}, conflict()
	}
	allowed := false
	for _, x := range v.Actions {
		if x == in.Action {
			allowed = true
		}
	}
	if !allowed {
		return Request{}, httpx.Forbidden()
	}
	from := v.Status
	switch in.Action {
	case "assign":
		ok, e := requestAssignee(ctx, tx, v.StudentID, in.AssigneeID)
		if e != nil {
			return Request{}, e
		}
		if !ok {
			return Request{}, httpx.Invalid("assignee_id", "Выберите действующего администратора или текущего куратора ребёнка.")
		}
		v.AssigneeID = in.AssigneeID
		if v.Status == "new" {
			v.Status = "assigned"
		}
	case "start":
		v.Status = "in_progress"
	case "answer":
		v.Status = "answered"
		v.Reply = in.Body
	case "close":
		v.Status = "closed"
	case "reopen":
		ok, e := requestAssignee(ctx, tx, v.StudentID, v.AssigneeID)
		if e != nil {
			return Request{}, e
		}
		if ok {
			v.Status = "assigned"
		} else {
			v.Status = "new"
			v.AssigneeID = nil
		}
	case "message":
		if !staff && v.Status == "answered" {
			v.Status = "in_progress"
		}
	}
	if in.Action == "message" || in.Action == "answer" {
		kind := "message"
		if in.Action == "answer" {
			kind = "answer"
		}
		if err = requestMessage(ctx, tx, id, a.ID, in.Body, kind, in.Files); err != nil {
			return Request{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE family_requests SET status=$2,assignee_id=$3,reply=$4,revision=revision+1,answered_at=CASE WHEN $5='answer' THEN clock_timestamp() ELSE answered_at END,closed_at=CASE WHEN $5='close' THEN clock_timestamp() WHEN $5='reopen' THEN NULL ELSE closed_at END WHERE id=$1`, id, v.Status, v.AssigneeID, v.Reply, in.Action)
	if err != nil {
		return Request{}, err
	}
	if err = requestEvent(ctx, tx, a, v, in.Action, from, strings.TrimSpace(in.Reason)); err != nil {
		return Request{}, err
	}
	if in.Action == "answer" {
		if err = notify.RequestAnswer(ctx, tx, id, v.Revision+1); err != nil {
			return Request{}, err
		}
	}
	v, err = requestRead(ctx, tx, a, id, staff, true)
	if err != nil {
		return Request{}, err
	}
	return v, tx.Commit(ctx)
}

// The old reply endpoint follows the same state machine and appends an answer.
func (r *Repository) Reply(ctx context.Context, id int64, in ReplyInput) error {
	_, err := r.ChangeRequest(ctx, id, RequestChange{Action: "answer", Revision: in.Revision, Body: in.Reply}, true)
	return err
}
func (s *Service) RequestDownload(ctx context.Context, id, fid int64, staff bool) (File, error) {
	return s.Repo.RequestDownload(ctx, id, fid, staff)
}
func (r *Repository) RequestDownload(ctx context.Context, id, fid int64, staff bool) (File, error) {
	tx, a, err := access.Begin(ctx, r.Pool, requestResource(staff), access.Read, false)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = requestRead(ctx, tx, a, id, staff, false); err != nil {
		return File{}, err
	}
	var f File
	err = tx.QueryRow(ctx, `SELECT f.id,f.name,f.media_type,octet_length(f.data),f.data FROM family_request_files f JOIN family_request_messages m ON m.id=f.message_id WHERE m.request_id=$1 AND f.id=$2`, id, fid).Scan(&f.ID, &f.Name, &f.MediaType, &f.Size, &f.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		err = missing()
	}
	if err != nil {
		return File{}, err
	}
	return f, tx.Commit(ctx)
}
