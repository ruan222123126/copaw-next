package transport

import (
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
)

type CronHandlers struct {
	ListCronJobs  stdhttp.HandlerFunc
	CreateCronJob stdhttp.HandlerFunc
	GetCronJob    stdhttp.HandlerFunc
	UpdateCronJob stdhttp.HandlerFunc
	DeleteCronJob stdhttp.HandlerFunc
	PauseCronJob  stdhttp.HandlerFunc
	ResumeCronJob stdhttp.HandlerFunc
	RunCronJob    stdhttp.HandlerFunc
	GetCronState  stdhttp.HandlerFunc
}

func registerCronRoutes(api chi.Router, handlers CronHandlers) {
	api.Route("/cron", func(r chi.Router) {
		r.Get("/jobs", mustHandler("list-cron-jobs", handlers.ListCronJobs))
		r.Post("/jobs", mustHandler("create-cron-job", handlers.CreateCronJob))
		r.Get("/jobs/{job_id}", mustHandler("get-cron-job", handlers.GetCronJob))
		r.Put("/jobs/{job_id}", mustHandler("update-cron-job", handlers.UpdateCronJob))
		r.Delete("/jobs/{job_id}", mustHandler("delete-cron-job", handlers.DeleteCronJob))
		r.Post("/jobs/{job_id}/pause", mustHandler("pause-cron-job", handlers.PauseCronJob))
		r.Post("/jobs/{job_id}/resume", mustHandler("resume-cron-job", handlers.ResumeCronJob))
		r.Post("/jobs/{job_id}/run", mustHandler("run-cron-job", handlers.RunCronJob))
		r.Get("/jobs/{job_id}/state", mustHandler("get-cron-job-state", handlers.GetCronState))
	})
}
