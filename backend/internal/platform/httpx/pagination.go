package httpx

import (
	"net/http"
	"strconv"
)

const DefaultPageSize = 20
const MaxPageSize = 100

// PageSize also supplies the legacy default to internal callers without a size.
func PageSize(sizes ...int) int {
	if len(sizes) == 0 || sizes[0] < 1 || sizes[0] > MaxPageSize {
		return DefaultPageSize
	}
	return sizes[0]
}

func Pagination(r *http.Request) (page, size int, err error) {
	page, size = 1, DefaultPageSize
	query, err := parseQuery(r)
	if err != nil {
		return 0, 0, err
	}
	for _, field := range []struct {
		name string
		dst  *int
		max  int
	}{{"page", &page, 1000000}, {"page_size", &size, MaxPageSize}} {
		values, ok := query[field.name]
		if !ok {
			continue
		}
		if len(values) != 1 || values[0] == "" {
			return 0, 0, Invalid(field.name, "Укажите одно целое положительное число.")
		}
		n, e := strconv.Atoi(values[0])
		if e != nil || n < 1 || n > field.max {
			return 0, 0, Invalid(field.name, "Значение вне допустимого диапазона.")
		}
		*field.dst = n
	}
	return page, size, nil
}
