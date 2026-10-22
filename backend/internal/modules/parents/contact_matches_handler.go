package parents

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
)

func (h *Handler) ContactMatches(w http.ResponseWriter, r *http.Request) {
	// Contacts belong in the JSON body, never in search/filter query parameters.
	for key := range r.URL.Query() {
		if key != "page" && key != "page_size" {
			respond(w, r, nil, httpx.Invalid("query", "В URL допустимы только page и page_size. Контакты передавайте в теле запроса."), 200)
			return
		}
	}
	f, err := filterFrom(r)
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	var in ContactMatchInput
	if err = httpx.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.ContactMatches(r.Context(), in, f)
	respond(w, r, v, err, 200)
}
