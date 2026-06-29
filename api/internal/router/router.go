package router

import (
	"github.com/go-chi/chi/v5"
	_ "github.com/jpgomesr/NeuralVault/docs"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/health"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	httpSwagger "github.com/swaggo/http-swagger"
)

func NewRouter(cfg *config.Config, pool storage.Pool) *chi.Mux {
	r := chi.NewRouter()

	healthService := health.HealthService{}
	healthHandler := health.NewHandler(healthService)

	r.Route("/", func(r chi.Router) {
		r.Mount("/health", health.Routes(healthHandler))

		// Swagger routes
		r.Get("/swagger/*", httpSwagger.WrapHandler)
	})

	return r
}
