package router

import (
	"github.com/go-chi/chi/v5"
	_ "github.com/jpgomesr/NeuralVault/docs"
	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/chunking/markdown"
	"github.com/jpgomesr/NeuralVault/internal/chunking/text"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/health"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	"github.com/jpgomesr/NeuralVault/internal/sourcereader"
	"github.com/jpgomesr/NeuralVault/internal/sources"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	httpSwagger "github.com/swaggo/http-swagger"
)

func NewRouter(cfg *config.Config, pool storage.Pool, store objectstorage.Client) *chi.Mux {
	r := chi.NewRouter()

	healthService := health.HealthService{}
	healthHandler := health.NewHandler(healthService)

	splitters := map[chunking.ContentType]chunking.Splitter{
		chunking.ContentTypeMarkdown:  markdown.New(),
		chunking.ContentTypePlaintext: text.New(),
	}
	chunkService := chunking.NewChunkService(pool, splitters)
	bus := sources.NewProgressBus()
	sourceService := sources.NewSourceService(pool, store, sourcereader.NewFileReader(), chunkService, bus)
	sourceHandler := sources.NewHandler(sourceService, bus)

	r.Route("/", func(r chi.Router) {
		r.Mount("/health", health.Routes(healthHandler))
		r.Mount("/sources", sources.Routes(sourceHandler))

		// Swagger routes
		r.Get("/swagger/*", httpSwagger.WrapHandler)
	})

	return r
}
