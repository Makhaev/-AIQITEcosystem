// m2-fixture is an explicit test helper. It never seeds the application schema.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func run() error {
	if len(os.Args)!=2 { return fmt.Errorf("expected init or cleanup") }
	baseURL:=os.Getenv("TEST_DATABASE_URL")
	if baseURL=="" { return fmt.Errorf("TEST_DATABASE_URL is required") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second); defer cancel()
	base,err:=pgx.Connect(ctx,baseURL); if err!=nil { return err }; defer base.Close(ctx)
	if os.Args[1]=="cleanup" {
		schema:=os.Getenv("M2_SCHEMA")
		if !regexp.MustCompile(`^aiq_m2_[a-f0-9]{16}$`).MatchString(schema) { return fmt.Errorf("invalid isolated schema") }
		_,err=base.Exec(ctx,`DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); return err
	}
	if os.Args[1]!="init" { return fmt.Errorf("unknown command") }
	password:=os.Getenv("M2_TEST_PASSWORD")
	if len(password)<12 || len(password)>72 { return fmt.Errorf("explicit M2_TEST_PASSWORD (12..72 bytes) is required") }
	var token [8]byte; if _,err=rand.Read(token[:]); err!=nil { return err }
	schema:="aiq_m2_"+hex.EncodeToString(token[:])
	if _,err=base.Exec(ctx,`CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err!=nil { return err }
	ready:=false
	defer func(){ if !ready { _,_=base.Exec(context.Background(),`DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`) } }()
	u,err:=url.Parse(baseURL); if err!=nil { return err }
	q:=u.Query(); q.Set("search_path",schema); u.RawQuery=q.Encode()
	pool,err:=pgxpool.New(ctx,u.String()); if err!=nil { return err }; defer pool.Close()
	if err=database.Migrate(ctx,pool,core.Migrations); err!=nil { return err }
	hash,err:=bcrypt.GenerateFromPassword([]byte(password),bcrypt.DefaultCost); if err!=nil { return err }
	ids:=map[string]int64{}
	for _,role:=range []string{"admin","methodologist","education_leader","teacher"} {
		var id int64
		err=pool.QueryRow(ctx,`INSERT INTO users(email,name,password_hash,must_change_password) VALUES($1,$2,$3,false) RETURNING id`,role+"@example.test","M2 "+role,string(hash)).Scan(&id)
		if err!=nil { return err }
		if _,err=pool.Exec(ctx,`INSERT INTO user_roles(user_id,role) VALUES($1,$2)`,id,role); err!=nil { return err }
		ids[role]=id
	}
	var teacherID int64
	if err=pool.QueryRow(ctx,`INSERT INTO teachers(name,name_search,user_id) VALUES('Педагог M2','педагог m2',$1) RETURNING id`,ids["teacher"]).Scan(&teacherID); err!=nil { return err }
	if err=json.NewEncoder(os.Stdout).Encode(map[string]any{"database_url":u.String(),"schema":schema,"teacher_id":teacherID}); err!=nil { return err }
	ready=true; return nil
}
func main(){ if err:=run(); err!=nil { fmt.Fprintln(os.Stderr,err); os.Exit(1) } }
