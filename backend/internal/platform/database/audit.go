package database

import (
	"reflect"
	"sort"
	"strings"
)

// ChangedFields compares only explicitly allowed JSON fields of a flat model.
// Values are never returned or persisted, including contacts and free text.
func ChangedFields[T any](before, after T, allowed ...string) []string {
	result := []string{}
	old, next := reflect.ValueOf(before), reflect.ValueOf(after)
	typ := next.Type()
	for _, name := range allowed {
		for i := 0; i < typ.NumField(); i++ {
			if strings.Split(typ.Field(i).Tag.Get("json"), ",")[0] == name && !reflect.DeepEqual(old.Field(i).Interface(), next.Field(i).Interface()) {
				result = append(result, name)
				break
			}
		}
	}
	sort.Strings(result)
	return result
}
