package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tinycdn/internal/app"
	"tinycdn/internal/auth"
	"tinycdn/internal/model"
	"tinycdn/internal/observe"
	"tinycdn/internal/telemetry"
)

const headerRequestID = "X-TinyCDN-Request-ID"

type Router struct {
	service   *app.Service
	telemetry *telemetry.Service
	uiDir     string
	auth      auth.Config
}

type requestMetadata struct {
	RequestID string
	RemoteIP  string
	Actor     string
}

type requestMetadataKey struct{}

type identityKey struct{}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token     string        `json:"token"`
	User      authIdentity  `json:"user"`
	ExpiresAt time.Time     `json:"expires_at"`
	TokenTTL  time.Duration `json:"token_ttl"`
}

type authIdentity struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func NewRouter(service *app.Service, telemetryService *telemetry.Service, uiDir string, authConfig auth.Config) http.Handler {
	router := &Router{service: service, telemetry: telemetryService, uiDir: uiDir, auth: authConfig}

	r := chi.NewRouter()
	r.Use(router.withRequestMetadata)
	r.Get("/healthz", func(rw http.ResponseWriter, req *http.Request) {
		writeJSON(rw, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(api chi.Router) {
		api.Post("/auth/login", router.login)
		api.Group(func(secured chi.Router) {
			secured.Use(router.requireAuth)
			secured.Get("/auth/me", router.me)
			secured.Post("/auth/logout", router.logout)
			secured.Get("/sites", router.listSites)
			secured.Post("/sites", router.createSite)
			secured.Get("/sites/{siteID}", router.getSite)
			secured.Put("/sites/{siteID}", router.updateSite)
			secured.Delete("/sites/{siteID}", router.deleteSite)
			secured.Get("/sites/{siteID}/rules", router.listRules)
			secured.Post("/sites/{siteID}/rules", router.createRule)
			secured.Put("/sites/{siteID}/rules/{ruleID}", router.updateRule)
			secured.Delete("/sites/{siteID}/rules/{ruleID}", router.deleteRule)
			secured.Post("/sites/{siteID}/rules/reorder", router.reorderRules)
			secured.Post("/sites/{siteID}/cache/purge", router.purgeCache)

			secured.Get("/analytics/report", router.analyticsReport)
			secured.Get("/logs/requests", router.requestLogs)
			secured.Get("/logs/audit", router.auditLogs)
			secured.Get("/sites/{siteID}/analytics/report", router.siteAnalyticsReport)
			secured.Get("/sites/{siteID}/logs/requests", router.siteRequestLogs)
			secured.Get("/sites/{siteID}/logs/audit", router.siteAuditLogs)
		})
	})

	r.Handle("/*", router.uiHandler())

	return r
}

func (r *Router) withRequestMetadata(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requestID := uuid.NewString()
		rw.Header().Set(headerRequestID, requestID)
		remoteIP := req.RemoteAddr
		ctx := context.WithValue(req.Context(), requestMetadataKey{}, requestMetadata{
			RequestID: requestID,
			RemoteIP:  remoteIP,
		})
		next.ServeHTTP(rw, req.WithContext(ctx))
	})
}

func metadataFromContext(ctx context.Context) requestMetadata {
	value := ctx.Value(requestMetadataKey{})
	if value == nil {
		return requestMetadata{}
	}
	meta, ok := value.(requestMetadata)
	if !ok {
		return requestMetadata{}
	}
	return meta
}

func identityFromContext(ctx context.Context) (auth.Identity, bool) {
	value := ctx.Value(identityKey{})
	if value == nil {
		return auth.Identity{}, false
	}
	identity, ok := value.(auth.Identity)
	return identity, ok
}

func (r *Router) login(rw http.ResponseWriter, req *http.Request) {
	var payload loginRequest
	if err := decodeJSON(req, &payload); err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}

	identity, err := r.auth.Authenticate(payload.Username, payload.Password)
	if err != nil {
		writeError(rw, http.StatusUnauthorized, errors.New("invalid credentials"))
		return
	}

	token, issued, err := r.auth.Issue(identity, time.Now().UTC())
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err)
		return
	}

	writeJSON(rw, http.StatusOK, authResponse{
		Token: token,
		User: authIdentity{
			Username: issued.Username,
			Role:     issued.Role,
		},
		ExpiresAt: issued.ExpiresAt,
		TokenTTL:  issued.ExpiresAt.Sub(issued.IssuedAt),
	})
}

func (r *Router) me(rw http.ResponseWriter, req *http.Request) {
	identity, ok := identityFromContext(req.Context())
	if !ok {
		writeError(rw, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	writeJSON(rw, http.StatusOK, authIdentity{
		Username: identity.Username,
		Role:     identity.Role,
	})
}

func (r *Router) logout(rw http.ResponseWriter, req *http.Request) {
	rw.WriteHeader(http.StatusNoContent)
}

func (r *Router) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		token, err := bearerToken(req.Header.Get("Authorization"))
		if err != nil {
			writeError(rw, http.StatusUnauthorized, errors.New("missing bearer token"))
			return
		}
		identity, err := r.auth.Verify(token, time.Now().UTC())
		if err != nil {
			writeError(rw, http.StatusUnauthorized, errors.New("invalid bearer token"))
			return
		}
		meta := metadataFromContext(req.Context())
		meta.Actor = identity.Username
		ctx := context.WithValue(req.Context(), requestMetadataKey{}, meta)
		ctx = context.WithValue(ctx, identityKey{}, identity)
		next.ServeHTTP(rw, req.WithContext(ctx))
	})
}

func bearerToken(value string) (string, error) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(value), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("invalid bearer token")
	}
	return strings.TrimSpace(token), nil
}

func (r *Router) recordAudit(req *http.Request, statusCode int, action string, resourceType string, resourceID string, summary string) {
	if r.telemetry == nil {
		return
	}
	meta := metadataFromContext(req.Context())
	r.telemetry.RecordAudit(telemetryAuditEvent(req, meta, statusCode, action, resourceType, resourceID, summary))
}

func telemetryAuditEvent(req *http.Request, meta requestMetadata, statusCode int, action string, resourceType string, resourceID string, summary string) observe.AuditEvent {
	return observe.AuditEvent{
		Timestamp:    time.Now().UTC(),
		RequestID:    meta.RequestID,
		RemoteIP:     meta.RemoteIP,
		Method:       req.Method,
		Path:         req.URL.Path,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Summary:      summary,
		StatusCode:   statusCode,
	}
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
	r.recordAudit(req, http.StatusCreated, "site.created", "site", site.ID, "Created site "+site.Name)
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
	r.recordAudit(req, http.StatusOK, "site.updated", "site", site.ID, "Updated site "+site.Name)
}

func (r *Router) deleteSite(rw http.ResponseWriter, req *http.Request) {
	siteID := chi.URLParam(req, "siteID")
	if err := r.service.DeleteSite(siteID); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	rw.WriteHeader(http.StatusNoContent)
	r.recordAudit(req, http.StatusNoContent, "site.deleted", "site", siteID, "Deleted site "+siteID)
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

	siteID := chi.URLParam(req, "siteID")
	created, err := r.service.CreateRule(siteID, rule)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	writeJSON(rw, http.StatusCreated, created)
	r.recordAudit(req, http.StatusCreated, "rule.created", "rule", siteID+"/"+created.ID, "Created rule "+created.Name)
}

func (r *Router) updateRule(rw http.ResponseWriter, req *http.Request) {
	var rule model.Rule
	if err := decodeJSON(req, &rule); err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}

	siteID := chi.URLParam(req, "siteID")
	ruleID := chi.URLParam(req, "ruleID")
	updated, err := r.service.UpdateRule(siteID, ruleID, rule)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	writeJSON(rw, http.StatusOK, updated)
	r.recordAudit(req, http.StatusOK, "rule.updated", "rule", siteID+"/"+updated.ID, "Updated rule "+updated.Name)
}

func (r *Router) deleteRule(rw http.ResponseWriter, req *http.Request) {
	siteID := chi.URLParam(req, "siteID")
	ruleID := chi.URLParam(req, "ruleID")
	if err := r.service.DeleteRule(siteID, ruleID); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	rw.WriteHeader(http.StatusNoContent)
	r.recordAudit(req, http.StatusNoContent, "rule.deleted", "rule", siteID+"/"+ruleID, "Deleted rule "+ruleID)
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

	siteID := chi.URLParam(req, "siteID")
	rules, err := r.service.ReorderRules(siteID, payload.RuleIDs)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	writeJSON(rw, http.StatusOK, rules)
	r.recordAudit(req, http.StatusOK, "rule.reordered", "site", siteID, "Reordered rules for site "+siteID)
}

func (r *Router) purgeCache(rw http.ResponseWriter, req *http.Request) {
	var payload app.PurgeCacheInput
	if err := decodeJSON(req, &payload); err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}

	siteID := chi.URLParam(req, "siteID")
	result, err := r.service.PurgeCache(req.Context(), siteID, payload)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(rw, status, err)
		return
	}

	writeJSON(rw, http.StatusOK, result)
	summary := "Purged cache for site " + siteID
	if result.Scope == "url" {
		summary = "Purged selected URLs for site " + siteID
	}
	r.recordAudit(req, http.StatusOK, "cache.purged", "site", siteID, summary)
}

func (r *Router) analyticsReport(rw http.ResponseWriter, req *http.Request) {
	r.renderAnalyticsReport(rw, req, "")
}

func (r *Router) siteAnalyticsReport(rw http.ResponseWriter, req *http.Request) {
	r.renderAnalyticsReport(rw, req, chi.URLParam(req, "siteID"))
}

func (r *Router) renderAnalyticsReport(rw http.ResponseWriter, req *http.Request, siteID string) {
	if r.telemetry == nil {
		writeJSON(rw, http.StatusOK, telemetry.Report{GeneratedAt: time.Now().UTC(), Period: "24h"})
		return
	}
	period, err := telemetry.ParsePeriod(req.URL.Query().Get("period"))
	if err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}
	report, err := r.telemetry.Report(req.Context(), telemetry.ReportParams{SiteID: siteID, Period: period})
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err)
		return
	}
	inventory, err := r.service.CacheInventory(req.Context())
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err)
		return
	}
	for _, item := range inventory {
		if siteID != "" && item.SiteID != siteID {
			continue
		}
		report.CacheInventory.Objects += item.Objects
		report.CacheInventory.Bytes += item.Bytes
	}
	writeJSON(rw, http.StatusOK, report)
}

func (r *Router) requestLogs(rw http.ResponseWriter, req *http.Request) {
	r.renderRequestLogs(rw, req, "")
}

func (r *Router) siteRequestLogs(rw http.ResponseWriter, req *http.Request) {
	r.renderRequestLogs(rw, req, chi.URLParam(req, "siteID"))
}

func (r *Router) renderRequestLogs(rw http.ResponseWriter, req *http.Request, siteID string) {
	if r.telemetry == nil {
		writeJSON(rw, http.StatusOK, telemetry.RequestLogPage{})
		return
	}
	period, err := telemetry.ParsePeriod(req.URL.Query().Get("period"))
	if err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}
	page, err := r.telemetry.RequestLogs(req.Context(), telemetry.RequestLogQuery{
		SiteID:          siteID,
		Period:          period,
		Limit:           parseIntDefault(req.URL.Query().Get("limit"), 50),
		Cursor:          req.URL.Query().Get("cursor"),
		Method:          req.URL.Query().Get("method"),
		CacheState:      req.URL.Query().Get("cache_state"),
		StatusClass:     req.URL.Query().Get("status_class"),
		PathPrefix:      req.URL.Query().Get("path_prefix"),
		Search:          req.URL.Query().Get("search"),
		IncludeInternal: req.URL.Query().Get("include_internal") == "true",
	})
	if err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}
	writeJSON(rw, http.StatusOK, page)
}

func (r *Router) auditLogs(rw http.ResponseWriter, req *http.Request) {
	r.renderAuditLogs(rw, req, "")
}

func (r *Router) siteAuditLogs(rw http.ResponseWriter, req *http.Request) {
	r.renderAuditLogs(rw, req, chi.URLParam(req, "siteID"))
}

func (r *Router) renderAuditLogs(rw http.ResponseWriter, req *http.Request, siteID string) {
	if r.telemetry == nil {
		writeJSON(rw, http.StatusOK, telemetry.AuditLogPage{})
		return
	}
	period, err := telemetry.ParsePeriod(req.URL.Query().Get("period"))
	if err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}
	page, err := r.telemetry.AuditLogs(req.Context(), telemetry.AuditLogQuery{
		SiteID: siteID,
		Period: period,
		Limit:  parseIntDefault(req.URL.Query().Get("limit"), 30),
		Cursor: req.URL.Query().Get("cursor"),
	})
	if err != nil {
		writeError(rw, http.StatusBadRequest, err)
		return
	}
	writeJSON(rw, http.StatusOK, page)
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

func parseIntDefault(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
