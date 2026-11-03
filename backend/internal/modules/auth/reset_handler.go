package auth

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
)

func (h *Handler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Service.RequestPasswordReset(r.Context(), in.Email, h.clientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]string{"message": "Если аккаунт доступен, мы отправим письмо со ссылкой для восстановления пароля. Ссылка действует 30 минут."})
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Service.ResetPassword(r.Context(), in.Token, in.Password, h.clientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	h.clearCookies(w)
	w.WriteHeader(http.StatusNoContent)
}
