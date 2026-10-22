package dashboard

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
)

type Handler struct{ Service *Service }

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	v, e := h.Service.Get(r.Context(), r.URL.Query())
	if e != nil {
		httpx.Fail(w, r, e)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}
