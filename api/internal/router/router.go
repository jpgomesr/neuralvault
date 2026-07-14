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
	"github.com/jpgomesr/NeuralVault/internal/conversations"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/health"
	"github.com/jpgomesr/NeuralVault/internal/modelconfig"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	"github.com/jpgomesr/NeuralVault/internal/reranking"
	"github.com/jpgomesr/NeuralVault/internal/retrieval"
	"github.com/jpgomesr/NeuralVault/internal/sourcereader"
	"github.com/jpgomesr/NeuralVault/internal/sources"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
	"github.com/jpgomesr/NeuralVault/internal/workspaces"
	httpSwagger "github.com/swaggo/http-swagger"
)

// NewRouter wires the dependency graph and mounts every route.
//
// embedder is the server's environment-default embedder, used only for the
// /health check. The embedder and LLM provider that actually serve requests are
// resolved per workspace by modelConfig, since each workspace can bring its own
// provider and key (BYOK).
func NewRouter(cfg *config.Config, pool storage.Pool, store objectstorage.Client, embedder embedding.Embedder, vectorStore vectorstorage.Client, modelConfig *modelconfig.ModelConfigService, reranker reranking.Reranker, authService auth.Service) *chi.Mux {
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
		health.Check{Name: "reranker", Fn: reranker.HealthCheck},
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

	sourceService := sources.NewSourceService(pool, store, sourcereader.NewFileReader(), chunkService, bus, modelConfig, vectorStore, modelConfig)
	sourceHandler := sources.NewHandler(sourceService, bus, workspaceService, cfg.Server.MaxUploadBytes)

	conversationService := conversations.NewConversationService(pool)
	conversationHandler := conversations.NewHandler(conversationService, workspaceService)

	retrievalService := retrieval.NewRetrievalService(pool, modelConfig, vectorStore, modelConfig, reranker)
	retrievalHandler := retrieval.NewHandler(retrievalService, workspaceService, conversationService)

	modelConfigHandler := modelconfig.NewHandler(modelConfig, workspaceService, sourceService)

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
			// Mounted under /workspaces/{workspace_id}/… but declared as its own
			// domain: model configuration is workspace-scoped data, not a
			// workspace attribute.
			r.Mount("/workspaces/{workspace_id}", modelconfig.Routes(modelConfigHandler))
			r.Mount("/sources", sources.Routes(sourceHandler))
			r.Mount("/query", retrieval.Routes(retrievalHandler))
			r.Mount("/conversations", conversations.Routes(conversationHandler))
		})
	})

	return r
}
