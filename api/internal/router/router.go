package router

import (
	"github.com/go-chi/chi/v5"
	_ "github.com/jpgomesr/NeuralVault/docs"
	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/chunking/markdown"
	"github.com/jpgomesr/NeuralVault/internal/chunking/text"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/health"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	"github.com/jpgomesr/NeuralVault/internal/retrieval"
	"github.com/jpgomesr/NeuralVault/internal/sourcereader"
	"github.com/jpgomesr/NeuralVault/internal/sources"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
	httpSwagger "github.com/swaggo/http-swagger"
)

func NewRouter(cfg *config.Config, pool storage.Pool, store objectstorage.Client, embedder embedding.Embedder, vectorStore vectorstorage.Client) *chi.Mux {
	r := chi.NewRouter()

	healthService := health.HealthService{}
	healthHandler := health.NewHandler(healthService)

	splitters := map[chunking.ContentType]chunking.Splitter{
		chunking.ContentTypeMarkdown:  markdown.New(),
		chunking.ContentTypePlaintext: text.New(),
	}
	chunkService := chunking.NewChunkService(pool, splitters)
	bus := sources.NewProgressBus()
	sourceService := sources.NewSourceService(pool, store, sourcereader.NewFileReader(), chunkService, bus, embedder, vectorStore, cfg.Qdrant.CollectionName, cfg.Ollama.EmbeddingModel)
	sourceHandler := sources.NewHandler(sourceService, bus)

	retrievalService := retrieval.NewRetrievalService(pool, embedder, vectorStore, cfg.Qdrant.CollectionName)
	retrievalHandler := retrieval.NewHandler(retrievalService)

	r.Route("/", func(r chi.Router) {
		r.Mount("/health", health.Routes(healthHandler))
		r.Mount("/sources", sources.Routes(sourceHandler))
		r.Mount("/query", retrieval.Routes(retrievalHandler))

		// Swagger routes
		r.Get("/swagger/*", httpSwagger.WrapHandler)
	})

	return r
}
