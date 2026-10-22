package curriculum

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type Mapping struct {
	LessonID    int64  `json:"lesson_id"`
	ModuleKey   string `json:"version_module_key"`
	TemplateKey string `json:"template_key"`
}
type TransferInput struct {
	TargetVersionID int64     `json:"target_version_id"`
	PreviewToken    string    `json:"preview_token"`
	Reason          string    `json:"reason"`
	Mappings        []Mapping `json:"mappings"`
}
type AffectedLesson struct {
	ID                 int64           `json:"id"`
	Topic              string          `json:"topic"`
	StartsAt           time.Time       `json:"starts_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	ProgramVersionID   int64           `json:"program_version_id"`
	ModuleID           *int64          `json:"module_id"`
	ModuleKey          string          `json:"version_module_key"`
	TemplateKey        string          `json:"template_key"`
	PlanSnapshot       json.RawMessage `json:"plan_snapshot"`
	Suggested          Mapping         `json:"suggested"`
	NeedsMapping       bool            `json:"needs_mapping"`
	ClearsLegacyModule bool            `json:"clears_legacy_module"`
}
type Preview struct {
	GroupID           int64            `json:"group_id"`
	GroupUpdatedAt    time.Time        `json:"group_updated_at"`
	PreviousVersionID int64            `json:"previous_version_id"`
	Target            Version          `json:"target"`
	Lessons           []AffectedLesson `json:"lessons"`
	Token             string           `json:"preview_token"`
}
type TransferRecord struct {
	ID                int64           `json:"id"`
	ActorID           int64           `json:"actor_id"`
	PreviousVersionID int64           `json:"previous_version_id"`
	TargetVersionID   int64           `json:"target_version_id"`
	Reason            string          `json:"reason"`
	PreviousLessons   json.RawMessage `json:"previous_lessons"`
	ResultingLessons  json.RawMessage `json:"resulting_lessons"`
	CreatedAt         time.Time       `json:"created_at"`
}

func mappingExists(d Document, m Mapping) bool {
	if m.ModuleKey == "" {
		return m.TemplateKey == ""
	}
	for _, s := range d.Sections {
		for _, mod := range s.Modules {
			if mod.Key == m.ModuleKey {
				if m.TemplateKey == "" {
					return true
				}
				for _, t := range mod.Lessons {
					if t.Key == m.TemplateKey {
						return true
					}
				}
			}
		}
	}
	return false
}
func preview(ctx context.Context, tx pgx.Tx, groupID, targetID int64, lock bool) (Preview, error) {
	out := Preview{GroupID: groupID, Lessons: []AffectedLesson{}}
	var programID int64
	var status string
	q := `SELECT program_id,program_version_id,updated_at,status FROM groups WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	e := tx.QueryRow(ctx, q, groupID).Scan(&programID, &out.PreviousVersionID, &out.GroupUpdatedAt, &status)
	if e != nil {
		return out, e
	}
	if status == "archived" {
		return out, httpx.E(409, "group_archived", "Сначала восстановите группу.")
	}
	q = `SELECT ` + versionColumns + ` FROM program_versions v JOIN programs p ON p.id=v.program_id WHERE v.id=$1 AND p.status='active'`
	if lock {
		q += ` FOR SHARE OF v,p`
	}
	out.Target, e = scanVersion(tx.QueryRow(ctx, q, targetID))
	if e != nil {
		return out, e
	}
	if out.Target.Status != "published" {
		return out, httpx.E(409, "version_not_published", "Выберите опубликованную версию активной программы.")
	}
	if out.PreviousVersionID == targetID {
		return out, httpx.E(409, "same_version", "Группа уже закреплена за этой версией.")
	}
	q = `SELECT id,topic,starts_at,updated_at,program_version_id,module_id,version_module_key,template_key,plan_snapshot
 FROM lessons WHERE group_id=$1 AND status='scheduled' AND starts_at>clock_timestamp() AND snapshot_state='not_captured' ORDER BY id`
	if lock {
		q += ` FOR UPDATE`
	}
	rows, e := tx.Query(ctx, q, groupID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var l AffectedLesson
		if e = rows.Scan(&l.ID, &l.Topic, &l.StartsAt, &l.UpdatedAt, &l.ProgramVersionID, &l.ModuleID, &l.ModuleKey, &l.TemplateKey, &l.PlanSnapshot); e != nil {
			rows.Close()
			return out, e
		}
		l.Suggested = Mapping{LessonID: l.ID, ModuleKey: l.ModuleKey, TemplateKey: l.TemplateKey}
		l.NeedsMapping = !mappingExists(out.Target.Document, l.Suggested)
		l.ClearsLegacyModule = programID != out.Target.ProgramID && l.ModuleID != nil
		// Keys are only unique within a document. A matching key in an unrelated
		// program does not establish that the teaching content corresponds.
		if programID != out.Target.ProgramID && (l.ModuleKey != "" || l.TemplateKey != "" || l.ModuleID != nil) {
			l.NeedsMapping = true
		}
		out.Lessons = append(out.Lessons, l)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	hash := sha256.Sum256(raw)
	out.Token = hex.EncodeToString(hash[:])
	return out, nil
}
func (s *Service) PreviewTransfer(ctx context.Context, id int64, in TransferInput) (Preview, error) {
	if in.TargetVersionID <= 0 {
		return Preview{}, httpx.Invalid("target_version_id", "Выберите опубликованную версию.")
	}
	v, e := s.Repo.PreviewTransfer(ctx, id, in.TargetVersionID)
	return v, translate(e)
}
func (r *Repository) PreviewTransfer(ctx context.Context, id, target int64) (Preview, error) {
	tx, _, e := r.begin(ctx, access.Groups, access.Manage)
	if e != nil {
		return Preview{}, e
	}
	defer tx.Rollback(ctx)
	v, e := preview(ctx, tx, id, target, false)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Service) Transfer(ctx context.Context, id int64, in TransferInput) (TransferRecord, error) {
	if in.TargetVersionID <= 0 || len(in.PreviewToken) != 64 {
		return TransferRecord{}, httpx.Invalid("preview_token", "Сначала получите предварительный просмотр.")
	}
	if e := textField(&in.Reason, "reason", 1, 1000); e != nil {
		return TransferRecord{}, e
	}
	v, e := s.Repo.Transfer(ctx, id, in)
	return v, translate(e)
}
func (r *Repository) Transfer(ctx context.Context, id int64, in TransferInput) (TransferRecord, error) {
	out := TransferRecord{}
	tx, actor, e := r.begin(ctx, access.Groups, access.Manage)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	p, e := preview(ctx, tx, id, in.TargetVersionID, true)
	if e != nil {
		return out, e
	}
	if p.Token != in.PreviewToken {
		return out, httpx.E(409, "stale_preview", "Состав занятий или версия изменились. Повторите предварительный просмотр.")
	}
	if len(in.Mappings) != len(p.Lessons) {
		return out, httpx.Invalid("mappings", "Подтвердите план каждого затронутого занятия.")
	}
	mappings := map[int64]Mapping{}
	for _, m := range in.Mappings {
		if _, ok := mappings[m.LessonID]; ok || !mappingExists(p.Target.Document, m) {
			return out, httpx.Invalid("mappings", "Повторное занятие или шаблон другой версии.")
		}
		mappings[m.LessonID] = m
	}
	for _, l := range p.Lessons {
		if _, ok := mappings[l.ID]; !ok {
			return out, httpx.Invalid("mappings", "Не указано сопоставление занятия.")
		}
	}
	before, e := json.Marshal(p.Lessons)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `SELECT set_config('aiq.program_transfer',$1,true)`, strconv.FormatInt(id, 10)); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE groups SET program_id=$2,program_version_id=$3,updated_at=clock_timestamp() WHERE id=$1`, id, p.Target.ProgramID, p.Target.ID); e != nil {
		return out, e
	}
	for _, l := range p.Lessons {
		m := mappings[l.ID]
		_, e = tx.Exec(ctx, `UPDATE lessons SET program_id=$2,program_version_id=$3,
 module_id=CASE WHEN program_id=$2 THEN module_id ELSE NULL END,version_module_key=$4,template_key=$5,updated_at=clock_timestamp() WHERE id=$1`,
			l.ID, p.Target.ProgramID, p.Target.ID, m.ModuleKey, m.TemplateKey)
		if e != nil {
			return out, e
		}
	}
	var after []byte
	e = tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'program_version_id',program_version_id,
 'module_id',module_id,'version_module_key',version_module_key,'template_key',template_key,'topic',topic,'plan_snapshot',plan_snapshot) ORDER BY id),'[]'::jsonb)
 FROM lessons WHERE id=ANY($1::bigint[])`, lessonIDs(p.Lessons)).Scan(&after)
	if e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, `INSERT INTO group_program_transfers(group_id,actor_id,previous_version_id,target_version_id,reason,previous_lessons,resulting_lessons)
 VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,created_at`, id, actor.ID, p.PreviousVersionID, p.Target.ID, in.Reason, before, after).Scan(&out.ID, &out.CreatedAt)
	if e != nil {
		return out, e
	}
	if e = database.AuditObject(ctx, tx, &actor.ID, "group.program_transferred", "group", id, "program_id", "program_version_id", "lessons"); e != nil {
		return out, e
	}
	out.ActorID = actor.ID
	out.PreviousVersionID = p.PreviousVersionID
	out.TargetVersionID = p.Target.ID
	out.Reason = in.Reason
	out.PreviousLessons = before
	out.ResultingLessons = after
	return out, tx.Commit(ctx)
}
func lessonIDs(ls []AffectedLesson) []int64 {
	ids := make([]int64, 0, len(ls))
	for _, l := range ls {
		ids = append(ids, l.ID)
	}
	return ids
}
func (s *Service) Transfers(ctx context.Context, id int64) ([]TransferRecord, error) {
	v, e := s.Repo.Transfers(ctx, id)
	return v, translate(e)
}
func (r *Repository) Transfers(ctx context.Context, id int64) ([]TransferRecord, error) {
	out := []TransferRecord{}
	tx, _, e := r.begin(ctx, access.Groups, access.Manage)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id,actor_id,previous_version_id,target_version_id,reason,previous_lessons,resulting_lessons,created_at FROM group_program_transfers WHERE group_id=$1 ORDER BY id DESC`, id)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v TransferRecord
		if e = rows.Scan(&v.ID, &v.ActorID, &v.PreviousVersionID, &v.TargetVersionID, &v.Reason, &v.PreviousLessons, &v.ResultingLessons, &v.CreatedAt); e != nil {
			rows.Close()
			return out, e
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
