package transport

import (
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
)

type AdminHandlers struct {
	ListProviders      stdhttp.HandlerFunc
	GetModelCatalog    stdhttp.HandlerFunc
	ConfigureProvider  stdhttp.HandlerFunc
	DeleteProvider     stdhttp.HandlerFunc
	GetActiveModels    stdhttp.HandlerFunc
	SetActiveModels    stdhttp.HandlerFunc
	ListEnvs           stdhttp.HandlerFunc
	PutEnvs            stdhttp.HandlerFunc
	DeleteEnv          stdhttp.HandlerFunc
	ListSkills         stdhttp.HandlerFunc
	ListAvailableSkill stdhttp.HandlerFunc
	BatchDisableSkills stdhttp.HandlerFunc
	BatchEnableSkills  stdhttp.HandlerFunc
	CreateSkill        stdhttp.HandlerFunc
	DisableSkill       stdhttp.HandlerFunc
	EnableSkill        stdhttp.HandlerFunc
	DeleteSkill        stdhttp.HandlerFunc
	LoadSkillFile      stdhttp.HandlerFunc
	ListWorkspaceFiles stdhttp.HandlerFunc
	GetWorkspaceFile   stdhttp.HandlerFunc
	PutWorkspaceFile   stdhttp.HandlerFunc
	DeleteWorkspace    stdhttp.HandlerFunc
	ExportWorkspace    stdhttp.HandlerFunc
	ImportWorkspace    stdhttp.HandlerFunc
	ListChannels       stdhttp.HandlerFunc
	ListChannelTypes   stdhttp.HandlerFunc
	PutChannels        stdhttp.HandlerFunc
	GetChannel         stdhttp.HandlerFunc
	PutChannel         stdhttp.HandlerFunc
}

func registerAdminRoutes(api chi.Router, handlers AdminHandlers) {
	api.Route("/models", func(r chi.Router) {
		r.Get("/", mustHandler("list-providers", handlers.ListProviders))
		r.Get("/catalog", mustHandler("get-model-catalog", handlers.GetModelCatalog))
		r.Put("/{provider_id}/config", mustHandler("configure-provider", handlers.ConfigureProvider))
		r.Delete("/{provider_id}", mustHandler("delete-provider", handlers.DeleteProvider))
		r.Get("/active", mustHandler("get-active-models", handlers.GetActiveModels))
		r.Put("/active", mustHandler("set-active-models", handlers.SetActiveModels))
	})

	api.Route("/envs", func(r chi.Router) {
		r.Get("/", mustHandler("list-envs", handlers.ListEnvs))
		r.Put("/", mustHandler("put-envs", handlers.PutEnvs))
		r.Delete("/{key}", mustHandler("delete-env", handlers.DeleteEnv))
	})

	api.Route("/skills", func(r chi.Router) {
		r.Get("/", mustHandler("list-skills", handlers.ListSkills))
		r.Get("/available", mustHandler("list-available-skills", handlers.ListAvailableSkill))
		r.Post("/batch-disable", mustHandler("batch-disable-skills", handlers.BatchDisableSkills))
		r.Post("/batch-enable", mustHandler("batch-enable-skills", handlers.BatchEnableSkills))
		r.Post("/", mustHandler("create-skill", handlers.CreateSkill))
		r.Post("/{skill_name}/disable", mustHandler("disable-skill", handlers.DisableSkill))
		r.Post("/{skill_name}/enable", mustHandler("enable-skill", handlers.EnableSkill))
		r.Delete("/{skill_name}", mustHandler("delete-skill", handlers.DeleteSkill))
		r.Get("/{skill_name}/files/{source}/{file_path}", mustHandler("load-skill-file", handlers.LoadSkillFile))
	})

	api.Route("/workspace", func(r chi.Router) {
		r.Get("/files", mustHandler("list-workspace-files", handlers.ListWorkspaceFiles))
		r.Get("/files/*", mustHandler("get-workspace-file", handlers.GetWorkspaceFile))
		r.Put("/files/*", mustHandler("put-workspace-file", handlers.PutWorkspaceFile))
		r.Delete("/files/*", mustHandler("delete-workspace-file", handlers.DeleteWorkspace))
		r.Get("/export", mustHandler("export-workspace", handlers.ExportWorkspace))
		r.Post("/import", mustHandler("import-workspace", handlers.ImportWorkspace))
	})

	api.Route("/config", func(r chi.Router) {
		r.Get("/channels", mustHandler("list-channels", handlers.ListChannels))
		r.Get("/channels/types", mustHandler("list-channel-types", handlers.ListChannelTypes))
		r.Put("/channels", mustHandler("put-channels", handlers.PutChannels))
		r.Get("/channels/{channel_name}", mustHandler("get-channel", handlers.GetChannel))
		r.Put("/channels/{channel_name}", mustHandler("put-channel", handlers.PutChannel))
	})
}
