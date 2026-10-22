package curriculum

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
)

func (h *Handler) PreviewTransfer(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	var in TransferInput
	if e = httpx.Decode(w, r, &in); e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.PreviewTransfer(r.Context(), id, in)
	reply(w, r, v, e, 200)
}
func (h *Handler) Transfer(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	var in TransferInput
	if e = httpx.DecodeLimit(w, r, &in, maxContentRequestBytes); e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Transfer(r.Context(), id, in)
	reply(w, r, v, e, 200)
}
func (h *Handler) Transfers(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Transfers(r.Context(), id)
	reply(w, r, v, e, 200)
}
