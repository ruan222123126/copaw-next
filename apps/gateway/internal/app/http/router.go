package transport

import (
	"fmt"
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"nextai/apps/gateway/internal/observability"
)

type PublicHandlers struct {
	Version       stdhttp.HandlerFunc
	Healthz       stdhttp.HandlerFunc
	RuntimeConfig stdhttp.HandlerFunc
}

type Handlers struct {
	Public PublicHandlers
	Agent  AgentHandlers
	Cron   CronHandlers
	Admin  AdminHandlers
}

func NewRouter(apiKey string, handlers Handlers, webHandler stdhttp.HandlerFunc) stdhttp.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(observability.RequestID)
	r.Use(observability.Logging)
	r.Use(cors)

	registerPublicRoutes(r, handlers.Public)

	r.Group(func(api chi.Router) {
		api.Use(observability.APIKey(apiKey))

		registerAgentRoutes(api, handlers.Agent)
		registerCronRoutes(api, handlers.Cron)
		registerAdminRoutes(api, handlers.Admin)
	})

	if webHandler != nil {
		r.Get("/*", webHandler)
	}

	return r
}

func registerPublicRoutes(r chi.Router, handlers PublicHandlers) {
	r.Get("/version", mustHandler("version", handlers.Version))
	r.Get("/healthz", mustHandler("healthz", handlers.Healthz))
	r.Get("/runtime-config", mustHandler("runtime-config", handlers.RuntimeConfig))
}

func cors(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Request-Id,X-NextAI-Source")
		if r.Method == stdhttp.MethodOptions {
			w.WriteHeader(stdhttp.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mustHandler(name string, handler stdhttp.HandlerFunc) stdhttp.HandlerFunc {
	if handler != nil {
		return handler
	}
	panic(fmt.Sprintf("transport router missing handler: %s", name))
}
