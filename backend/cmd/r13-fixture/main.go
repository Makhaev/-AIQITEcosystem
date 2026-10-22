// r13-fixture creates only new, explicitly marked synthetic databases.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/backup"
	"aiq.local/core/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Fixture struct {
	URL      string    `json:"database_url"`
	Database string    `json:"database"`
	Password string    `json:"password"`
	Kind     string    `json:"kind"`
	Users    []Account `json:"users"`
}
type Account struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if os.Getenv("R13_TEST_MODE") != "synthetic" || len(os.Args) != 3 {
		return fmt.Errorf("set R13_TEST_MODE=synthetic; usage: r13-fixture create-e2e|create-load|inventory fixture.json")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	action, path := os.Args[1], os.Args[2]
	if action == "inventory" {
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var f Fixture
		if e = json.Unmarshal(b, &f); e != nil {
			return e
		}
		p, e := pgxpool.New(ctx, f.URL)
		if e != nil {
			return fmt.Errorf("fixture connection failed")
		}
		defer p.Close()
		var name, marker string
		if e = p.QueryRow(ctx, "SELECT current_database(),shobj_description(oid,'pg_database') FROM pg_database WHERE datname=current_database()").Scan(&name, &marker); e != nil {
			return e
		}
		if !strings.HasPrefix(name, "aiq_r13_") || name != f.Database || marker != "AIQ R13 SYNTHETIC ONLY" {
			return fmt.Errorf("refusing non-fixture database")
		}
		// Session/login counters change under authenticated reads; educational data must not.
		if _, e = p.Exec(ctx, "SET TIME ZONE 'UTC'"); e != nil {
			return e
		}
		data, e := backup.Inventory(ctx, p)
		if e != nil {
			return e
		}
		for _, n := range []string{"sessions", "login_rate_limits", "users", "audit_logs"} {
			delete(data, "public."+n)
		}
		return json.NewEncoder(os.Stdout).Encode(data)
	}
	if action != "create-e2e" && action != "create-load" {
		return fmt.Errorf("unknown action")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		return fmt.Errorf("fixture config already exists; choose a new path")
	}
	baseURL := os.Getenv("R13_ADMIN_URL")
	if baseURL == "" {
		return fmt.Errorf("R13_ADMIN_URL required (maintenance database, CREATEDB)")
	}
	base, e := pgx.Connect(ctx, baseURL)
	if e != nil {
		return fmt.Errorf("maintenance connection failed")
	}
	defer base.Close(ctx)
	var id [8]byte
	if _, e = rand.Read(id[:]); e != nil {
		return e
	}
	name := "aiq_r13_" + strings.TrimPrefix(action, "create-") + "_" + hex.EncodeToString(id[:])
	if _, e = base.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE template0"); e != nil {
		return e
	}
	if _, e = base.Exec(ctx, "COMMENT ON DATABASE "+pgx.Identifier{name}.Sanitize()+" IS 'AIQ R13 SYNTHETIC ONLY'"); e != nil {
		return e
	}
	u, e := url.Parse(baseURL)
	if e != nil {
		return e
	}
	u.Path = "/" + name
	f := Fixture{URL: u.String(), Database: name, Password: "R13-Synthetic-" + hex.EncodeToString(id[:]), Kind: strings.TrimPrefix(action, "create-")}
	// Persist identity immediately so a failed seed remains diagnosable, never silently reused.
	save := func() error { b, _ := json.MarshalIndent(f, "", "  "); return os.WriteFile(path, b, 0600) }
	if e = save(); e != nil {
		return e
	}
	if action == "create-e2e" {
		return nil
	} // Admin CLI must bootstrap this empty database.
	pool, e := pgxpool.New(ctx, f.URL)
	if e != nil {
		return e
	}
	defer pool.Close()
	if e = database.Migrate(ctx, pool, core.Migrations); e != nil {
		return e
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(f.Password), bcrypt.DefaultCost)
	if e != nil {
		return e
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for i := 1; i <= 20; i++ {
		role := []string{"admin", "teacher", "curator", "leader"}[(i-1)/5]
		email := fmt.Sprintf("r13-%02d@example.test", i)
		if _, e = tx.Exec(ctx, `INSERT INTO users(email,name,password_hash,must_change_password) VALUES($1,$2,$3,false)`, email, fmt.Sprintf("R13 %s %02d", role, i), string(hash)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO user_roles(user_id,role) VALUES($1,$2)", i, role); e != nil {
			return e
		}
		f.Users = append(f.Users, Account{email, role})
	}
	_, e = tx.Exec(ctx, `
 INSERT INTO teachers(name,name_search,user_id) SELECT 'R13 teacher '||i,'r13 teacher '||i,i+5 FROM generate_series(1,5)i;
 INSERT INTO students(name,name_search,birth_date,curator_id) SELECT 'R13 pupil '||i,'r13 pupil '||i,DATE '2012-01-01'+(i%2000),11+(i-1)%5 FROM generate_series(1,1000)i;
 INSERT INTO parents(name,name_search,email) SELECT 'R13 parent '||i,'r13 parent '||i,'parent-'||i||'@example.test' FROM generate_series(1,1000)i;
 INSERT INTO student_parents(student_id,parent_id,relationship,is_primary) SELECT i,i,'parent',true FROM generate_series(1,1000)i;
 INSERT INTO programs(name,name_search,status) SELECT 'R13 program '||i,'r13 program '||i,'active' FROM generate_series(1,10)i;
 INSERT INTO program_modules(program_id,name,position) SELECT i,'R13 module '||i,1 FROM generate_series(1,10)i;
 -- Synthetic load accounts also prepare/approve the fixture versions.
 -- The real HTTP publication/ACL workflow is covered by the M2 browser test.
 INSERT INTO user_roles(user_id,role) VALUES(1,'methodologist'),(2,'education_leader');
 INSERT INTO program_versions(program_id,number,created_by,document)
 SELECT id,1,1,jsonb_build_object('name',name) || '{"description":"Load fixture","sections":[
   {"key":"section","name":"Раздел","modules":[{"key":"module","name":"Модуль","description":"","lessons":[
     {"key":"template","topic":"Занятие","duration_minutes":45,"goals":"Цели","outline":"Конспект",
      "exercises":"Упражнения","equipment":"Оборудование","materials":"Материалы","criteria":"Критерии"}
   ]}]}]}'::jsonb
 FROM programs;
 INSERT INTO program_version_events(version_id,actor_id,action) SELECT id,1,'created' FROM program_versions;
 UPDATE program_versions SET status='review',submitted_by=1;
 INSERT INTO program_version_events(version_id,actor_id,action) SELECT id,1,'submit' FROM program_versions;
 UPDATE program_versions SET status='published',approved_by=2,published_at=clock_timestamp();
 INSERT INTO program_version_events(version_id,actor_id,action) SELECT id,2,'publish' FROM program_versions;
 INSERT INTO groups(name,name_search,program_id,teacher_id,capacity,status) SELECT 'R13 group '||i,'r13 group '||i,1+(i-1)%10,1+(i-1)%5,10,'active' FROM generate_series(1,100)i;
 INSERT INTO group_enrollments(group_id,student_id,starts_on) SELECT 1+(i-1)/10,i,'2020-01-01' FROM generate_series(1,1000)i;
 INSERT INTO lessons(group_id,program_id,module_id,topic,topic_search,teacher_id,starts_at,ends_at)
 SELECT g.id,g.program_id,g.program_id,'R13 lesson '||d,'r13 lesson '||d,g.teacher_id,
 TIMESTAMPTZ '2025-01-01 00:00:00+03'+(d*24+(g.id-1)/5)*interval '1 hour',
 TIMESTAMPTZ '2025-01-01 00:00:00+03'+(d*24+(g.id-1)/5)*interval '1 hour'+interval '45 minutes'
 FROM groups g CROSS JOIN generate_series(0,99)d;
 INSERT INTO lesson_participants(lesson_id,group_id,enrollment_id,student_id)
 SELECT l.id,l.group_id,e.id,e.student_id FROM lessons l JOIN group_enrollments e ON e.group_id=l.group_id;
 UPDATE attendance SET status=CASE id%3 WHEN 0 THEN 'absent' WHEN 1 THEN 'present' ELSE 'late' END;
 UPDATE lessons SET roster_initialized_at=now(),status='completed',snapshot_program_name='R13 historical program',snapshot_topic=topic,snapshot_state='captured',snapshot_captured_at=now();
 INSERT INTO tasks(title,title_search,description,creator_id,assignee_id,due_at)
 SELECT 'R13 task '||i,'r13 task '||i,'Synthetic expected result',1,1+(i-1)%20,now()+interval '7 days' FROM generate_series(1,1000)i;
 INSERT INTO audit_logs(actor_id,action,object_type,object_id,changed_fields) VALUES(1,'fixture.created','student',1,ARRAY['name']);
 `)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if _, e = pool.Exec(ctx, "ANALYZE"); e != nil {
		return e
	}
	return save()
}
