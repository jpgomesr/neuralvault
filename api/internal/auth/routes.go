package auth

import (
	"github.com/go-chi/chi/v5"
)

// Routes returns a Chi router with all auth endpoints mounted. The login and
// callback endpoints are public; /me is protected by RequireUser so it reflects
// the current session.
func Routes(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Get("/login", h.Login)
	r.Get("/callback", h.Callback)
	r.Post("/token", h.Token)
	r.Post("/logout", h.Logout)
	r.With(h.RequireUser).Get("/me", h.Me)
	return r
}
