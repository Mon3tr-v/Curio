package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"curio/internal/classifier"
	"curio/internal/clouddrive"
	"curio/internal/embyproxy"
	"curio/internal/models"
	"curio/internal/naming"
	"curio/internal/p115"
	"curio/internal/repository"
	"curio/internal/scraper"
	"curio/internal/worker"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"
)

type API struct {
	store      *repository.Store
	worker     *worker.Service
	scraper    *scraper.Client
	redis      *redis.Client
	p115       *p115.Service
	adminToken string
}

type rearchivePayload struct {
	TMDBID        int    `json:"tmdb_id"`
	MediaType     string `json:"media_type"`
	Season        int    `json:"season"`
	Episode       int    `json:"episode"`
	SeasonOffset  int    `json:"season_offset"`
	EpisodeOffset int    `json:"episode_offset"`
}

func (p rearchivePayload) options() worker.RearchiveOptions {
	return worker.RearchiveOptions{
		TMDBID:        p.TMDBID,
		MediaType:     p.MediaType,
		Season:        p.Season,
		Episode:       p.Episode,
		SeasonOffset:  p.SeasonOffset,
		EpisodeOffset: p.EpisodeOffset,
	}
}

func New(store *repository.Store, workerService *worker.Service, scraperClient *scraper.Client, redisClient *redis.Client, allowedOrigin, frontendDir string) http.Handler {
	p115Service := p115.NewService(store)
	return NewWithP115(store, workerService, scraperClient, redisClient, p115Service, allowedOrigin, frontendDir, "")
}

func NewWithP115(store *repository.Store, workerService *worker.Service, scraperClient *scraper.Client, redisClient *redis.Client, p115Service *p115.Service, allowedOrigin, frontendDir, adminToken string) http.Handler {
	api := &API{store: store, worker: workerService, scraper: scraperClient, redis: redisClient, p115: p115Service, adminToken: strings.TrimSpace(adminToken)}
	router := chi.NewRouter()
	router.Use(recoverJSON)
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{allowedOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Curio-Token"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	router.Use(api.authMiddleware)
	router.Get("/api/auth/status", api.authStatus)
	router.Post("/api/auth/login", api.authLogin)
	router.Get("/api/health", api.health)
	router.Get("/api/events", api.events)
	router.Post("/api/scan/start", api.startScan)
	router.Post("/api/scan/clouddrive/start", api.startCloudDriveScan)
	router.Get("/api/tasks/active", api.activeTask)
	router.Post("/api/tasks/{batchID}/stop", api.stopTask)
	router.Get("/api/batches", api.batches)
	router.Get("/api/batches/{batchID}", api.batch)
	router.Get("/api/stats", api.stats)
	router.Get("/api/settings/directories", api.directories)
	router.Put("/api/settings/directories", api.saveDirectories)
	router.Get("/api/settings/system", api.systemSettings)
	router.Put("/api/settings/system", api.saveSystemSettings)
	router.Get("/api/settings/clouddrive", api.cloudDriveSettings)
	router.Put("/api/settings/clouddrive", api.saveCloudDriveSettings)
	router.Get("/api/settings/p115", api.p115Settings)
	router.Put("/api/settings/p115", api.saveP115Settings)
	router.Post("/api/p115/auth/qrcode/start", api.startP115QRCode)
	router.Get("/api/p115/auth/qrcode/{uid}/status", api.p115QRCodeStatus)
	router.Post("/api/p115/auth/qrcode/complete", api.completeP115QRCode)
	router.Post("/api/p115/auth/oauth/start", api.startP115OAuth)
	router.Get("/api/p115/auth/oauth/callback", api.p115OAuthCallback)
	router.Post("/api/p115/auth/import-token", api.importP115Token)
	router.Post("/api/p115/auth/refresh", api.refreshP115Token)
	router.Post("/api/p115/test", api.testP115)
	router.Post("/api/p115/export-tree", api.exportP115Tree)
	router.Post("/api/p115/strm/sync", api.syncP115STRM)
	router.Post("/api/p115/strm/cleanup", api.cleanupP115STRM)
	router.Get("/api/p115/sync-runs", api.p115SyncRuns)
	router.Get("/api/settings/classification", api.classification)
	router.Put("/api/settings/classification", api.saveClassification)
	router.Get("/api/settings/templates", api.templates)
	router.Put("/api/settings/templates/{templateType}", api.saveTemplate)
	router.Post("/api/settings/templates/preview", api.previewTemplate)
	router.Get("/api/media-files", api.mediaFiles)
	router.Post("/api/media-files/bulk-delete", api.bulkDeleteMediaFiles)
	router.Post("/api/media-files/bulk-rearchive", api.bulkRearchiveMediaFiles)
	router.Delete("/api/media-files/{fileID}", api.deleteMediaFile)
	router.Post("/api/media-files/{fileID}/rearchive", api.rearchiveMediaFile)
	router.Post("/api/clouddrive/test", api.testCloudDrive)
	router.Get("/api/clouddrive/files", api.cloudDriveFiles)
	router.Get("/api/staging", api.staging)
	router.Get("/api/failed", api.failed)
	router.Get("/api/tv-shows", api.tvShows)
	router.Get("/api/tv-shows/{showID}", api.tvShow)
	router.Get("/api/collections", api.collections)
	router.Get("/api/collections/{collectionID}", api.collection)
	router.Get("/play/115/*", api.play115)
	router.Head("/play/115/*", api.play115)
	router.Mount("/emby", embyproxy.New(store, p115Service))
	if strings.TrimSpace(frontendDir) != "" {
		router.Handle("/*", spa(frontendDir))
	}
	return router
}

func (a *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.adminToken == "" || !strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		token := strings.TrimSpace(r.Header.Get("X-Curio-Token"))
		if token == "" {
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
				token = strings.TrimSpace(auth[7:])
			}
		}
		if token == "" && r.URL.Path == "/api/events" {
			token = strings.TrimSpace(r.URL.Query().Get("token"))
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(a.adminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "需要 Curio 管理令牌")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) authStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"enabled": a.adminToken != ""})
}

func (a *API) authLogin(w http.ResponseWriter, r *http.Request) {
	if a.adminToken == "" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "enabled": false})
		return
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(payload.Token)), []byte(a.adminToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "管理令牌无效")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "enabled": true})
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前连接不支持实时事件")
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprint(w, "data: {}\n\n")
			flusher.Flush()
		}
	}
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	dbStatus := "ok"
	if err := a.store.Ping(ctx); err != nil {
		dbStatus = err.Error()
	}
	redisStatus := "ok"
	if a.redis != nil {
		if err := a.redis.Ping(ctx).Err(); err != nil {
			redisStatus = err.Error()
		}
	}
	queues := map[string]int64{}
	for _, queue := range []string{"queue:scan", "queue:parse", "queue:scrape", "queue:match", "queue:collection_check", "queue:organize", "queue:failed"} {
		if a.redis != nil {
			queues[queue] = a.redis.LLen(ctx, queue).Val()
		}
	}
	active, ok, _ := a.worker.ActiveBatch(ctx)
	var activePayload any
	if ok {
		activePayload = active
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "database": dbStatus, "redis": redisStatus, "queues": queues, "active_task": activePayload})
}

func (a *API) startScan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batchID, err := a.worker.StartScan(ctx)
	if err != nil {
		if errors.Is(err, worker.ErrTaskAlreadyRunning) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"batch_id": batchID, "status": "started"})
}

func (a *API) startCloudDriveScan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batchID, err := a.worker.StartCloudDriveScan(ctx)
	if err != nil {
		if errors.Is(err, worker.ErrTaskAlreadyRunning) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, worker.ErrCloudDriveNotReady) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"batch_id": batchID, "status": "started"})
}

func (a *API) activeTask(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batch, ok, err := a.worker.ActiveBatch(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (a *API) stopTask(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batch, err := a.worker.Stop(ctx, chi.URLParam(r, "batchID"))
	if err != nil {
		if errors.Is(err, worker.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (a *API) batch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batch, err := a.store.Batch(ctx, chi.URLParam(r, "batchID"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (a *API) batches(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batches, err := a.store.Batches(ctx, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, batches)
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	stats, err := a.store.MediaStats(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) directories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	dirs, err := a.store.Directories(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dirs)
}

func (a *API) saveDirectories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var dirs models.DirectoryConfig
	if err := decodeJSON(w, r, &dirs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized, err := validateDirectories(dirs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.SaveDirectories(ctx, normalized); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, normalized)
}

func (a *API) systemSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.Settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, redactSystemSettings(settings))
}

func (a *API) saveSystemSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var settings models.SystemSettings
	if err := decodeJSON(w, r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	settings.TMDBAPIKey = strings.TrimSpace(settings.TMDBAPIKey)
	settings.NetworkProxy = strings.TrimSpace(settings.NetworkProxy)
	current, err := a.store.Settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mergeHiddenSystemSettings(&settings, current)
	settings.ClassificationYAML = current.ClassificationYAML
	if settings.NetworkProxy != "" {
		parsed, err := url.Parse(settings.NetworkProxy)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			writeError(w, http.StatusBadRequest, "网络代理地址无效")
			return
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			writeError(w, http.StatusBadRequest, "网络代理协议必须是 http 或 https")
			return
		}
	}
	saved, err := a.store.SaveSettings(ctx, settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.scraper.Configure(saved.TMDBAPIKey, saved.NetworkProxy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, redactSystemSettings(saved))
}

func (a *API) cloudDriveSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.CloudDriveSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, redactCloudDriveSettings(settings))
}

func (a *API) saveCloudDriveSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var settings models.CloudDriveSettings
	if err := decodeJSON(w, r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if existing, err := a.store.CloudDriveSettings(ctx); err == nil {
		mergeHiddenCloudDriveSettings(&settings, existing)
	}
	normalized, err := validateCloudDriveSettings(settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := a.store.SaveCloudDriveSettings(ctx, normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, redactCloudDriveSettings(saved))
}

func (a *API) p115Settings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.P115Settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings = normalizeEmbyPortSettings(settings)
	settings.CookieLoginApp = p115.NormalizeCookieLoginApp(settings.CookieLoginApp)
	writeJSON(w, http.StatusOK, redactP115Settings(settings))
}

func (a *API) saveP115Settings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var settings models.P115Settings
	if err := decodeJSON(w, r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if existing, err := a.store.P115Settings(ctx); err == nil {
		mergeHiddenP115Settings(&settings, existing)
	}
	normalized, err := validateP115Settings(settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := a.store.SaveP115Settings(ctx, normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, redactP115Settings(saved))
}

func (a *API) startP115QRCode(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	session, err := a.p115.StartQRCode(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (a *API) p115QRCodeStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	status, err := a.p115.QRCodeStatus(ctx, chi.URLParam(r, "uid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) completeP115QRCode(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		UID string `json:"uid"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.p115.CompleteQRCode(ctx, payload.UID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) startP115OAuth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	result, err := a.p115.StartOAuth(ctx, requestBaseURL(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) p115OAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" {
		writeHTML(w, http.StatusBadRequest, "115 OAuth 登录失败：缺少 code")
		return
	}
	result, err := a.p115.CompleteOAuth(ctx, code, state)
	if err != nil {
		writeHTML(w, http.StatusBadRequest, "115 OAuth 登录失败："+err.Error())
		return
	}
	writeHTML(w, http.StatusOK, result.Message+"，可以关闭此页面")
}

func (a *API) importP115Token(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.p115.ImportOpenToken(ctx, payload.AccessToken, payload.RefreshToken)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) refreshP115Token(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	result, err := a.p115.RefreshOpenToken(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) testP115(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	status, err := a.p115.Status(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) exportP115Tree(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	result, err := a.p115.ExportTree(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) syncP115STRM(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Minute)
	defer cancel()
	result, err := a.p115.Sync(ctx, requestBaseURL(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) cleanupP115STRM(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	result, err := a.p115.Cleanup(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) p115SyncRuns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := a.p115.SyncRuns(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (a *API) play115(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	var directURL string
	var err error
	if token != "" {
		if _, err := a.p115.LinkIDFromToken(token); err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		directURL, err = a.p115.ResolvePlayURL(ctx, token, r.UserAgent())
	} else {
		route := strings.TrimSpace(chi.URLParam(r, "*"))
		directURL, err = a.p115.ResolvePlayURLFromRoute(ctx, route, requestBaseURL(r), r.UserAgent())
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", directURL)
	w.WriteHeader(http.StatusFound)
}

func (a *API) classification(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.Settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"classification_yaml": settings.ClassificationYAML})
}

func (a *API) saveClassification(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		ClassificationYAML string `json:"classification_yaml"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := classifier.Parse(payload.ClassificationYAML); err != nil {
		writeError(w, http.StatusBadRequest, "分类 YAML 无效："+err.Error())
		return
	}
	saved, err := a.store.SaveClassification(ctx, payload.ClassificationYAML)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"classification_yaml": saved.ClassificationYAML})
}

func (a *API) templates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	templates, err := a.store.Templates(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (a *API) saveTemplate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	templateType := chi.URLParam(r, "templateType")
	var payload models.NamingTemplate
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := naming.Validate(templateType, payload.Template); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(payload.Name) == "" {
		payload.Name = templateType
	}
	payload.TemplateType = templateType
	payload.Enabled = true
	if err := a.store.SaveTemplate(ctx, payload); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *API) previewTemplate(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		TemplateType string `json:"template_type"`
		Template     string `json:"template"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview, err := naming.Preview(payload.TemplateType, payload.Template)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"preview": preview})
}

func (a *API) mediaFiles(w http.ResponseWriter, r *http.Request) {
	a.mediaByStatus(w, r, "")
}

func (a *API) staging(w http.ResponseWriter, r *http.Request) {
	a.mediaByStatus(w, r, models.StatusDone)
}

func (a *API) failed(w http.ResponseWriter, r *http.Request) {
	a.mediaByStatus(w, r, models.StatusFailed)
}

func (a *API) mediaByStatus(w http.ResponseWriter, r *http.Request, status string) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, offset := paginationFromRequest(r)
	if status == "" {
		status = strings.TrimSpace(r.URL.Query().Get("status"))
	}
	files, err := a.store.ListMediaFiles(ctx, status, strings.TrimSpace(r.URL.Query().Get("q")), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func paginationFromRequest(r *http.Request) (int, int) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= 200 {
			limit = value
		}
	}
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			offset = value
		}
	}
	return limit, offset
}

func (a *API) bulkDeleteMediaFiles(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		FileIDs []string `json:"file_ids"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids := cleanIDs(payload.FileIDs)
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "媒体文件 ID 不能为空")
		return
	}
	batchIDs, err := a.store.DeleteMediaFiles(ctx, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, batchID := range batchIDs {
		_ = a.store.RecountBatch(ctx, batchID)
	}
	_ = a.store.RefreshCollectionLocalCounts(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "count": len(ids)})
}

func (a *API) deleteMediaFile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	fileID := strings.TrimSpace(chi.URLParam(r, "fileID"))
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "媒体文件 ID 不能为空")
		return
	}
	file, err := a.store.MediaFile(ctx, fileID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := a.store.DeleteMediaFile(ctx, fileID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.store.RecountBatch(ctx, file.BatchID)
	_ = a.store.RefreshCollectionLocalCounts(ctx)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *API) rearchiveMediaFile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	var payload rearchivePayload
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, err := a.worker.RearchiveMedia(ctx, chi.URLParam(r, "fileID"), payload.options())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (a *API) bulkRearchiveMediaFiles(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()
	var payload struct {
		FileIDs []string `json:"file_ids"`
		rearchivePayload
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids := cleanIDs(payload.FileIDs)
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "媒体文件 ID 不能为空")
		return
	}
	files, failures, err := a.worker.RearchiveMediaBatch(ctx, ids, payload.options())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": files, "count": len(files), "failed": len(failures), "errors": failures})
}

func (a *API) testCloudDrive(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.CloudDriveSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, err := clouddrive.New(settings).Test(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) cloudDriveFiles(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.CloudDriveSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir := r.URL.Query().Get("path")
	if strings.TrimSpace(dir) == "" {
		dir = settings.RootPath
	}
	files, err := clouddrive.New(settings).List(ctx, dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (a *API) collections(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, offset := paginationFromRequest(r)
	collections, err := a.store.Collections(ctx, strings.TrimSpace(r.URL.Query().Get("q")), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, collections)
}

func (a *API) tvShows(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, offset := paginationFromRequest(r)
	shows, err := a.store.TVShows(ctx, strings.TrimSpace(r.URL.Query().Get("q")), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, shows)
}

func (a *API) tvShow(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id, err := strconv.Atoi(chi.URLParam(r, "showID"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "剧集 ID 无效")
		return
	}
	show, err := a.store.TVShow(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if needsTVShowRefresh(show) {
		if refreshed, episodes, refreshErr := a.scraper.RefreshTVShow(ctx, id); refreshErr == nil {
			if saveErr := a.store.UpsertTVShow(ctx, refreshed); saveErr == nil {
				episodeIDs := make([]string, 0, len(episodes))
				for _, episode := range episodes {
					if err := a.store.UpsertTVEpisode(ctx, episode); err != nil {
						saveErr = err
						break
					}
					episodeIDs = append(episodeIDs, episode.ID)
				}
				if saveErr == nil && len(episodes) >= refreshed.EpisodeCount {
					_ = a.store.DeleteTVEpisodesNotIn(ctx, refreshed.TMDBID, episodeIDs)
				}
			}
			if next, err := a.store.TVShow(ctx, id); err == nil {
				show = next
			}
		}
	}
	writeJSON(w, http.StatusOK, show)
}

func needsTVShowRefresh(show models.TVShowStatus) bool {
	known := show.ReleasedEpisodeCount + show.UnreleasedEpisodeCount
	return (show.EpisodeCount > 0 && known != show.EpisodeCount) || (known == 0 && show.SeasonCount > 0)
}

func (a *API) collection(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	id, err := strconv.Atoi(chi.URLParam(r, "collectionID"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "合集 ID 无效")
		return
	}
	collection, err := a.store.Collection(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func validateDirectories(dirs models.DirectoryConfig) (models.DirectoryConfig, error) {
	values := map[string]string{
		"incoming_path":               dirs.IncomingPath,
		"staging_path":                dirs.StagingPath,
		"failed_path":                 dirs.FailedPath,
		"incomplete_collections_path": dirs.IncompleteCollectionsPath,
	}
	labels := map[string]string{
		"incoming_path":               "入库目录",
		"staging_path":                "整理目录",
		"failed_path":                 "失败目录",
		"incomplete_collections_path": "缺失合集目录",
	}
	normalized := map[string]string{}
	for key, value := range values {
		clean, err := normalizePath(value)
		if err != nil {
			return models.DirectoryConfig{}, errors.New(labels[key] + ": " + err.Error())
		}
		if err := ensureReadWrite(clean); err != nil {
			return models.DirectoryConfig{}, errors.New(labels[key] + ": " + err.Error())
		}
		normalized[key] = clean
	}
	seen := map[string]struct{}{}
	for key, value := range normalized {
		if _, ok := seen[value]; ok {
			return models.DirectoryConfig{}, errors.New(labels[key] + ": 目录路径不能相同")
		}
		seen[value] = struct{}{}
	}
	incoming := normalized["incoming_path"]
	for _, key := range []string{"staging_path", "failed_path", "incomplete_collections_path"} {
		if isInside(incoming, normalized[key]) {
			return models.DirectoryConfig{}, errors.New(labels[key] + ": 目录不能位于入库目录内")
		}
	}
	return models.DirectoryConfig{
		IncomingPath:              normalized["incoming_path"],
		StagingPath:               normalized["staging_path"],
		FailedPath:                normalized["failed_path"],
		IncompleteCollectionsPath: normalized["incomplete_collections_path"],
	}, nil
}

func validateCloudDriveSettings(settings models.CloudDriveSettings) (models.CloudDriveSettings, error) {
	settings.Address = strings.TrimSpace(settings.Address)
	if settings.Address == "" {
		settings.Address = "http://localhost:19798"
	}
	if parsed, err := url.Parse(settings.Address); err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return models.CloudDriveSettings{}, errors.New("CloudDrive2 地址协议必须是 http 或 https")
		}
		if parsed.Host == "" {
			return models.CloudDriveSettings{}, errors.New("CloudDrive2 地址缺少主机名")
		}
	}
	settings.Username = strings.TrimSpace(settings.Username)
	settings.Password = strings.TrimSpace(settings.Password)
	settings.Token = strings.TrimSpace(settings.Token)
	settings.RootPath = clouddrive.NormalizePath(settings.RootPath)
	settings.StagingPath = clouddrive.NormalizePath(settings.StagingPath)
	settings.FailedPath = clouddrive.NormalizePath(settings.FailedPath)
	settings.IncompleteCollectionsPath = clouddrive.NormalizePath(settings.IncompleteCollectionsPath)
	if settings.StagingPath == settings.RootPath || settings.FailedPath == settings.RootPath || settings.IncompleteCollectionsPath == settings.RootPath {
		return models.CloudDriveSettings{}, errors.New("CloudDrive2 输出目录不能等于扫描根目录")
	}
	if settings.StagingPath == settings.FailedPath || settings.StagingPath == settings.IncompleteCollectionsPath || settings.FailedPath == settings.IncompleteCollectionsPath {
		return models.CloudDriveSettings{}, errors.New("CloudDrive2 输出目录不能相同")
	}
	return settings, nil
}

const hiddenSecretValue = "********"

func isHiddenSecret(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && strings.Trim(value, "*") == ""
}

func redactedSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return hiddenSecretValue
}

func redactSystemSettings(settings models.SystemSettings) models.SystemSettings {
	settings.TMDBAPIKey = redactedSecret(settings.TMDBAPIKey)
	settings.CloudDrivePassword = redactedSecret(settings.CloudDrivePassword)
	settings.CloudDriveToken = redactedSecret(settings.CloudDriveToken)
	return settings
}

func mergeHiddenSystemSettings(next *models.SystemSettings, existing models.SystemSettings) {
	if isHiddenSecret(next.TMDBAPIKey) {
		next.TMDBAPIKey = existing.TMDBAPIKey
	}
	if isHiddenSecret(next.CloudDrivePassword) {
		next.CloudDrivePassword = existing.CloudDrivePassword
	}
	if isHiddenSecret(next.CloudDriveToken) {
		next.CloudDriveToken = existing.CloudDriveToken
	}
}

func redactCloudDriveSettings(settings models.CloudDriveSettings) models.CloudDriveSettings {
	settings.Password = redactedSecret(settings.Password)
	settings.Token = redactedSecret(settings.Token)
	return settings
}

func mergeHiddenCloudDriveSettings(next *models.CloudDriveSettings, existing models.CloudDriveSettings) {
	if isHiddenSecret(next.Password) {
		next.Password = existing.Password
	}
	if isHiddenSecret(next.Token) {
		next.Token = existing.Token
	}
}

func redactP115Settings(settings models.P115Settings) models.P115Settings {
	settings.AppSecret = redactedSecret(settings.AppSecret)
	settings.Cookies = redactedSecret(settings.Cookies)
	settings.EmbyAPIKey = redactedSecret(settings.EmbyAPIKey)
	return settings
}

func mergeHiddenP115Settings(next *models.P115Settings, existing models.P115Settings) {
	if next.AuthMode == "" {
		next.AuthMode = existing.AuthMode
	}
	if isHiddenSecret(next.AppSecret) {
		next.AppSecret = existing.AppSecret
	}
	if isHiddenSecret(next.Cookies) {
		next.Cookies = existing.Cookies
	}
	if isHiddenSecret(next.EmbyAPIKey) {
		next.EmbyAPIKey = existing.EmbyAPIKey
	}
	if next.AccessToken == "" {
		next.AccessToken = existing.AccessToken
	}
	if next.RefreshToken == "" {
		next.RefreshToken = existing.RefreshToken
	}
	if next.DirectURLTTLSeconds == 0 {
		next.DirectURLTTLSeconds = existing.DirectURLTTLSeconds
	}
	if next.CookieLoginApp == "" {
		next.CookieLoginApp = existing.CookieLoginApp
	}
	if next.UserAgentMode == "" {
		next.UserAgentMode = existing.UserAgentMode
	}
	if next.FixedUserAgent == "" {
		next.FixedUserAgent = existing.FixedUserAgent
	}
	if next.KeepDeletedDays == 0 {
		next.KeepDeletedDays = existing.KeepDeletedDays
	}
}

func validateP115Settings(settings models.P115Settings) (models.P115Settings, error) {
	settings.Enabled = true
	settings.AppID = strings.TrimSpace(settings.AppID)
	settings.AppSecret = strings.TrimSpace(settings.AppSecret)
	settings.AccessToken = strings.TrimSpace(settings.AccessToken)
	settings.RefreshToken = strings.TrimSpace(settings.RefreshToken)
	settings.Cookies = strings.TrimSpace(settings.Cookies)
	settings.CookieLoginApp = p115.NormalizeCookieLoginApp(settings.CookieLoginApp)
	settings.AuthMode = strings.ToLower(strings.TrimSpace(settings.AuthMode))
	if settings.AuthMode == "" {
		if settings.Cookies != "" {
			settings.AuthMode = "cookies"
		} else if settings.AccessToken != "" || settings.RefreshToken != "" {
			settings.AuthMode = "open"
		} else {
			settings.AuthMode = "open"
		}
	}
	if settings.AuthMode == "cookies" && settings.Cookies == "" && (settings.AccessToken != "" || settings.RefreshToken != "") {
		settings.AuthMode = "open"
	}
	if settings.AuthMode != "cookies" && settings.AuthMode != "open" {
		return models.P115Settings{}, errors.New("115 授权方式必须是 cookies 或 open")
	}
	settings.STRMOutputPath = strings.TrimSpace(settings.STRMOutputPath)
	if settings.STRMOutputPath == "" {
		settings.STRMOutputPath = "/data/Curio/strm"
	}
	settings.PublicBaseURL = strings.TrimRight(strings.TrimSpace(settings.PublicBaseURL), "/")
	settings.EmbyPublicURL = strings.TrimRight(strings.TrimSpace(settings.EmbyPublicURL), "/")
	for label, raw := range map[string]string{"播放外部地址": settings.PublicBaseURL, "Emby 对外地址": settings.EmbyPublicURL} {
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return models.P115Settings{}, errors.New(label + "无效")
		}
	}
	settings.DirectURLTTLSeconds = 300
	settings.UserAgentMode = "inherit"
	settings.FixedUserAgent = ""
	settings.LibrariesYAML = strings.TrimSpace(settings.LibrariesYAML)
	if settings.LibrariesYAML != "" {
		if _, err := p115.ParseLibraries(settings.LibrariesYAML); err != nil {
			return models.P115Settings{}, errors.New("115 媒体库 CID 配置无效：" + err.Error())
		}
	}
	if settings.KeepDeletedDays <= 0 {
		settings.KeepDeletedDays = 7
	}
	if settings.SyncIntervalMinutes <= 0 {
		settings.SyncIntervalMinutes = 60
	}
	if settings.SyncIntervalMinutes < 5 || settings.SyncIntervalMinutes > 10080 {
		return models.P115Settings{}, errors.New("115 定时同步间隔必须在 5 到 10080 分钟之间")
	}
	settings.EmbyUpstreamURL = strings.TrimRight(strings.TrimSpace(settings.EmbyUpstreamURL), "/")
	if settings.EmbyUpstreamURL != "" {
		parsed, err := url.Parse(settings.EmbyUpstreamURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return models.P115Settings{}, errors.New("Emby 原始地址无效")
		}
	}
	if settings.EmbyProxyPort == 0 {
		settings.EmbyProxyPort = 8097
	}
	if settings.EmbyProxyPort < 1 || settings.EmbyProxyPort > 65535 {
		return models.P115Settings{}, errors.New("Emby 反代端口无效")
	}
	settings.EmbyProxyBasePath = "/" + strings.Trim(strings.TrimSpace(settings.EmbyProxyBasePath), "/")
	if settings.EmbyProxyBasePath == "/" {
		settings.EmbyProxyBasePath = "/emby"
	}
	settings.EmbyAPIKey = strings.TrimSpace(settings.EmbyAPIKey)
	return normalizeEmbyPortSettings(settings), nil
}

func normalizeEmbyPortSettings(settings models.P115Settings) models.P115Settings {
	if settings.EmbyProxyPort <= 0 {
		settings.EmbyProxyPort = 8097
	}
	parsed, err := url.Parse(strings.TrimSpace(settings.EmbyPublicURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return settings
	}
	rewrite := strings.TrimRight(parsed.Path, "/") == "/emby"
	if upstream, err := url.Parse(strings.TrimSpace(settings.EmbyUpstreamURL)); err == nil {
		rewrite = rewrite || (upstream.Scheme != "" &&
			strings.EqualFold(parsed.Hostname(), upstream.Hostname()) &&
			parsed.Port() != "" &&
			parsed.Port() == upstream.Port())
	}
	if !rewrite {
		return settings
	}
	if host := parsed.Hostname(); host != "" {
		parsed.Host = net.JoinHostPort(host, strconv.Itoa(settings.EmbyProxyPort))
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	settings.EmbyPublicURL = strings.TrimRight(parsed.String(), "/")
	return settings
}

func clampInt(value, min, max, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func cleanIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func normalizePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("路径不能为空")
	}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", errors.New("路径包含非法跳转")
		}
	}
	clean := filepath.Clean(value)
	if !filepath.IsAbs(clean) {
		abs, err := filepath.Abs(clean)
		if err != nil {
			return "", err
		}
		clean = abs
	}
	return clean, nil
}

func ensureReadWrite(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(path, ".curio-rw-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}

func isInside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func contextWithTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 10*time.Second)
}

func recoverJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("api panic: %v\n%s", recovered, debug.Stack())
				writeError(w, http.StatusInternalServerError, "服务处理请求时异常退出，请查看后端日志")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func requestBaseURL(r *http.Request) string {
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeHTML(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(message)
	_, _ = fmt.Fprintf(w, "<!doctype html><meta charset=\"utf-8\"><title>Curio</title><body style=\"font-family:system-ui,sans-serif;padding:32px;color:#202124\">%s</body>", escaped)
}

func spa(dir string) http.HandlerFunc {
	files := http.FileServer(http.Dir(dir))
	return func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(r.URL.Path)
		fullPath := filepath.Join(dir, cleanPath)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	}
}
