package p115

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"curio/internal/models"
	"curio/internal/repository"
)

type Service struct {
	store         *repository.Store
	syncMu        sync.Mutex
	cacheMu       sync.Mutex
	directCache   map[string]cachedDirectURL
	authMu        sync.Mutex
	qrSessions    map[string]qrAuthSession
	oauthSessions map[string]oauthSession
}

type cachedDirectURL struct {
	URL       string
	ExpiresAt time.Time
}

type qrAuthSession struct {
	UID          string
	Kind         string
	App          string
	CodeVerifier string
	Time         string
	Sign         string
	ExpiresAt    time.Time
}

type oauthSession struct {
	State       string
	RedirectURI string
	ExpiresAt   time.Time
}

const defaultDirectURLTTL = 5 * time.Minute

func NewService(store *repository.Store) *Service {
	return &Service{
		store:         store,
		directCache:   map[string]cachedDirectURL{},
		qrSessions:    map[string]qrAuthSession{},
		oauthSessions: map[string]oauthSession{},
	}
}

func (s *Service) Status(ctx context.Context) (models.P115Status, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115Status{}, err
	}
	return NewClient(settings).Status(ctx)
}

func (s *Service) StartQRCode(ctx context.Context) (models.P115QRCodeSession, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115QRCodeSession{}, err
	}
	session, err := NewClient(settings).StartCookieQRCode(ctx)
	if err != nil {
		return models.P115QRCodeSession{}, err
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	s.authMu.Lock()
	s.qrSessions[session.UID] = qrAuthSession{
		UID:       session.UID,
		Kind:      authModeCookies,
		App:       NormalizeCookieLoginApp(settings.CookieLoginApp),
		Time:      session.Time,
		Sign:      session.Sign,
		ExpiresAt: expiresAt,
	}
	s.authMu.Unlock()
	return models.P115QRCodeSession{
		UID:       session.UID,
		QRCodeURL: session.QRCodeURL,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) QRCodeStatus(ctx context.Context, uid string) (models.P115QRCodeStatus, error) {
	session, err := s.qrSession(uid)
	if err != nil {
		return models.P115QRCodeStatus{}, err
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115QRCodeStatus{}, err
	}
	status, err := NewClient(settings).OpenQRCodeStatus(ctx, session.UID, session.Time, session.Sign)
	if err != nil {
		return models.P115QRCodeStatus{}, err
	}
	return status, nil
}

func (s *Service) CompleteQRCode(ctx context.Context, uid string) (models.P115AuthResult, error) {
	session, err := s.qrSession(uid)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	client := NewClient(settings)
	if session.Kind == authModeOpen {
		tokens, err := client.OpenDeviceCodeToToken(ctx, session.UID, session.CodeVerifier)
		if err != nil {
			return models.P115AuthResult{}, err
		}
		if err := s.saveOpenTokens(ctx, tokens.AccessToken, tokens.RefreshToken); err != nil {
			return models.P115AuthResult{}, err
		}
	} else {
		cookies, err := client.CookieQRCodeToCookies(ctx, session.UID, session.App)
		if err != nil {
			return models.P115AuthResult{}, err
		}
		if err := s.saveCookies(ctx, cookies); err != nil {
			return models.P115AuthResult{}, err
		}
	}
	s.authMu.Lock()
	delete(s.qrSessions, session.UID)
	s.authMu.Unlock()
	return models.P115AuthResult{Status: "ok", Message: "115 Cookies 已保存"}, nil
}

func (s *Service) StartOAuth(ctx context.Context, fallbackBaseURL string) (models.P115OAuthStart, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115OAuthStart{}, err
	}
	if strings.TrimSpace(settings.AppID) == "" {
		return models.P115OAuthStart{}, errors.New("请先填写 115 App ID")
	}
	if strings.TrimSpace(settings.AppSecret) == "" {
		return models.P115OAuthStart{}, errors.New("请先填写 115 App Secret")
	}
	baseURL := strings.TrimSpace(settings.PublicBaseURL)
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
	redirectURI := joinPublicURL(baseURL, "/api/p115/auth/oauth/callback")
	state, err := randomToken(18)
	if err != nil {
		return models.P115OAuthStart{}, err
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	s.authMu.Lock()
	s.oauthSessions[state] = oauthSession{State: state, RedirectURI: redirectURI, ExpiresAt: expiresAt}
	s.authMu.Unlock()
	return NewClient(settings).OpenOAuthAuthorizeURL(redirectURI, state), nil
}

func (s *Service) CompleteOAuth(ctx context.Context, code, state string) (models.P115AuthResult, error) {
	session, err := s.oauthSession(state)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	tokens, err := NewClient(settings).OpenAuthCodeToToken(ctx, code, session.RedirectURI)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	if err := s.saveOpenTokens(ctx, tokens.AccessToken, tokens.RefreshToken); err != nil {
		return models.P115AuthResult{}, err
	}
	s.authMu.Lock()
	delete(s.oauthSessions, state)
	s.authMu.Unlock()
	return models.P115AuthResult{Status: "ok", Message: "115 OAuth 登录成功"}, nil
}

func (s *Service) RefreshOpenToken(ctx context.Context) (models.P115AuthResult, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	if strings.TrimSpace(settings.RefreshToken) == "" {
		return models.P115AuthResult{}, errors.New("当前没有可刷新的 115 Refresh Token")
	}
	tokens, err := NewClient(settings).RefreshOpenToken(ctx)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	if err := s.saveOpenTokens(ctx, tokens.AccessToken, tokens.RefreshToken); err != nil {
		return models.P115AuthResult{}, err
	}
	return models.P115AuthResult{Status: "ok", Message: "115 令牌已刷新"}, nil
}

func (s *Service) ImportOpenToken(ctx context.Context, accessToken, refreshToken string) (models.P115AuthResult, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return models.P115AuthResult{}, errors.New("请填写 Refresh Token，用于后续刷新授权")
	}
	if err := s.saveOpenTokens(ctx, accessToken, refreshToken); err != nil {
		return models.P115AuthResult{}, err
	}
	return models.P115AuthResult{Status: "ok", Message: "OpenList Token 已导入"}, nil
}

func (s *Service) ExportTree(ctx context.Context) (models.STRMSyncResult, error) {
	return s.runLogged(ctx, models.P115SyncTriggerManualExport, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.exportTree(ctx)
	})
}

func (s *Service) exportTree(ctx context.Context) (models.STRMSyncResult, error) {
	settings, cfg, err := s.settingsAndLibraries(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	client := NewClient(settings)
	result := models.STRMSyncResult{TreeVersion: treeVersion(), Mode: "refresh"}
	for _, lib := range cfg.Libraries {
		items, err := client.ExportTree(ctx, lib)
		if err != nil {
			result.Failed++
			return result, err
		}
		items, snapshot := prepareTreeItems(lib, items, result.TreeVersion)
		if err := s.store.ReplaceP115Snapshot(ctx, lib.CID, result.TreeVersion, snapshot); err != nil {
			return result, err
		}
		cursor := latestCursor(ctx, client, lib.CID, "refresh")
		if err := s.store.ReplaceP115NodesAndCursor(ctx, lib.CID, result.TreeVersion, nodesFromTreeItems(lib, items, result.TreeVersion), cursor); err != nil {
			return result, err
		}
		result.Exported += len(items)
		result.Skipped += countMediaTreeItems(items)
	}
	return result, nil
}

func (s *Service) Sync(ctx context.Context, fallbackBaseURL string) (models.STRMSyncResult, error) {
	return s.runLogged(ctx, models.P115SyncTriggerManualSync, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.sync(ctx, fallbackBaseURL)
	})
}

func (s *Service) SyncScheduled(ctx context.Context) (models.STRMSyncResult, error) {
	return s.runLogged(ctx, models.P115SyncTriggerCron, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.sync(ctx, "")
	})
}

func (s *Service) sync(ctx context.Context, fallbackBaseURL string) (models.STRMSyncResult, error) {
	settings, cfg, err := s.settingsAndLibraries(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	client := NewClient(settings)
	result := models.STRMSyncResult{TreeVersion: treeVersion(), Mode: "sync"}
	for _, lib := range cfg.Libraries {
		items, version, sourceMode, err := s.treeItemsForSync(ctx, client, lib, result.TreeVersion)
		if err != nil {
			result.Failed++
			return result, err
		}
		if version != "" {
			result.TreeVersion = version
		}
		if result.Mode == "sync" && sourceMode != "" {
			result.Mode = sourceMode
		}
		result.Exported += len(items)
		seen := map[string]struct{}{}
		for _, item := range items {
			media := isMediaTreeItem(item)
			if !media {
				continue
			}
			seen[item.RelativePath] = struct{}{}
		}
		existing, err := s.store.ActiveSTRMLinksByLibrary(ctx, models.STRMProvider115, lib.CID)
		if err != nil {
			return result, err
		}
		existingByPath := make(map[string]models.STRMLink, len(existing))
		for _, link := range existing {
			existingByPath[link.RelativePath] = link
		}
		for _, item := range items {
			if !isMediaTreeItem(item) {
				continue
			}
			link, err := s.linkForItem(settings, fallbackBaseURL, lib, item, result.TreeVersion)
			if err != nil {
				result.Failed++
				continue
			}
			old, existed := existingByPath[item.RelativePath]
			if existed && old.PickCode != "" && link.PickCode == "" {
				link.PickCode = old.PickCode
				link.RemoteFileID = old.RemoteFileID
				link.SHA1 = old.SHA1
				link.Size = old.Size
				link.ResolveStatus = old.ResolveStatus
				link.ResolvedAt = old.ResolvedAt
			}
			localExists := fileExists(link.STRMPath)
			if err := writeSTRM(settings.STRMOutputPath, link.STRMPath, link.PlayPath); err != nil {
				result.Failed++
				_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusFailed, models.STRMResolveFailed, "STRM_WRITE_FAILED", err.Error())
				continue
			}
			if err := s.store.UpsertSTRMLink(ctx, link); err != nil {
				result.Failed++
				continue
			}
			if !existed {
				result.Generated++
			} else if !localExists {
				result.Restored++
			} else if old.SourceTreeHash != link.SourceTreeHash || old.STRMPath != link.STRMPath || old.PlayPath != link.PlayPath {
				result.Updated++
			} else {
				result.Skipped++
			}
		}
		for _, link := range existing {
			if _, ok := seen[link.RelativePath]; ok {
				continue
			}
			if err := s.markMissing(ctx, settings, link); err != nil {
				result.Failed++
				continue
			}
			result.Deleted++
		}
		if sourceMode == "scan" || sourceMode == "events" {
			_, snapshot := prepareTreeItems(lib, items, result.TreeVersion)
			if err := s.store.ReplaceP115Snapshot(ctx, lib.CID, result.TreeVersion, snapshot); err != nil {
				return result, err
			}
		}
	}
	if settings.RefreshEmbyAfterSync {
		_ = refreshEmby(ctx, settings)
	}
	return result, nil
}

func (s *Service) Cleanup(ctx context.Context) (models.STRMSyncResult, error) {
	return s.runLogged(ctx, models.P115SyncTriggerManualCleanup, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.cleanup(ctx)
	})
}

func (s *Service) cleanup(ctx context.Context) (models.STRMSyncResult, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	links, err := s.store.STRMLinksByStatuses(ctx, []string{models.STRMStatusStale, models.STRMStatusDeleted, models.STRMStatusFailed})
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	result := models.STRMSyncResult{TreeVersion: treeVersion(), Mode: "cleanup"}
	for _, link := range links {
		if removeManagedSTRM(settings.STRMOutputPath, link.STRMPath) == nil {
			result.Deleted++
		} else {
			result.Failed++
		}
		_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusDeleted, models.STRMResolveStale, "", "")
	}
	return result, nil
}

func (s *Service) SyncRuns(ctx context.Context, limit int) ([]models.P115SyncRun, error) {
	return s.store.P115SyncRuns(ctx, limit)
}

func (s *Service) StartScheduler(ctx context.Context) {
	go s.scheduler(ctx)
}

func (s *Service) scheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runScheduledIfDue(ctx)
		}
	}
}

func (s *Service) runScheduledIfDue(ctx context.Context) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil || !settings.SyncCronEnabled {
		return
	}
	interval := time.Duration(settings.SyncIntervalMinutes) * time.Minute
	if interval < 5*time.Minute {
		interval = 5 * time.Minute
	}
	last, ok, err := s.store.LatestP115SyncRun(ctx, []string{models.P115SyncTriggerManualSync, models.P115SyncTriggerCron})
	if err != nil {
		return
	}
	if ok && time.Since(last.StartedAt) < interval {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()
	_, _ = s.SyncScheduled(runCtx)
}

func (s *Service) runLogged(ctx context.Context, trigger string, fn func(context.Context) (models.STRMSyncResult, error)) (models.STRMSyncResult, error) {
	if !s.syncMu.TryLock() {
		return models.STRMSyncResult{}, errors.New("115 同步正在运行，请稍后再试")
	}
	defer s.syncMu.Unlock()

	id, err := randomToken(12)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	started := time.Now()
	run := models.P115SyncRun{
		ID:        trigger + "_" + id,
		Trigger:   trigger,
		Status:    models.P115SyncStatusRunning,
		StartedAt: started,
	}
	if err := s.store.CreateP115SyncRun(ctx, run); err != nil {
		return models.STRMSyncResult{}, err
	}

	result, runErr := fn(ctx)
	ended := time.Now()
	run.Mode = result.Mode
	run.TreeVersion = result.TreeVersion
	run.Exported = result.Exported
	run.Generated = result.Generated
	run.Restored = result.Restored
	run.Updated = result.Updated
	run.Deleted = result.Deleted
	run.Skipped = result.Skipped
	run.Failed = result.Failed
	run.EndedAt = &ended
	run.DurationMS = ended.Sub(started).Milliseconds()
	switch {
	case runErr != nil:
		run.Status = models.P115SyncStatusFailed
		run.ErrorMessage = runErr.Error()
	case result.Failed > 0:
		run.Status = models.P115SyncStatusPartial
	default:
		run.Status = models.P115SyncStatusSuccess
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.store.FinishP115SyncRun(finishCtx, run); err != nil && runErr == nil {
		return result, err
	}
	return result, runErr
}

func (s *Service) PlayURLForLink(linkID, baseURL string) (string, error) {
	token, err := signPlayToken(linkID, 10*365*24*time.Hour)
	if err != nil {
		return "", err
	}
	return joinPublicURL(baseURL, "/play/115/"+token), nil
}

func (s *Service) PlayURLForLinkName(linkID, baseURL, displayName string) (string, error) {
	name := cleanPlayDisplayName(displayName)
	if name == "" {
		name = path.Join("id", strings.TrimSpace(linkID))
	}
	return joinPublicURLReadable(baseURL, "/play/115/"+name), nil
}

func (s *Service) LinkIDFromToken(token string) (string, error) {
	return verifyPlayToken(token)
}

func (s *Service) ResolvePlayURL(ctx context.Context, token, requestUserAgent string) (string, error) {
	linkID, err := verifyPlayToken(token)
	if err != nil {
		return "", err
	}
	return s.resolvePlayURLByLinkID(ctx, linkID, requestUserAgent)
}

func (s *Service) ResolvePlayURLFromRoute(ctx context.Context, route, baseURL, requestUserAgent string) (string, error) {
	route = cleanPlayDisplayName(route)
	if route == "" {
		return "", errors.New("播放地址无效")
	}
	if linkID, err := verifyPlayToken(route); err == nil {
		return s.resolvePlayURLByLinkID(ctx, linkID, requestUserAgent)
	}
	if strings.HasPrefix(route, "id/") {
		linkID := strings.TrimSpace(strings.TrimPrefix(route, "id/"))
		if linkID != "" {
			return s.resolvePlayURLByLinkID(ctx, linkID, requestUserAgent)
		}
	}
	settings, cfg, err := s.settingsAndLibraries(ctx)
	if err != nil {
		return "", err
	}
	link, err := s.store.STRMLinkByPlayRoute(ctx, models.STRMProvider115, route, playPathCandidates(baseURL, route))
	if err != nil {
		return "", err
	}
	return s.resolvePlayURLForLink(ctx, settings, cfg, link, requestUserAgent)
}

func (s *Service) resolvePlayURLByLinkID(ctx context.Context, linkID, requestUserAgent string) (string, error) {
	settings, cfg, err := s.settingsAndLibraries(ctx)
	if err != nil {
		return "", err
	}
	link, err := s.store.STRMLink(ctx, linkID)
	if err != nil {
		return "", err
	}
	return s.resolvePlayURLForLink(ctx, settings, cfg, link, requestUserAgent)
}

func (s *Service) resolvePlayURLForLink(ctx context.Context, settings models.P115Settings, cfg LibrariesConfig, link models.STRMLink, requestUserAgent string) (string, error) {
	if link.Provider != models.STRMProvider115 || link.Status != models.STRMStatusGenerated {
		return "", errors.New("STRM 链接不可播放")
	}
	if _, ok := libraryCIDs(cfg)[link.LibraryCID]; !ok {
		_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusStale, models.STRMResolveStale, "CID_NOT_CONFIGURED", "115 媒体库 CID 已不在当前配置中")
		return "", errors.New("115 媒体库 CID 已不在当前配置中")
	}
	client := NewClient(settings)
	if strings.TrimSpace(link.PickCode) == "" {
		info, err := client.ResolvePath(ctx, link.LibraryCID, link.RelativePath)
		if err != nil {
			_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, link.Status, models.STRMResolveFailed, "RESOLVE_PATH_FAILED", err.Error())
			return "", err
		}
		link.RemoteFileID = info.ID
		link.PickCode = info.PickCode
		link.SHA1 = info.SHA1
		link.Size = info.Size
		if err := s.store.UpdateSTRMLinkResolved(ctx, link.ID, info.ID, info.PickCode, info.SHA1, info.Size); err != nil {
			return "", err
		}
	}
	ua := userAgent(settings, requestUserAgent)
	if directURL, ok := s.cachedDirectURL(link.PickCode, ua); ok {
		return directURL, nil
	}
	directURL, err := client.DirectURL(ctx, link.PickCode, ua)
	if err != nil {
		_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, link.Status, models.STRMResolveFailed, "DIRECT_URL_FAILED", err.Error())
		return "", err
	}
	s.rememberDirectURL(link.PickCode, ua, directURL, defaultDirectURLTTL)
	return directURL, nil
}

func (s *Service) cachedDirectURL(pickcode, userAgentValue string) (string, bool) {
	key := directCacheKey(pickcode, userAgentValue)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	item, ok := s.directCache[key]
	if !ok || time.Now().After(item.ExpiresAt) {
		delete(s.directCache, key)
		return "", false
	}
	return item.URL, true
}

func (s *Service) rememberDirectURL(pickcode, userAgentValue, directURL string, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	key := directCacheKey(pickcode, userAgentValue)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.directCache[key] = cachedDirectURL{URL: directURL, ExpiresAt: time.Now().Add(ttl)}
}

func (s *Service) qrSession(uid string) (qrAuthSession, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return qrAuthSession{}, errors.New("扫码会话为空")
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	session, ok := s.qrSessions[uid]
	if !ok || time.Now().After(session.ExpiresAt) {
		delete(s.qrSessions, uid)
		return qrAuthSession{}, errors.New("扫码会话已过期，请重新生成二维码")
	}
	return session, nil
}

func (s *Service) oauthSession(state string) (oauthSession, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return oauthSession{}, errors.New("OAuth state 为空")
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	session, ok := s.oauthSessions[state]
	if !ok || time.Now().After(session.ExpiresAt) {
		delete(s.oauthSessions, state)
		return oauthSession{}, errors.New("OAuth 会话已过期，请重新发起登录")
	}
	return session, nil
}

func (s *Service) saveOpenTokens(ctx context.Context, accessToken, refreshToken string) error {
	accessToken = strings.TrimSpace(accessToken)
	refreshToken = strings.TrimSpace(refreshToken)
	if accessToken == "" {
		return errors.New("115 未返回 Access Token")
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return err
	}
	settings.Enabled = true
	settings.AuthMode = authModeOpen
	settings.AccessToken = accessToken
	if refreshToken != "" {
		settings.RefreshToken = refreshToken
	}
	settings.DirectURLTTLSeconds = int(defaultDirectURLTTL / time.Second)
	settings.UserAgentMode = "inherit"
	settings.FixedUserAgent = ""
	if settings.KeepDeletedDays <= 0 {
		settings.KeepDeletedDays = 7
	}
	_, err = s.store.SaveP115Settings(ctx, settings)
	return err
}

func (s *Service) saveCookies(ctx context.Context, cookies string) error {
	cookies = strings.TrimSpace(cookies)
	if cookies == "" {
		return errors.New("115 未返回 Cookies")
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return err
	}
	settings.Enabled = true
	settings.Cookies = cookies
	settings.AuthMode = authModeCookies
	settings.CookieLoginApp = NormalizeCookieLoginApp(settings.CookieLoginApp)
	if settings.KeepDeletedDays <= 0 {
		settings.KeepDeletedDays = 7
	}
	_, err = s.store.SaveP115Settings(ctx, settings)
	return err
}

func (s *Service) settingsAndLibraries(ctx context.Context) (models.P115Settings, LibrariesConfig, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115Settings{}, LibrariesConfig{}, err
	}
	if !settings.Enabled {
		return models.P115Settings{}, LibrariesConfig{}, errors.New("115 播放未启用")
	}
	cfg, err := ParseLibraries(settings.LibrariesYAML)
	if err != nil {
		return models.P115Settings{}, LibrariesConfig{}, err
	}
	if strings.TrimSpace(settings.STRMOutputPath) == "" {
		return models.P115Settings{}, LibrariesConfig{}, errors.New("STRM 输出目录不能为空")
	}
	return settings, cfg, nil
}

func (s *Service) linkForItem(settings models.P115Settings, fallbackBaseURL string, lib LibraryConfig, item TreeItem, version string) (models.STRMLink, error) {
	strmPath, err := strmPathFor(settings.STRMOutputPath, lib.OutputPrefix, item.RelativePath)
	if err != nil {
		return models.STRMLink{}, err
	}
	id := stableLinkID(lib.CID, item.RelativePath)
	baseURL := playBaseURL(settings, fallbackBaseURL)
	displayName := item.RelativePath
	if strings.TrimSpace(displayName) == "" {
		displayName = item.Name
	}
	playPath, err := s.PlayURLForLinkName(id, baseURL, displayName)
	if err != nil {
		return models.STRMLink{}, err
	}
	resolveStatus := models.STRMResolvePending
	var resolvedAt *time.Time
	if strings.TrimSpace(item.PickCode) != "" {
		now := time.Now()
		resolveStatus = models.STRMResolveResolved
		resolvedAt = &now
	}
	now := time.Now()
	return models.STRMLink{
		ID:             id,
		Provider:       models.STRMProvider115,
		LibraryCID:     lib.CID,
		LibraryName:    lib.Name,
		LibraryType:    lib.Type,
		RelativePath:   item.RelativePath,
		RemotePath:     "/" + path.Join(lib.Name, item.RelativePath),
		RemoteFileID:   item.RemoteFileID,
		PickCode:       item.PickCode,
		SHA1:           item.SHA1,
		Size:           item.Size,
		STRMPath:       strmPath,
		PlayPath:       playPath,
		SourceTreeHash: item.SourceTreeHash,
		TreeVersion:    version,
		ResolveStatus:  resolveStatus,
		Status:         models.STRMStatusGenerated,
		GeneratedAt:    now,
		ResolvedAt:     resolvedAt,
		UpdatedAt:      now,
	}, nil
}

func (s *Service) treeItemsForSync(ctx context.Context, client *Client, lib LibraryConfig, version string) ([]TreeItem, string, string, error) {
	nodeCount, err := s.store.P115NodeCount(ctx, lib.CID)
	if err != nil {
		return nil, "", "", err
	}
	if nodeCount > 0 {
		nodes, nodeVersion, err := s.nodesWithLifeEvents(ctx, client, lib, version)
		if err == nil {
			if nodeVersion != "" {
				version = nodeVersion
			}
			return treeItemsFromNodes(nodes), version, "events", nil
		}
	}
	items, scanErr := client.ExportTree(ctx, lib)
	if scanErr == nil {
		items, _ = prepareTreeItems(lib, items, version)
		nodes := nodesFromTreeItems(lib, items, version)
		if err := s.store.ReplaceP115NodesAndCursor(ctx, lib.CID, version, nodes, latestCursor(ctx, client, lib.CID, "scan")); err != nil {
			return nil, "", "", err
		}
		return items, version, "scan", nil
	}
	if nodeCount > 0 {
		nodes, nodeVersion, err := s.store.P115Nodes(ctx, lib.CID, true)
		if err != nil {
			return nil, "", "", err
		}
		if nodeVersion != "" {
			version = nodeVersion
		}
		return treeItemsFromNodes(nodes), version, "cache", nil
	}
	snapshot, snapshotVersion, err := s.store.P115Snapshot(ctx, lib.CID)
	if err != nil {
		return nil, "", "", err
	}
	if len(snapshot) > 0 {
		return treeItemsFromSnapshot(snapshot), snapshotVersion, "snapshot", nil
	}
	return nil, "", "", scanErr
}

func (s *Service) nodesWithLifeEvents(ctx context.Context, client *Client, lib LibraryConfig, version string) ([]models.P115Node, string, error) {
	nodes, nodeVersion, err := s.store.P115Nodes(ctx, lib.CID, false)
	if err != nil {
		return nil, "", err
	}
	cursor, err := s.store.P115EventCursor(ctx, lib.CID)
	if err != nil {
		return nil, "", err
	}
	if cursor.LastEventID == 0 && cursor.LastEventTime == 0 {
		cursor = latestCursor(ctx, client, lib.CID, "init")
		if cursor.LastSyncStatus == "error" {
			if err := s.store.SaveP115EventCursor(ctx, cursor); err != nil {
				return nil, "", err
			}
			return nil, "", errors.New(cursor.LastSyncError)
		}
		if err := s.store.SaveP115EventCursor(ctx, cursor); err != nil {
			return nil, "", err
		}
		return aliveNodes(nodes), nodeVersion, nil
	}
	batch, err := client.LifeEventsBatch(ctx, cursor.LastEventID, cursor.LastEventTime, 20)
	if err != nil {
		saveErr := s.store.SaveP115EventCursor(ctx, models.P115EventCursor{
			LibraryCID:     lib.CID,
			LastEventID:    cursor.LastEventID,
			LastEventTime:  cursor.LastEventTime,
			LastSyncStatus: "error",
			LastSyncError:  err.Error(),
		})
		if saveErr != nil {
			return nil, "", saveErr
		}
		return nil, "", err
	}
	events := batch.Events
	nextCursor := advanceCursorWithBatch(cursor, batch)
	nextCursor.LibraryCID = lib.CID
	nextCursor.LastSyncStatus = "ok"
	nextCursor.LastSyncError = ""
	if len(events) == 0 {
		if err := s.store.SaveP115EventCursor(ctx, nextCursor); err != nil {
			return nil, "", err
		}
		return aliveNodes(nodes), nodeVersion, nil
	}
	sort.SliceStable(events, func(i, j int) bool {
		left, right := eventApplyPriority(events[i]), eventApplyPriority(events[j])
		if left != right {
			return left < right
		}
		return events[i].ID < events[j].ID
	})
	nextVersion := version
	if nextVersion == "" {
		nextVersion = treeVersion()
	}
	updated, changed := applyLifeEventsToNodes(lib, nodes, events, nextVersion)
	if changed {
		if err := s.store.ReplaceP115NodesAndCursor(ctx, lib.CID, nextVersion, updated, nextCursor); err != nil {
			return nil, "", err
		}
		return aliveNodes(updated), nextVersion, nil
	}
	if err := s.store.SaveP115EventCursor(ctx, nextCursor); err != nil {
		return nil, "", err
	}
	return aliveNodes(updated), nodeVersion, nil
}

func latestCursor(ctx context.Context, client *Client, libraryCID, status string) models.P115EventCursor {
	id, eventTime, err := client.LatestLifeEventCursor(ctx)
	cursor := models.P115EventCursor{
		LibraryCID:     libraryCID,
		LastEventID:    id,
		LastEventTime:  eventTime,
		LastSyncStatus: status,
	}
	if err != nil {
		cursor.LastSyncStatus = "error"
		cursor.LastSyncError = err.Error()
	}
	return cursor
}

func eventsNeedTreeScan(events []LifeEvent) bool {
	return false
}

func eventApplyPriority(event LifeEvent) int {
	if eventCreatesDirectory(event) {
		return 0
	}
	return 1
}

func eventCreatesDirectory(event LifeEvent) bool {
	switch event.Type {
	case 17, 18, 20:
		return true
	case 5, 6:
		if strings.TrimSpace(event.PickCode) != "" || strings.TrimSpace(event.SHA1) != "" || event.Size > 0 {
			return false
		}
		name := strings.TrimSpace(event.Name)
		return name == "" || !mediaExtension(path.Ext(name))
	default:
		return false
	}
}

func advanceCursor(cursor models.P115EventCursor, events []LifeEvent) models.P115EventCursor {
	for _, event := range events {
		if event.ID > cursor.LastEventID {
			cursor.LastEventID = event.ID
		}
		if event.UpdateTime > cursor.LastEventTime {
			cursor.LastEventTime = event.UpdateTime
		}
	}
	return cursor
}

func advanceCursorWithBatch(cursor models.P115EventCursor, batch LifeEventBatch) models.P115EventCursor {
	if batch.LastEventID > cursor.LastEventID {
		cursor.LastEventID = batch.LastEventID
	}
	if batch.LastEventTime > cursor.LastEventTime {
		cursor.LastEventTime = batch.LastEventTime
	}
	if batch.LastEventID == 0 && batch.LastEventTime == 0 {
		return advanceCursor(cursor, batch.Events)
	}
	return cursor
}

func applyLifeEventsToNodes(lib LibraryConfig, nodes []models.P115Node, events []LifeEvent, version string) ([]models.P115Node, bool) {
	byID := make(map[string]*models.P115Node, len(nodes)+len(events))
	out := make([]models.P115Node, 0, len(nodes)+len(events))
	for _, node := range nodes {
		if strings.TrimSpace(node.RemoteFileID) == "" {
			continue
		}
		node.LibraryCID = lib.CID
		out = append(out, node)
		byID[node.RemoteFileID] = &out[len(out)-1]
	}
	changed := false
	for _, event := range events {
		if event.FileID == "" {
			continue
		}
		node, exists := byID[event.FileID]
		if event.Type == 22 {
			if exists && node.IsAlive {
				node.IsAlive = false
				node.TreeVersion = version
				changed = true
			}
			continue
		}
		parentInside := event.ParentID == lib.CID
		if !parentInside {
			if parent, ok := byID[event.ParentID]; ok && parent.IsAlive {
				parentInside = true
			}
		}
		if exists && !parentInside {
			if node.IsAlive {
				node.IsAlive = false
				node.TreeVersion = version
				changed = true
			}
			continue
		}
		if !parentInside {
			continue
		}
		name := strings.TrimSpace(event.Name)
		if name == "" && exists {
			name = node.Name
		}
		if name == "" {
			continue
		}
		if !exists {
			newNode := models.P115Node{LibraryCID: lib.CID, RemoteFileID: event.FileID}
			out = append(out, newNode)
			node = &out[len(out)-1]
			byID[event.FileID] = node
		}
		node.LibraryCID = lib.CID
		node.TreeVersion = version
		node.ParentFileID = event.ParentID
		node.Name = name
		node.IsAlive = true
		node.IsDirectory = eventIsDirectory(event, *node)
		if event.PickCode != "" {
			node.PickCode = event.PickCode
		}
		if event.SHA1 != "" || node.IsDirectory {
			node.SHA1 = event.SHA1
		}
		if event.Size > 0 || node.IsDirectory {
			node.Size = event.Size
		}
		changed = true
	}
	if rebuildNodePaths(lib, out, version) {
		changed = true
	}
	return out, changed
}

func eventIsDirectory(event LifeEvent, existing models.P115Node) bool {
	if eventCreatesDirectory(event) {
		return true
	}
	if event.SHA1 != "" || event.Size > 0 {
		return false
	}
	return existing.IsDirectory
}

func rebuildNodePaths(lib LibraryConfig, nodes []models.P115Node, version string) bool {
	byID := make(map[string]*models.P115Node, len(nodes))
	for i := range nodes {
		byID[nodes[i].RemoteFileID] = &nodes[i]
	}
	state := map[string]bool{}
	visiting := map[string]bool{}
	var changed bool
	var compute func(string) (string, bool)
	compute = func(id string) (string, bool) {
		node, ok := byID[id]
		if !ok || !node.IsAlive {
			return "", false
		}
		if alive, ok := state[id]; ok {
			return node.RelativePath, alive
		}
		if visiting[id] {
			node.IsAlive = false
			changed = true
			state[id] = false
			return "", false
		}
		visiting[id] = true
		defer delete(visiting, id)
		var rel string
		if node.ParentFileID == "" || node.ParentFileID == lib.CID {
			rel = node.Name
		} else {
			parentRel, alive := compute(node.ParentFileID)
			if !alive {
				node.IsAlive = false
				node.TreeVersion = version
				changed = true
				state[id] = false
				return "", false
			}
			rel = path.Join(parentRel, node.Name)
		}
		if node.RelativePath != rel {
			node.RelativePath = rel
			changed = true
		}
		node.IsMedia = mediaExtension(path.Ext(node.Name)) && !node.IsDirectory
		hash := sourceHash(lib.CID, node.RelativePath, node.RemoteFileID, node.PickCode, node.SHA1, node.Size)
		if node.SourceTreeHash != hash {
			node.SourceTreeHash = hash
			changed = true
		}
		if version != "" && node.TreeVersion != version {
			node.TreeVersion = version
			changed = true
		}
		state[id] = node.IsAlive
		return node.RelativePath, node.IsAlive
	}
	for i := range nodes {
		compute(nodes[i].RemoteFileID)
	}
	return changed
}

func nodesFromTreeItems(lib LibraryConfig, items []TreeItem, version string) []models.P115Node {
	nodes := make([]models.P115Node, 0, len(items))
	for _, item := range items {
		if item.RemoteFileID == "" {
			continue
		}
		nodes = append(nodes, models.P115Node{
			LibraryCID:     lib.CID,
			TreeVersion:    version,
			RemoteFileID:   item.RemoteFileID,
			ParentFileID:   item.ParentFileID,
			RelativePath:   item.RelativePath,
			Name:           item.Name,
			PickCode:       item.PickCode,
			SHA1:           item.SHA1,
			Size:           item.Size,
			IsDirectory:    item.IsDirectory,
			IsMedia:        isMediaTreeItem(item),
			IsAlive:        true,
			SourceTreeHash: item.SourceTreeHash,
		})
	}
	return nodes
}

func aliveNodes(nodes []models.P115Node) []models.P115Node {
	out := make([]models.P115Node, 0, len(nodes))
	for _, node := range nodes {
		if node.IsAlive {
			out = append(out, node)
		}
	}
	return out
}

func treeItemsFromNodes(nodes []models.P115Node) []TreeItem {
	items := make([]TreeItem, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsAlive {
			continue
		}
		items = append(items, TreeItem{
			RelativePath:   node.RelativePath,
			Name:           node.Name,
			RemoteFileID:   node.RemoteFileID,
			ParentFileID:   node.ParentFileID,
			PickCode:       node.PickCode,
			SHA1:           node.SHA1,
			Size:           node.Size,
			Depth:          pathDepth(node.RelativePath),
			IsDirectory:    node.IsDirectory,
			SourceTreeHash: node.SourceTreeHash,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].RelativePath < items[j].RelativePath })
	return items
}

func pathDepth(value string) int {
	value = strings.Trim(value, "/")
	if value == "" {
		return 0
	}
	return strings.Count(value, "/") + 1
}

func prepareTreeItems(lib LibraryConfig, items []TreeItem, version string) ([]TreeItem, []models.P115TreeSnapshotItem) {
	prepared := make([]TreeItem, 0, len(items))
	snapshot := make([]models.P115TreeSnapshotItem, 0, len(items))
	for _, item := range items {
		item.SourceTreeHash = sourceHash(lib.CID, item.RelativePath, item.RemoteFileID, item.PickCode, item.SHA1, item.Size)
		media := isMediaTreeItem(item)
		extension := strings.TrimPrefix(strings.ToLower(path.Ext(item.Name)), ".")
		prepared = append(prepared, item)
		snapshot = append(snapshot, models.P115TreeSnapshotItem{
			LibraryCID:     lib.CID,
			TreeVersion:    version,
			RelativePath:   item.RelativePath,
			Name:           item.Name,
			RemoteFileID:   item.RemoteFileID,
			ParentFileID:   item.ParentFileID,
			PickCode:       item.PickCode,
			SHA1:           item.SHA1,
			Size:           item.Size,
			Extension:      extension,
			Depth:          item.Depth,
			IsDirectory:    item.IsDirectory,
			IsMedia:        media,
			SourceTreeHash: item.SourceTreeHash,
		})
	}
	return prepared, snapshot
}

func treeItemsFromSnapshot(snapshot []models.P115TreeSnapshotItem) []TreeItem {
	items := make([]TreeItem, 0, len(snapshot))
	for _, item := range snapshot {
		isDirectory := item.IsDirectory || (!item.IsMedia && item.Extension == "")
		items = append(items, TreeItem{
			RelativePath:   item.RelativePath,
			Name:           item.Name,
			RemoteFileID:   item.RemoteFileID,
			ParentFileID:   item.ParentFileID,
			PickCode:       item.PickCode,
			SHA1:           item.SHA1,
			Size:           item.Size,
			Depth:          item.Depth,
			IsDirectory:    isDirectory,
			SourceTreeHash: item.SourceTreeHash,
		})
	}
	return items
}

func countMediaTreeItems(items []TreeItem) int {
	count := 0
	for _, item := range items {
		if isMediaTreeItem(item) {
			count++
		}
	}
	return count
}

func playBaseURL(settings models.P115Settings, fallbackBaseURL string) string {
	if value := strings.TrimSpace(settings.PublicBaseURL); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.EmbyPublicURL); value != "" {
		return value
	}
	return fallbackBaseURL
}

func (s *Service) markMissing(ctx context.Context, settings models.P115Settings, link models.STRMLink) error {
	if settings.DeleteMissingSTRM && !(settings.StaleBeforeDelete && link.Status == models.STRMStatusGenerated) {
		if err := removeManagedSTRM(settings.STRMOutputPath, link.STRMPath); err != nil {
			return err
		}
		return s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusDeleted, models.STRMResolveStale, "", "")
	}
	return s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusStale, models.STRMResolveStale, "", "")
}

func isMediaTreeItem(item TreeItem) bool {
	if item.IsDirectory {
		return false
	}
	return mediaExtension(path.Ext(item.Name))
}

func stableLinkID(libraryCID, relativePath string) string {
	sum := sha256.Sum256([]byte(models.STRMProvider115 + ":" + libraryCID + ":" + strings.ToLower(relativePath)))
	return hex.EncodeToString(sum[:])
}

func sourceHash(libraryCID, relativePath, fileID, pickcode, sha1 string, size int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s:%s:%d", libraryCID, relativePath, fileID, pickcode, sha1, size)))
	return hex.EncodeToString(sum[:])
}

func directCacheKey(pickcode, userAgentValue string) string {
	sum := sha256.Sum256([]byte(userAgentValue))
	return pickcode + ":" + hex.EncodeToString(sum[:])
}

func refreshEmby(ctx context.Context, settings models.P115Settings) error {
	upstream := strings.TrimRight(strings.TrimSpace(settings.EmbyUpstreamURL), "/")
	if upstream == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream+"/Library/Refresh", nil)
	if err != nil {
		return err
	}
	if key := strings.TrimSpace(settings.EmbyAPIKey); key != "" {
		req.Header.Set("X-Emby-Token", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Emby 刷新失败：HTTP %d", resp.StatusCode)
	}
	return nil
}

func treeVersion() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}

func strmPathFor(root, outputPrefix, relativePath string) (string, error) {
	relativePath = strings.Trim(strings.ReplaceAll(relativePath, "\\", "/"), "/")
	if relativePath == "" || strings.Contains(relativePath, "../") || strings.HasPrefix(relativePath, "..") {
		return "", errors.New("115 STRM 相对路径无效")
	}
	ext := path.Ext(relativePath)
	strmRel := strings.TrimSuffix(relativePath, ext) + ".strm"
	parts := []string{root}
	for _, part := range splitRelativePath(outputPrefix) {
		parts = append(parts, part)
	}
	for _, part := range splitRelativePath(strmRel) {
		parts = append(parts, part)
	}
	target := filepath.Join(parts...)
	if !insideRoot(root, target) {
		return "", errors.New("STRM 路径越界")
	}
	return target, nil
}

func writeSTRM(root, target, playPath string) error {
	if strings.TrimSpace(playPath) == "" {
		return errors.New("STRM 播放地址为空")
	}
	if !insideRoot(root, target) {
		return errors.New("STRM 路径越界")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(playPath+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func fileExists(target string) bool {
	info, err := os.Stat(target)
	return err == nil && !info.IsDir()
}

func removeManagedSTRM(root, target string) error {
	if strings.TrimSpace(target) == "" {
		return nil
	}
	if !insideRoot(root, target) {
		return errors.New("拒绝删除 STRM 输出目录外的文件")
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	removeEmptyParents(filepath.Dir(target), root)
	return nil
}

func removeEmptyParents(start, root string) {
	current, err := filepath.Abs(start)
	if err != nil {
		return
	}
	stop, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for current != stop && insideRoot(stop, current) {
		if err := os.Remove(current); err != nil {
			return
		}
		next := filepath.Dir(current)
		if next == current {
			return
		}
		current = next
	}
}

func insideRoot(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func joinPublicURL(baseURL, playPath string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return playPath
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/") + playPath
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + playPath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func joinPublicURLWithQuery(baseURL, playPath string, query url.Values) string {
	u := &url.URL{Path: playPath, RawQuery: query.Encode()}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return u.String()
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/") + u.String()
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + playPath
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func joinPublicURLReadable(baseURL, playPath string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	playPath = "/" + strings.TrimLeft(playPath, "/")
	playPath = escapeReadablePath(playPath)
	if baseURL == "" {
		return playPath
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL + playPath
	}
	prefix := parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.Path, "/")
	return prefix + playPath
}

func escapeReadablePath(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case r == '/':
			builder.WriteRune(r)
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r >= 0x80:
			builder.WriteRune(r)
		case strings.ContainsRune("-._~!$&'()*+,;=:@", r):
			builder.WriteRune(r)
		default:
			for _, b := range []byte(string(r)) {
				builder.WriteString(fmt.Sprintf("%%%02X", b))
			}
		}
	}
	return builder.String()
}

func cleanPlayDisplayName(displayName string) string {
	displayName = strings.Trim(strings.ReplaceAll(displayName, "\\", "/"), "/")
	if decoded, err := url.PathUnescape(displayName); err == nil {
		displayName = decoded
	}
	parts := splitRelativePath(displayName)
	if len(parts) == 0 {
		return ""
	}
	for _, part := range parts {
		if part == ".." {
			return ""
		}
	}
	return path.Join(parts...)
}

func playPathCandidates(baseURL, route string) []string {
	route = cleanPlayDisplayName(route)
	if route == "" {
		return nil
	}
	playPath := "/play/115/" + route
	escapedPath := (&url.URL{Path: playPath}).String()
	values := []string{
		joinPublicURLReadable(baseURL, playPath),
		joinPublicURL(baseURL, playPath),
		playPath,
		escapedPath,
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func newCodeChallenge() (string, string, error) {
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier := hex.EncodeToString(raw)
	if len(verifier) > 128 {
		verifier = verifier[:128]
	}
	if len(verifier) < 43 {
		return "", "", errors.New("PKCE code verifier 生成失败")
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.StdEncoding.EncodeToString(sum[:]), nil
}

func randomToken(size int) (string, error) {
	if size <= 0 {
		size = 18
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
