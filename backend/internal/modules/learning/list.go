package learning

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"sort"
	"strconv"
)

func placeholder(prefix string, n int) string { return prefix + "$" + strconv.Itoa(n) }

func (repo *Repository) List(ctx context.Context, sid int64, f Filter) (Block, error) {
	out := Block{State: "ready", Items: []Record{}, Page: f.Page, PageSize: 20, From: f.Start.Format("2006-01-02"), To: f.End.AddDate(0, 0, -1).Format("2006-01-02")}
	if !validKind(f.Kind) && f.Kind != "skills" || f.Page < 1 || f.Start.IsZero() || !f.End.After(f.Start) {
		return out, httpx.Invalid("filter", "Некорректный раздел, страница или период.")
	}
	tx, a, err := access.Begin(ctx, repo.Pool, access.Students, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = student(ctx, tx, a, sid); err != nil {
		return out, err
	}
	assigned, err := curator(ctx, tx, a, sid)
	if err != nil {
		return out, err
	}
	out.CanCreate = f.Kind != "skills" && (f.Kind == "goals" && assigned || f.Kind != "goals" && (supervisor(a) || assigned || has(a, "teacher")))
	kind := f.Kind
	if kind == "skills" {
		kind = "observations"
	}
	where := ` WHERE r.student_id=$5 AND r.kind=$6 AND ` + scopeSQL
	values := append(args(a), sid, kind)
	if f.Kind == "skills" || f.Kind == "observations" && !f.Work {
		where += ` AND r.state='reviewed' AND r.published`
	}
	if f.Kind == "skills" {
	} else if f.Current {
		if kind != "goals" && kind != "assignments" {
			return out, httpx.Invalid("current", "Текущий срез доступен для целей и заданий.")
		}
		where += ` AND r.closed_at IS NULL AND r.state NOT IN ('cancelled','withdrawn','revised')`
	} else {
		values = append(values, f.Start, f.End, f.Start.Format("2006-01-02"), f.End.Format("2006-01-02"))
		switch kind {
		case "observations", "achievements":
			where += ` AND v.content->>'occurred_on'>=$9 AND v.content->>'occurred_on'<$10`
		case "goals":
			where += ` AND r.created_at<$8 AND (r.closed_at IS NULL OR r.closed_at>=$7)`
		case "projects":
			where += ` AND v.content->>'starts_on'<$10 AND (COALESCE(v.content->>'ends_on','')='' OR v.content->>'ends_on'>=$9)`
		case "assignments":
			where += ` AND (r.created_at>=$7 AND r.created_at<$8 OR EXISTS(SELECT 1 FROM learning_decisions d WHERE d.record_id=r.id AND d.created_at>=$7 AND d.created_at<$8))`
		default:
			where += ` AND r.created_at>=$7 AND r.created_at<$8`
		}
		// All placeholders have explicit types, including those unused by a kind.
		where += ` AND $7::timestamptz IS NOT NULL AND $8::timestamptz IS NOT NULL AND $9::text IS NOT NULL AND $10::text IS NOT NULL`
	}
	if f.Status != "" {
		values = append(values, f.Status)
		where += placeholder(" AND r.state=", len(values))
	}
	if f.ModelVersion != "" {
		values = append(values, f.ModelVersion)
		where += placeholder(" AND v.content->>'model_version'=", len(values))
	}
	if f.SkillKey != "" {
		values = append(values, f.SkillKey)
		where += placeholder(" AND v.content->>'skill_key'=", len(values))
	}
	// Filter object rights before counting or slicing. Skills use the same allowed
	// observations and retain their sources; no numeric level is inferred.
	rows, err := tx.Query(ctx, `SELECT `+columns+recordFrom+where+` ORDER BY r.created_at DESC,r.id DESC`, values...)
	if err != nil {
		return out, err
	}
	records := []Record{}
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if f.Kind == "skills" {
		groups := map[string]*Record{}
		keys := []string{}
		for _, r := range records {
			key := r.Content.ModelVersion + "\x00" + r.Content.SkillKey
			if groups[key] == nil {
				copy := r
				copy.State = "no_methodology"
				copy.Source = source(r, f.Start, f.End)
				copy.Sources = []Source{}
				copy.Actions = []string{}
				groups[key] = &copy
				keys = append(keys, key)
			}
			groups[key].Sources = append(groups[key].Sources, source(r, f.Start, f.End))
		}
		sort.Strings(keys)
		records = []Record{}
		for _, key := range keys {
			records = append(records, *groups[key])
		}
	}
	out.Total = len(records)
	offset := (f.Page - 1) * out.PageSize
	if offset < 0 {
		return out, httpx.Invalid("page", "Некорректная страница.")
	}
	for i := offset; i < len(records) && i < offset+out.PageSize; i++ {
		r := records[i]
		if f.Kind != "skills" {
			r, err = projected(ctx, tx, a, r, f.Start, f.End)
			if err != nil {
				return out, err
			}
		}
		out.Items = append(out.Items, r)
	}
	return out, tx.Commit(ctx)
}
