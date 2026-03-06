package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"tinycdn/internal/app"
	"tinycdn/internal/model"
)

type Router struct {
	service *app.Service
	uiDir   string
}

func NewRouter(service *app.Service, uiDir string) http.Handler {
	router := &Router{service: service, uiDir: uiDir}

	r := chi.NewRouter()
	r.Get("/healthz", func(rw http.ResponseWriter, req *http.Request) {
		writeJSON(rw, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(api chi.Router) {
		api.Get("/sites", router.listSites)
		api.Post("/sites", router.createSite)
		api.Get("/sites/{siteID}", router.getSite)
		api.Put("/sites/{siteID}", router.updateSite)
		api.Delete("/sites/{siteID}", router.deleteSite)
		api.Get("/sites/{siteID}/rules", router.listRules)
		api.Post("/sites/{siteID}/rules", router.createRule)
		api.Put("/sites/{siteID}/rules/{ruleID}", router.updateRule)
		api.Delete("/sites/{siteID}/rules/{ruleID}", router.deleteRule)
		api.Post("/sites/{siteID}/rules/reorder", router.reorderRules)
	})

	r.Handle("/*", router.uiHandler())

	return r
}

func (r *Router) listSites(rw http.ResponseWriter, req *http.Request) {
	writeJSON(rw, http.StatusOK, r.service.ListSites())
}

func (r *Router) createSite(rw http.ResponseWriter, req *http.Request) {
	var input app.SiteInput
	if err := decodeJSON(req, &input); err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}

	site, err := r.service.CreateSite(input)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}

	writeJSON(rw, http.StatusCreated, site)
}

func (r *Router) getSite(rw http.ResponseWriter, req *http.Request) {
	site, ok := r.service.GetSite(chi.URLParam(req, "siteID"))
	if !ok {
		writeError(rw, http.StatusNotFound, errors.New("site not found"))
		return
	}

	writeJSON(rw, http.StatusOK, site)
}

func (r *Router) updateSite(rw http.ResponseWriter, req *http.Request) {
	var input app.SiteInput
	if err := decodeJSON(req, &input); err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}

	site, err := r.service.UpdateSite(chi.URLParam(req, "siteID"), input)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	writeJSON(rw, http.StatusOK, site)
}

func (r *Router) deleteSite(rw http.ResponseWriter, req *http.Request) {
	if err := r.service.DeleteSite(chi.URLParam(req, "siteID")); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	rw.WriteHeader(http.StatusNoContent)
}

func (r *Router) listRules(rw http.ResponseWriter, req *http.Request) {
	rules, err := r.service.ListRules(chi.URLParam(req, "siteID"))
	if err != nil {
		writeError(rw, http.StatusNotFound, err)
		return
	}

	writeJSON(rw, http.StatusOK, rules)
}

func (r *Router) createRule(rw http.ResponseWriter, req *http.Request) {
	var rule model.Rule
	if err := decodeJSON(req, &rule); err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}
	rule.System = false

	created, err := r.service.CreateRule(chi.URLParam(req, "siteID"), rule)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	writeJSON(rw, http.StatusCreated, created)
}

func (r *Router) updateRule(rw http.ResponseWriter, req *http.Request) {
	var rule model.Rule
	if err := decodeJSON(req, &rule); err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}

	updated, err := r.service.UpdateRule(chi.URLParam(req, "siteID"), chi.URLParam(req, "ruleID"), rule)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	writeJSON(rw, http.StatusOK, updated)
}

func (r *Router) deleteRule(rw http.ResponseWriter, req *http.Request) {
	if err := r.service.DeleteRule(chi.URLParam(req, "siteID"), chi.URLParam(req, "ruleID")); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	rw.WriteHeader(http.StatusNoContent)
}

type reorderRequest struct {
	RuleIDs []string `json:"rule_ids"`
}

func (r *Router) reorderRules(rw http.ResponseWriter, req *http.Request) {
	var payload reorderRequest
	if err := decodeJSON(req, &payload); err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}

	rules, err := r.service.ReorderRules(chi.URLParam(req, "siteID"), payload.RuleIDs)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	writeJSON(rw, http.StatusOK, rules)
}

func (r *Router) uiHandler() http.Handler {
	indexPath := filepath.Join(r.uiDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			writeJSON(rw, http.StatusServiceUnavailable, map[string]string{
				"error":   "frontend assets not found",
				"hint":    "run pnpm --dir web build",
				"ui_path": indexPath,
			})
		})
	}

	files := http.FileServer(http.Dir(r.uiDir))
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			http.NotFound(rw, req)
			return
		}

		target := filepath.Join(r.uiDir, filepath.Clean(req.URL.Path))
		if stat, err := os.Stat(target); err == nil && !stat.IsDir() {
			files.ServeHTTP(rw, req)
			return
		}

		http.ServeFile(rw, req, indexPath)
	})
}

func decodeJSON(req *http.Request, target any) error {
	defer req.Body.Close()

	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(rw http.ResponseWriter, status int, payload any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(payload)
}

func writeError(rw http.ResponseWriter, status int, err error) {
	writeJSON(rw, status, map[string]string{"error": err.Error()})
}
