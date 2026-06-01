package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"

	"ebs-netwatch/internal/monitor"
	"ebs-netwatch/internal/report"
)

type Server struct {
	runner *monitor.Runner
	mux    *http.ServeMux
}

func New(runner *monitor.Runner) *Server {
	s := &Server{
		runner: runner,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	errCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join("web", "index.html"))
	})
	s.mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "style.css"))
	})
	s.mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "app.js"))
	})
	s.mux.HandleFunc("/api/status", s.handleStatus)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(report.Build(s.runner.Snapshot()))
}
