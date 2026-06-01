package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/gorelov-m-v/gophprofile/internal/handlers"
)

func NewRouter(handler *handlers.Handler, webDir string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(httprate.LimitByIP(120, time.Minute))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-User-ID"},
		ExposedHeaders:   []string{"ETag"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/web/upload", http.StatusFound)
	})
	r.Get("/health", handler.Health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/avatars", handler.UploadAvatar)
		r.Get("/avatars/{avatar_id}", handler.GetAvatar)
		r.Delete("/avatars/{avatar_id}", handler.DeleteAvatar)
		r.Get("/avatars/{avatar_id}/metadata", handler.GetMetadata)
		r.Get("/users/{user_id}/avatar", handler.GetUserAvatar)
		r.Delete("/users/{user_id}/avatar", handler.DeleteUserAvatar)
		r.Get("/users/{user_id}/avatars", handler.ListUserAvatars)
	})

	r.Get("/web/upload", handler.WebUpload)
	r.Post("/web/upload", handler.WebUploadPost)
	r.Get("/web/gallery/{user_id}", handler.WebGallery)

	fileServer := http.FileServer(http.Dir(webDir))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	return r
}
