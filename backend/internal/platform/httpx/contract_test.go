package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStrictJSONContract(t *testing.T) {
	type Embedded struct {
		Name string `json:"name"`
	}
	type Input struct {
		Embedded
		Nested []struct {
			Flag bool `json:"flag"`
		} `json:"nested"`
		At   *time.Time     `json:"at"`
		Data map[string]any `json:"data"`
	}
	valid := []string{`{"name":"Тест","nested":[{"flag":true}],"at":"2026-10-29T10:00:00Z"}`, `{"name":"","at":null,"nested":null,"data":{"A":1,"a":2}}`}
	invalid := []string{`null`, `[]`, `true`, ``, `{"name":"one","name":"two"}`, `{"name":"one","\u006eame":"two"}`, `{"Name":"alias"}`, `{"name":"one","Name":"two"}`, `{"nested":[{"flag":true,"flag":false}]}`, `{"nested":[{"Flag":true}]}`, `{"unknown":1}`, `{"nested":[{"unknown":true}]}`, `{"name":3}`, `{} {}`, `{"data":{"nested":{"x":1,"x":2}}}`, "{\"name\":\"\xff\"}"}
	invalid=append(invalid,`{"name":null}`,`{"nested":[{"flag":null}]}`)
	for _, body := range append(valid, invalid...) {
		t.Run(body, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			var in Input
			err := Decode(httptest.NewRecorder(), r, &in)
			want := false
			for _, s := range valid {
				if s == body {
					want = true
				}
			}
			if (err == nil) != want {
				t.Fatalf("valid=%v err=%v", want, err)
			}
		})
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"too long"}`))
	r.Header.Set("Content-Type", "application/json")
	if DecodeLimit(httptest.NewRecorder(), r, &Input{}, 8) == nil {
		t.Fatal("body limit ignored")
	}
}
func TestPaginationContract(t *testing.T) {
	for _, tc := range []struct {
		query      string
		page, size int
		valid      bool
	}{{"", 1, 20, true}, {"?page=2&page_size=100", 2, 100, true}, {"?page_size=1", 1, 1, true}, {"?page=0", 0, 0, false}, {"?page_size=101", 0, 0, false}, {"?page_size=0", 0, 0, false}, {"?page_size=-1", 0, 0, false}, {"?page_size=1.5", 0, 0, false}, {"?page_size=", 0, 0, false}, {"?page=1&page=2", 0, 0, false}, {"?page_size=1&page_size=2", 0, 0, false}, {"?page=99999999999999999", 0, 0, false}, {"?search=%ZZ", 0, 0, false}} {
		p, s, e := Pagination(httptest.NewRequest("GET", "/"+tc.query, nil))
		if (e == nil) != tc.valid || tc.valid && (p != tc.page || s != tc.size) {
			t.Fatalf("%s got %d/%d/%v", tc.query, p, s, e)
		}
	}
}
func TestEmptyCommandsAndErrorFields(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"target":1}`, `null`, `{}`, `[]`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		err := DecodeEmpty(httptest.NewRecorder(), r)
		if (err == nil) != (body == "" || body == `{}`) {
			t.Fatal(body, err)
		}
	}
	w := httptest.NewRecorder()
	Fail(w, httptest.NewRequest("GET", "/", nil), &Error{Status: 400, Code: "test", Message: "Test"})
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if _, ok := v["field_errors"].(map[string]any); !ok {
		t.Fatal(v)
	}
}
