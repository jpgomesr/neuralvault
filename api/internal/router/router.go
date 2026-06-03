package router

import (
	"github.com/go-chi/chi/v5"
	_ "github.com/jpgomesr/NeuralVault/docs"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/health"
	httpSwagger "github.com/swaggo/http-swagger"
)

func NewRouter(cfg *config.Config) *chi.Mux {
	r := chi.NewRouter()

	// Example of how to add a new service and handler:
	// exampleService := example.ExampleService{}
	// exampleHandler := example.NewHandler(exampleService)

	healthService := health.HealthService{}
	healthHandler := health.NewHandler(healthService)

	r.Route("/", func(r chi.Router) {
		// Example of how to mount a new service's routes:
		// r.Mount("/example", example.Routes(exampleHandler))
		r.Mount("/health", health.Routes(healthHandler))

		// Swagger routes
		r.Get("/swagger/*", httpSwagger.WrapHandler)
	})

	return r
}
