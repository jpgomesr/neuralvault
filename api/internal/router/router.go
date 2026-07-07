package router

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/jpgomesr/NeuralVault/docs"
	"github.com/jpgomesr/NeuralVault/internal/auth"
	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/chunking/markdown"
	"github.com/jpgomesr/NeuralVault/internal/chunking/text"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/health"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	"github.com/jpgomesr/NeuralVault/internal/retrieval"
	"github.com/jpgomesr/NeuralVault/internal/sourcereader"
	"github.com/jpgomesr/NeuralVault/internal/sources"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
	"github.com/jpgomesr/NeuralVault/internal/workspaces"
	httpSwagger "github.com/swaggo/http-swagger"
)

func NewRouter(cfg *config.Config, pool storage.Pool, store objectstorage.Client, embedder embedding.Embedder, vectorStore vectorstorage.Client, llmProvider llm.Provider, authService auth.Service) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// requestLogging wraps Recoverer so the "request completed" line still records
	// the 500 that Recoverer writes when a downstream handler panics.
	r.Use(requestLogging)
	r.Use(middleware.Recoverer)

	healthService := health.NewHealthService(3*time.Second,
		health.Check{Name: "postgres", Fn: pool.Ping},
		health.Check{Name: "qdrant", Fn: func(ctx context.Context) error {
			_, err := vectorStore.HealthCheck(ctx)
			return err
		}},
		health.Check{Name: "minio", Fn: store.HealthCheck},
		health.Check{Name: "ollama", Fn: embedder.HealthCheck},
		health.Check{Name: "keycloak", Fn: authService.HealthCheck},
	)
	healthHandler := health.NewHandler(healthService)

	splitters := map[chunking.ContentType]chunking.Splitter{
		chunking.ContentTypeMarkdown:  markdown.New(),
		chunking.ContentTypePlaintext: text.New(),
	}
	chunkService := chunking.NewChunkService(pool, splitters)
	bus := sources.NewProgressBus()
	workspaceService := workspaces.NewWorkspaceService(pool)
	workspaceHandler := workspaces.NewHandler(workspaceService)

	sourceService := sources.NewSourceService(pool, store, sourcereader.NewFileReader(), chunkService, bus, embedder, vectorStore, cfg.Qdrant.CollectionName, cfg.Ollama.EmbeddingModel)
	sourceHandler := sources.NewHandler(sourceService, bus, workspaceService, cfg.Server.MaxUploadBytes)

	retrievalService := retrieval.NewRetrievalService(pool, embedder, vectorStore, llmProvider, cfg.Qdrant.CollectionName, cfg.Ollama.CompletionModel)
	retrievalHandler := retrieval.NewHandler(retrievalService, workspaceService)

	authHandler := auth.NewHandler(authService, cfg.Auth.SessionSecret, cfg.Auth.CookieSecure, cfg.Auth.PostLoginURL)

	r.Route("/", func(r chi.Router) {
		// Public routes.
		r.Mount("/health", health.Routes(healthHandler))
		r.Mount("/auth", auth.Routes(authHandler))
		r.Get("/swagger/*", httpSwagger.WrapHandler)

		// Authenticated routes: a valid session is required.
		r.Group(func(r chi.Router) {
			r.Use(authHandler.RequireUser)
			r.Mount("/workspaces", workspaces.Routes(workspaceHandler))
			r.Mount("/sources", sources.Routes(sourceHandler))
			r.Mount("/query", retrieval.Routes(retrievalHandler))
		})
	})

	return r
}
