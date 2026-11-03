package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Struct field matching is exact, as in OpenAPI; encoding/json alone also
// accepts case-insensitive aliases, which can overwrite a validated field.
func validateJSONFields(raw []byte, t reflect.Type) error {
	if t == nil {
		return nil
	}
	if t.Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()) {
		return nil
	}
	nullable:=t.Kind()==reflect.Pointer || t.Kind()==reflect.Map || t.Kind()==reflect.Slice || t.Kind()==reflect.Interface
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()) {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if nullable{return nil}
		return errors.New("null for non-nullable field")
	} // nullable PATCH fields retain their contract
	switch t.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		allowed := map[string]reflect.Type{}
		var collect func(reflect.Type)
		collect = func(st reflect.Type) {
			for i := 0; i < st.NumField(); i++ {
				f := st.Field(i)
				if !f.IsExported() {
					continue
				}
				tag := strings.Split(f.Tag.Get("json"), ",")[0]
				if tag == "-" {
					continue
				}
				ft := f.Type
				for ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if f.Anonymous && tag == "" && ft.Kind() == reflect.Struct {
					collect(ft)
					continue
				}
				if tag == "" {
					tag = f.Name
				}
				allowed[tag] = f.Type
			}
		}
		collect(t)
		for name, value := range fields {
			ft, ok := allowed[name]
			if !ok {
				return errors.New("unknown JSON field")
			}
			if err := validateJSONFields(value, ft); err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return nil
		}
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, v := range values {
			if err := validateJSONFields(v, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, v := range values {
			if err := validateJSONFields(v, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseQuery(r *http.Request) (url.Values, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, Invalid("query", "Некорректные параметры запроса.")
	}
	return q, nil
}

// Reject ambiguous object keys before decoding into Go structs: encoding/json
// otherwise silently overwrites the first value, including escaped aliases.
func validateJSONObject(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("invalid UTF-8")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("object required")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := jsonValue(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
func jsonValue(dec *json.Decoder, depth int) error {
	if depth > 128 {
		return errors.New("JSON too deeply nested")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || keys[name] {
				return errors.New("duplicate key")
			}
			keys[name] = true
			if err = jsonValue(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err = jsonValue(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected delimiter")
	}
	_, err = dec.Token()
	return err
}

// Commands without parameters accept an empty body for existing clients, or {}.
// Supplying a body never silently bypasses JSON validation.
func DecodeEmpty(w http.ResponseWriter, r *http.Request) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		return E(400, "invalid_json", "Некорректный JSON или превышен размер запроса.")
	}
	if len(raw) == 0 {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return Decode(w, r, &struct{}{})
}
