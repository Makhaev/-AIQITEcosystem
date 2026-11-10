package pilot

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type testQuestion struct {
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
	Correct int      `json:"correct"`
}

func (h *Handler) beginHomework(ctx context.Context, action access.Action) (pgx.Tx, httpx.Actor, error) {
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return nil, httpx.Actor{}, err
	}
	var a httpx.Actor
	if action == access.Read {
		a, err = access.AuthorizeRead(ctx, tx, access.HomeworkTests)
	} else {
		err = access.LockManagement(ctx, tx)
		if err == nil {
			a, err = access.Authorize(ctx, tx, access.HomeworkTests, action)
		}
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, a, err
	}
	return tx, a, nil
}

func (h *Handler) HomeworkTestStudents(w http.ResponseWriter, r *http.Request) {
	tx, a, err := h.beginHomework(r.Context(), access.Manage)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Creators choose from current active students, independently of lessons.
	sendRows(w, r, tx, `SELECT jsonb_build_object('id',s.id,'name',s.name) FROM students s WHERE s.status='active' AND ($2::boolean OR curator_has_student($1,s.id)) ORDER BY s.name,s.id`, a.ID, a.Admin() || has(a, "methodist"))
}

func (h *Handler) CreateHomeworkTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title        string         `json:"title"`
		Instructions string         `json:"instructions"`
		Questions    []testQuestion `json:"questions"`
		StudentIDs   []int64        `json:"student_ids"`
		DueAt        *time.Time     `json:"due_at"`
	}
	if err := httpx.DecodeLimit(w, r, &in, 1<<20); err != nil {
		fail(w, r, err)
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Instructions = strings.TrimSpace(in.Instructions)
	if !textValid(in.Title, 200) || strings.ContainsRune(in.Title, 0) || len([]rune(in.Instructions)) > 5000 || strings.ContainsRune(in.Instructions, 0) || len(in.Questions) < 1 || len(in.Questions) > 30 || len(in.StudentIDs) < 1 || len(in.StudentIDs) > 100 {
		fail(w, r, httpx.Invalid("test", "Укажите название, 1–30 вопросов и 1–100 учеников."))
		return
	}
	for i := range in.Questions {
		q := &in.Questions[i]
		q.Prompt = strings.TrimSpace(q.Prompt)
		if !textValid(q.Prompt, 2000) || strings.ContainsRune(q.Prompt, 0) || len(q.Options) < 2 || len(q.Options) > 6 || q.Correct < 0 || q.Correct >= len(q.Options) {
			fail(w, r, httpx.Invalid("questions", "Проверьте вопросы, варианты и правильные ответы."))
			return
		}
		seen := map[string]bool{}
		for j := range q.Options {
			q.Options[j] = strings.TrimSpace(q.Options[j])
			option := q.Options[j]
			if !textValid(option, 500) || strings.ContainsRune(option, 0) || seen[option] {
				fail(w, r, httpx.Invalid("questions", "Варианты должны быть заполнены и различаться."))
				return
			}
			seen[option] = true
		}
	}
	tx, a, err := h.beginHomework(r.Context(), access.Manage)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	questions, err := json.Marshal(in.Questions)
	if err != nil {
		fail(w, r, err)
		return
	}
	var id int64
	err = tx.QueryRow(r.Context(), `INSERT INTO homework_tests(title,instructions,questions,created_by) VALUES($1,$2,$3,$4) RETURNING id`, in.Title, in.Instructions, questions, a.ID).Scan(&id)
	seen := map[int64]bool{}
	for _, studentID := range in.StudentIDs {
		if err != nil {
			break
		}
		if studentID <= 0 || seen[studentID] {
			err = httpx.Invalid("student_ids", "Проверьте выбранных учеников.")
			break
		}
		seen[studentID] = true
		var allowed bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM students s WHERE s.id=$1 AND s.status='active' AND ($2::boolean OR curator_has_student($3,s.id)))`, studentID, a.Admin() || has(a, "methodist"), a.ID).Scan(&allowed)
		if err == nil && !allowed {
			err = httpx.Invalid("student_ids", "Выбранный ученик недоступен.")
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO homework_test_assignments(test_id,student_id,due_at) VALUES($1,$2,$3)`, id, studentID, in.DueAt)
		}
	}
	if err == nil {
		err = audit(r.Context(), tx, a.ID, "homework_test.created", "homework_test", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"id": id, "assigned": len(in.StudentIDs)})
}

func (h *Handler) HomeworkTests(w http.ResponseWriter, r *http.Request) {
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000000 {
			fail(w, r, httpx.Invalid("page", "Некорректная страница."))
			return
		}
		page = n
	}
	tx, a, err := h.beginHomework(r.Context(), access.Read)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	where := ` FROM homework_test_assignments h JOIN homework_tests t ON t.id=h.test_id JOIN students s ON s.id=h.student_id JOIN users u ON u.id=t.created_by WHERE ($2::boolean OR ($3::boolean AND t.created_by=$1) OR ($4::boolean AND t.created_by=$1 AND curator_has_student($1,s.id)) OR ($5::boolean AND s.user_id=$1 AND s.status='active'))`
	args := []any{a.ID, a.Admin() || has(a, "leader"), has(a, "methodist"), has(a, "curator"), has(a, "student")}
	var total int
	err = tx.QueryRow(r.Context(), `SELECT count(*)`+where, args...).Scan(&total)
	if err != nil {
		fail(w, r, err)
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT jsonb_build_object('id',h.id,'test_id',t.id,'title',t.title,'instructions',t.instructions,'student_id',s.id,'student_name',s.name,'author_name',u.name,'can_submit',($5::boolean AND s.user_id=$1 AND s.status='active' AND h.attempts=0),'due_at',h.due_at,'completed_at',h.completed_at,'attempts',h.attempts,'correct_count',h.correct_count,'answers',h.answers,'questions',(SELECT jsonb_agg(q-'correct' ORDER BY n) FROM jsonb_array_elements(t.questions) WITH ORDINALITY x(q,n)))`+where+` ORDER BY h.created_at DESC,h.id DESC LIMIT 20 OFFSET $6`, append(args, (page-1)*20)...)
	if err != nil {
		fail(w, r, err)
		return
	}
	items := []map[string]any{}
	for rows.Next() {
		var item map[string]any
		if err = rows.Scan(&item); err != nil {
			break
		}
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": 20})
}

func (h *Handler) SubmitHomeworkTest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, r, httpx.Invalid("id", "Некорректный тест."))
		return
	}
	var in struct {
		Answers []int `json:"answers"`
	}
	if err = httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	tx, a, err := h.beginHomework(r.Context(), access.Mark)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var raw []byte
	var completed *time.Time
	var attempts int
	err = tx.QueryRow(r.Context(), `SELECT t.questions,h.completed_at,h.attempts FROM homework_test_assignments h JOIN homework_tests t ON t.id=h.test_id JOIN students s ON s.id=h.student_id WHERE h.id=$1 AND s.user_id=$2 AND s.status='active' FOR UPDATE OF h`, id, a.ID).Scan(&raw, &completed, &attempts)
	if err != nil {
		fail(w, r, err)
		return
	}
	var questions []testQuestion
	if err = json.Unmarshal(raw, &questions); err != nil {
		fail(w, r, err)
		return
	}
	if completed != nil {
		httpx.JSON(w, 200, map[string]any{"completed": true, "awarded": false, "correct_count": len(questions), "total": len(questions)})
		return
	}
	if attempts > 0 {
		fail(w, r, httpx.E(409, "test_already_submitted", "Ответы уже отправлены. Изменить их или пройти тест повторно нельзя."))
		return
	}
	if len(in.Answers) != len(questions) {
		fail(w, r, httpx.Invalid("answers", "Ответьте на все вопросы."))
		return
	}
	correct := 0
	for i, answer := range in.Answers {
		if answer < 0 || answer >= len(questions[i].Options) {
			fail(w, r, httpx.Invalid("answers", "Выберите допустимый вариант ответа."))
			return
		}
		if answer == questions[i].Correct {
			correct++
		}
	}
	passed := correct == len(questions)
	answers, err := json.Marshal(in.Answers)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE homework_test_assignments SET answers=$2,correct_count=$3,attempts=attempts+1,last_attempt_at=now(),completed_at=CASE WHEN $4 THEN now() ELSE NULL END WHERE id=$1`, id, answers, correct, passed)
	}
	if err == nil {
		err = audit(r.Context(), tx, a.ID, "homework_test.attempted", "homework_test_assignment", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"completed": passed, "awarded": passed, "correct_count": correct, "total": len(questions)})
}
