package p115

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"curio/internal/models"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type TreeItem struct {
	RelativePath   string
	Name           string
	RemoteFileID   string
	ParentFileID   string
	PickCode       string
	SHA1           string
	Size           int64
	Depth          int
	IsDirectory    bool
	SourceTreeHash string
}

type FileInfo struct {
	ID          string
	ParentID    string
	Name        string
	PickCode    string
	SHA1        string
	Size        int64
	IsDirectory bool
}

type LifeEvent struct {
	ID         int64
	Type       int
	EventName  string
	FileID     string
	ParentID   string
	Name       string
	PickCode   string
	SHA1       string
	Size       int64
	CreateTime int64
	UpdateTime int64
}

type LifeEventBatch struct {
	Events        []LifeEvent
	LastEventID   int64
	LastEventTime int64
	RawCount      int
}

type openQRCodeSession struct {
	UID       string
	QRCodeURL string
	Time      string
	Sign      string
}

type openTokenPair struct {
	AccessToken  string
	RefreshToken string
}

type rateLimitError struct {
	message string
}

func (e rateLimitError) Error() string { return e.message }

var behaviorTypeNames = map[int]string{
	1:  "upload_image_file",
	2:  "upload_file",
	3:  "star_image",
	4:  "star_file",
	5:  "move_image_file",
	6:  "move_file",
	7:  "browse_image",
	8:  "browse_video",
	9:  "browse_audio",
	10: "browse_document",
	14: "receive_files",
	17: "new_folder",
	18: "copy_folder",
	20: "folder_rename",
	22: "delete_file",
	23: "copy_file",
	24: "file_rename",
}

var behaviorNameTypes = func() map[string]int {
	out := make(map[string]int, len(behaviorTypeNames))
	for code, name := range behaviorTypeNames {
		out[name] = code
	}
	return out
}()

var ignoredBehaviorTypes = map[int]struct{}{
	3: {}, 4: {}, 7: {}, 8: {}, 9: {}, 10: {}, 19: {},
}

var recentOperationDetailTypes = map[string]struct{}{
	"move_image_file": {},
	"move_file":       {},
	"copy_folder":     {},
	"folder_rename":   {},
	"copy_file":       {},
	"file_rename":     {},
}

const lifeEventLookbackSeconds = 3600

var p115RequestThrottle = newP115Throttle(500 * time.Millisecond)

type Client struct {
	settings models.P115Settings
	http     *http.Client
}

func NewClient(settings models.P115Settings) *Client {
	return &Client{
		settings: settings,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) Status(ctx context.Context) (models.P115Status, error) {
	status := models.P115Status{
		Ready:     c.settings.Enabled,
		CanExport: c.hasOpenToken() || c.hasCookies(),
		CanPlay:   c.hasOpenToken() || c.hasCookies(),
	}
	if !c.settings.Enabled {
		status.Message = "115 播放未启用"
		return status, nil
	}
	if c.hasCookies() {
		if _, err := c.listCookie(ctx, "0"); err == nil {
			status.Ready = true
			status.Message = "115 Cookies 已连接"
			return status, nil
		} else if !c.hasOpenToken() {
			return status, err
		}
	}
	if c.hasOpenToken() {
		payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://proapi.115.com/open/user/info", nil, true, "")
		if err != nil {
			return status, err
		}
		status.Ready = true
		status.UserName = firstString(payload, "user_name", "name", "nickname", "uid")
		status.Message = "115 Open 已连接"
		return status, nil
	}
	status.Ready = false
	status.Message = "请配置 115 Cookies 或 Open Access Token"
	return status, nil
}

func (c *Client) StartOpenQRCode(ctx context.Context, codeChallenge string) (openQRCodeSession, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(c.settings.AppID))
	form.Set("code_challenge", codeChallenge)
	form.Set("code_challenge_method", "sha256")
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://qrcodeapi.115.com/open/authDeviceCode", form, false, "")
	if err != nil {
		return openQRCodeSession{}, err
	}
	if err := payloadError(payload); err != nil {
		return openQRCodeSession{}, err
	}
	data := asMap(payload["data"])
	if len(data) == 0 {
		data = payload
	}
	uid := firstString(data, "uid", "qrcode_uid")
	if uid == "" {
		return openQRCodeSession{}, errors.New("115 未返回扫码会话 UID")
	}
	return openQRCodeSession{
		UID:       uid,
		QRCodeURL: "https://qrcodeapi.115.com/api/1.0/web/1.0/qrcode?uid=" + url.QueryEscape(uid),
		Time:      firstString(data, "time", "timestamp"),
		Sign:      firstString(data, "sign", "signature"),
	}, nil
}

func (c *Client) StartCookieQRCode(ctx context.Context) (openQRCodeSession, error) {
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://qrcodeapi.115.com/api/1.0/web/1.0/token/", nil, false, "")
	if err != nil {
		return openQRCodeSession{}, err
	}
	if err := payloadError(payload); err != nil {
		return openQRCodeSession{}, err
	}
	data := asMap(payload["data"])
	if len(data) == 0 {
		data = payload
	}
	uid := firstString(data, "uid", "qrcode_uid")
	if uid == "" {
		return openQRCodeSession{}, errors.New("115 未返回扫码会话 UID")
	}
	return openQRCodeSession{
		UID:       uid,
		QRCodeURL: "https://qrcodeapi.115.com/api/1.0/web/1.0/qrcode?uid=" + url.QueryEscape(uid),
		Time:      firstString(data, "time", "timestamp"),
		Sign:      firstString(data, "sign", "signature"),
	}, nil
}

func (c *Client) OpenQRCodeStatus(ctx context.Context, uid, timeValue, sign string) (models.P115QRCodeStatus, error) {
	query := url.Values{}
	query.Set("uid", uid)
	if timeValue != "" {
		query.Set("time", timeValue)
	}
	if sign != "" {
		query.Set("sign", sign)
	}
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://qrcodeapi.115.com/get/status/", query, false, "")
	if err != nil {
		return models.P115QRCodeStatus{}, err
	}
	data := asMap(payload["data"])
	if len(data) == 0 {
		data = payload
	}
	status := firstString(data, "status", "code", "state")
	return models.P115QRCodeStatus{
		UID:     uid,
		Status:  status,
		Message: qrStatusMessage(status, responseMessage(payload)),
	}, nil
}

func (c *Client) CookieQRCodeToCookies(ctx context.Context, uid, app string) (string, error) {
	app = NormalizeCookieLoginApp(app)
	form := url.Values{}
	form.Set("account", strings.TrimSpace(uid))
	form.Set("app", app)
	var lastErr error
	for _, endpoint := range []string{
		"https://passportapi.115.com/app/1.0/" + app + "/1.0/login/qrcode/",
		"https://qrcodeapi.115.com/app/1.0/" + app + "/1.0/login/qrcode/",
	} {
		payload, header, err := c.requestJSON(ctx, http.MethodPost, endpoint, form, false, "")
		if err != nil {
			lastErr = err
			continue
		}
		if err := payloadError(payload); err != nil {
			lastErr = err
			continue
		}
		cookies := cookiesFromLoginResponse(payload, header)
		if cookies != "" {
			return cookies, nil
		}
		lastErr = errors.New("115 未返回 Cookies，请确认已在手机端完成扫码")
	}
	return "", lastErr
}

func (c *Client) OpenDeviceCodeToToken(ctx context.Context, uid, codeVerifier string) (openTokenPair, error) {
	form := url.Values{}
	form.Set("uid", uid)
	form.Set("code_verifier", codeVerifier)
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://qrcodeapi.115.com/open/deviceCodeToToken", form, false, "")
	if err != nil {
		return openTokenPair{}, err
	}
	if err := payloadError(payload); err != nil {
		return openTokenPair{}, err
	}
	return tokenPairFromPayload(payload)
}

func (c *Client) OpenOAuthAuthorizeURL(redirectURI, state string) models.P115OAuthStart {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(c.settings.AppID))
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	values.Set("state", state)
	return models.P115OAuthStart{
		AuthorizeURL: "https://qrcodeapi.115.com/open/authorize?" + values.Encode(),
		RedirectURI:  redirectURI,
		State:        state,
	}
}

func (c *Client) OpenAuthCodeToToken(ctx context.Context, code, redirectURI string) (openTokenPair, error) {
	if strings.TrimSpace(c.settings.AppSecret) == "" {
		return openTokenPair{}, errors.New("请先填写 115 App Secret")
	}
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(c.settings.AppID))
	form.Set("client_secret", strings.TrimSpace(c.settings.AppSecret))
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://qrcodeapi.115.com/open/authCodeToToken", form, false, "")
	if err != nil {
		return openTokenPair{}, err
	}
	if err := payloadError(payload); err != nil {
		return openTokenPair{}, err
	}
	return tokenPairFromPayload(payload)
}

func (c *Client) RefreshOpenToken(ctx context.Context) (openTokenPair, error) {
	form := url.Values{}
	form.Set("refresh_token", strings.TrimSpace(c.settings.RefreshToken))
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://qrcodeapi.115.com/open/refreshToken", form, false, "")
	if err != nil {
		return openTokenPair{}, err
	}
	if err := payloadError(payload); err != nil {
		return openTokenPair{}, err
	}
	return tokenPairFromPayload(payload)
}

func (c *Client) hasCookies() bool {
	return strings.TrimSpace(c.settings.Cookies) != ""
}

func (c *Client) hasOpenToken() bool {
	return strings.TrimSpace(c.settings.AccessToken) != ""
}

func (c *Client) preferOpen() bool {
	if !c.hasOpenToken() {
		return false
	}
	return !c.hasCookies()
}

func (c *Client) ScanTree(ctx context.Context, lib LibraryConfig) ([]TreeItem, error) {
	if c.hasCookies() {
		return c.exportTreeByCookieList(ctx, lib)
	}
	if c.hasOpenToken() {
		return c.exportTreeByListWith(ctx, lib, c.listOpen)
	}
	return nil, errors.New("115 未配置可用授权")
}

func (c *Client) ExportTree(ctx context.Context, lib LibraryConfig) ([]TreeItem, error) {
	if c.hasCookies() {
		items, err := c.exportTreeByWeb(ctx, lib)
		if err == nil {
			return items, nil
		}
		if isRateLimitError(err) {
			return nil, err
		}
		scanItems, scanErr := c.ScanTree(ctx, lib)
		if scanErr == nil {
			return scanItems, nil
		}
		if !c.hasOpenToken() {
			return nil, fmt.Errorf("115 目录树导出失败：%v；慢速分页兜底也失败：%w", err, scanErr)
		}
		items, openErr := c.exportTreeByListWith(ctx, lib, c.listOpen)
		if openErr == nil {
			return items, nil
		}
		return nil, fmt.Errorf("115 目录树导出失败：%v；Cookies 分页兜底失败：%v；Open 分页兜底也失败：%w", err, scanErr, openErr)
	}
	items, openErr := c.exportTreeByListWith(ctx, lib, c.listOpen)
	if openErr == nil {
		return items, nil
	}
	return nil, fmt.Errorf("Open 分页失败：%w", openErr)
}

func (c *Client) ResolvePath(ctx context.Context, cid, relativePath string) (FileInfo, error) {
	segments := splitRelativePath(relativePath)
	if len(segments) == 0 {
		return FileInfo{}, errors.New("115 相对路径为空")
	}
	current := strings.TrimSpace(cid)
	for index, name := range segments {
		children, err := c.List(ctx, current)
		if err != nil {
			return FileInfo{}, err
		}
		var match *FileInfo
		for i := range children {
			if children[i].Name == name {
				match = &children[i]
				break
			}
		}
		if match == nil {
			return FileInfo{}, fmt.Errorf("115 路径不存在：%s", relativePath)
		}
		if index == len(segments)-1 {
			if match.IsDirectory {
				return FileInfo{}, fmt.Errorf("115 路径不是文件：%s", relativePath)
			}
			return *match, nil
		}
		if !match.IsDirectory {
			return FileInfo{}, fmt.Errorf("115 路径中间节点不是目录：%s", name)
		}
		current = match.ID
	}
	return FileInfo{}, fmt.Errorf("115 路径不存在：%s", relativePath)
}

func (c *Client) List(ctx context.Context, cid string) ([]FileInfo, error) {
	if c.hasCookies() {
		items, err := c.listCookie(ctx, cid)
		if err == nil {
			return items, nil
		}
		if !c.hasOpenToken() {
			return nil, err
		}
	}
	if c.hasOpenToken() {
		return c.listOpen(ctx, cid)
	}
	return nil, errors.New("115 未配置可用授权")
}

func (c *Client) DirectURL(ctx context.Context, pickcode, userAgentValue string) (string, error) {
	pickcode = strings.TrimSpace(pickcode)
	if pickcode == "" {
		return "", errors.New("115 pickcode 为空")
	}
	if c.hasCookies() {
		directURL, err := c.directURLAppChrome(ctx, pickcode, userAgentValue)
		if err == nil {
			return directURL, nil
		}
		webURL, webErr := c.directURLWeb(ctx, pickcode, userAgentValue)
		if webErr == nil {
			return webURL, nil
		}
		if c.hasOpenToken() {
			if fallbackURL, fallbackErr := c.directURLOpen(ctx, pickcode, userAgentValue); fallbackErr == nil {
				return fallbackURL, nil
			} else {
				return "", fmt.Errorf("Cookies App 直链失败：%v；Cookies Web 直链失败：%v；Open 直链也失败：%v", err, webErr, fallbackErr)
			}
		}
		return "", fmt.Errorf("Cookies App 直链失败：%v；Cookies Web 直链失败：%w", err, webErr)
	}
	if c.hasOpenToken() {
		directURL, err := c.directURLOpen(ctx, pickcode, userAgentValue)
		if err == nil {
			return directURL, nil
		}
		return "", err
	}
	return "", errors.New("115 未配置可用授权")
}

func (c *Client) LifeEventsOnce(ctx context.Context, fromID, fromTime int64, maxPages int) ([]LifeEvent, error) {
	batch, err := c.LifeEventsBatch(ctx, fromID, fromTime, maxPages)
	return batch.Events, err
}

func (c *Client) LifeEventsBatch(ctx context.Context, fromID, fromTime int64, maxPages int) (LifeEventBatch, error) {
	if !c.hasCookies() {
		return LifeEventBatch{}, errors.New("115 操作事件需要 Cookies 授权")
	}
	_ = c.ensureLifeEvents(ctx)
	batch, err := c.lifeBehaviorEventsOnce(ctx, fromID, lifeEventStartTime(fromTime), maxPages)
	if err == nil {
		return batch, nil
	}
	recentBatch, recentErr := c.lifeRecentEventsOnce(ctx, fromID, lifeEventStartTime(fromTime), maxPages)
	if recentErr == nil {
		return recentBatch, nil
	}
	return LifeEventBatch{}, fmt.Errorf("115 Android 操作事件失败：%v；Recent 操作事件也失败：%w", err, recentErr)
}

func lifeEventStartTime(fromTime int64) int64 {
	if fromTime <= lifeEventLookbackSeconds {
		return fromTime
	}
	return fromTime - lifeEventLookbackSeconds
}

func advanceLifeEventBatchCursor(batch LifeEventBatch, event LifeEvent) LifeEventBatch {
	if event.ID > batch.LastEventID {
		batch.LastEventID = event.ID
	}
	if event.UpdateTime > batch.LastEventTime {
		batch.LastEventTime = event.UpdateTime
	}
	return batch
}

func (c *Client) lifeBehaviorEventsOnce(ctx context.Context, fromID, fromTime int64, maxPages int) (LifeEventBatch, error) {
	if maxPages <= 0 {
		maxPages = 20
	}
	batch := LifeEventBatch{Events: make([]LifeEvent, 0)}
	seenEvents := map[int64]struct{}{}
	limit := 64
	offset := 0
	for page := 0; page < maxPages; page++ {
		payload, err := c.lifeBehaviorDetailApp(ctx, "", "", limit, offset)
		if err != nil {
			return LifeEventBatch{}, err
		}
		rows := extractArray(payload)
		if len(rows) == 0 {
			break
		}
		stop := false
		for _, row := range rows {
			event := lifeEventFromBehaviorMap(row)
			if event.ID == 0 && event.UpdateTime == 0 {
				continue
			}
			if lifeEventBeforeCursor(event, fromID, fromTime) {
				stop = true
				break
			}
			batch.RawCount++
			batch = advanceLifeEventBatchCursor(batch, event)
			if event.ID == 0 || event.FileID == "" {
				continue
			}
			if _, ignored := ignoredBehaviorTypes[event.Type]; ignored {
				continue
			}
			if _, seen := seenEvents[event.ID]; seen {
				continue
			}
			seenEvents[event.ID] = struct{}{}
			batch.Events = append(batch.Events, event)
		}
		if stop || len(rows) < limit {
			break
		}
		offset += len(rows)
		limit = 1000
	}
	return batch, nil
}

func (c *Client) lifeRecentEventsOnce(ctx context.Context, fromID, fromTime int64, maxPages int) (LifeEventBatch, error) {
	if maxPages <= 0 {
		maxPages = 20
	}
	batch := LifeEventBatch{Events: make([]LifeEvent, 0)}
	seenEvents := map[int64]struct{}{}
	detailCache := map[string][]LifeEvent{}
	limit := 1000
	offset := 0
	lastData := ""
	for page := 0; page < maxPages; page++ {
		payload, err := c.lifeRecentOperations(ctx, limit, offset, fromTime, lastData)
		if err != nil {
			return LifeEventBatch{}, err
		}
		lastData = recentLastData(payload)
		rows := extractArray(payload)
		if len(rows) == 0 {
			break
		}
		stop := false
		for _, row := range rows {
			rowEvents, err := c.lifeEventsFromRecentOperation(ctx, row, detailCache)
			if err != nil {
				return LifeEventBatch{}, err
			}
			if len(rowEvents) == 0 {
				rowEvents = []LifeEvent{lifeEventFromRecentMap(row, LifeEvent{})}
			}
			for _, event := range rowEvents {
				if event.ID == 0 && event.UpdateTime == 0 {
					continue
				}
				if lifeEventBeforeCursor(event, fromID, fromTime) {
					stop = true
					break
				}
				batch.RawCount++
				batch = advanceLifeEventBatchCursor(batch, event)
				if event.ID == 0 || event.FileID == "" {
					continue
				}
				if _, ignored := ignoredBehaviorTypes[event.Type]; ignored {
					continue
				}
				if _, seen := seenEvents[event.ID]; seen {
					continue
				}
				seenEvents[event.ID] = struct{}{}
				batch.Events = append(batch.Events, event)
			}
			if stop {
				break
			}
		}
		if stop || len(rows) < limit {
			break
		}
		offset += len(rows)
	}
	return batch, nil
}

func (c *Client) LatestLifeEventCursor(ctx context.Context) (int64, int64, error) {
	payload, err := c.lifeBehaviorDetailApp(ctx, "", "", 1, 0)
	if err == nil {
		for _, row := range extractArray(payload) {
			event := lifeEventFromBehaviorMap(row)
			if event.ID > 0 {
				return event.ID, event.UpdateTime, nil
			}
		}
	}
	recentPayload, recentErr := c.lifeRecentOperations(ctx, 1, 0, 0, "")
	if recentErr != nil {
		if err != nil {
			return 0, 0, fmt.Errorf("115 Android 最新事件失败：%v；Recent 最新事件也失败：%w", err, recentErr)
		}
		return 0, 0, recentErr
	}
	for _, row := range extractArray(recentPayload) {
		event := lifeEventFromRecentMap(row, LifeEvent{})
		if event.ID > 0 {
			return event.ID, event.UpdateTime, nil
		}
	}
	return 0, 0, nil
}

func (c *Client) lifeRecentOperations(ctx context.Context, limit, offset int, startTime int64, lastData string) (map[string]any, error) {
	if limit <= 0 {
		limit = 1000
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	values.Set("start", strconv.Itoa(offset))
	if startTime > 0 {
		values.Set("start_time", strconv.FormatInt(startTime, 10))
	}
	if strings.TrimSpace(lastData) != "" {
		values.Set("last_data", strings.TrimSpace(lastData))
	}
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://life.115.com/api/1.0/web/1.0/life/recent_operations", values, false, "")
	return payload, err
}

func (c *Client) lifeRecentOperationItems(ctx context.Context, behaviorType, date string, limit, offset int) (map[string]any, error) {
	if limit <= 0 {
		limit = 1000
	}
	values := url.Values{}
	values.Set("behavior_type", strings.TrimSpace(behaviorType))
	values.Set("date", strings.TrimSpace(date))
	values.Set("limit", strconv.Itoa(limit))
	values.Set("start", strconv.Itoa(offset))
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://life.115.com/api/1.0/web/1.0/life/recent_operation_items", values, false, "")
	return payload, err
}

func (c *Client) lifeBehaviorDetailApp(ctx context.Context, behaviorType, date string, limit, offset int) (map[string]any, error) {
	if limit <= 0 {
		limit = 1000
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	values.Set("offset", strconv.Itoa(offset))
	values.Set("type", strings.TrimSpace(behaviorType))
	values.Set("date", strings.TrimSpace(date))
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://proapi.115.com/android/behavior/detail", values, false, "")
	return payload, err
}

func (c *Client) ensureLifeEvents(ctx context.Context) error {
	form := url.Values{}
	form.Set("locus", "1")
	form.Set("open_life", "1")
	_, _, err := c.requestJSON(ctx, http.MethodPost, "https://life.115.com/api/1.0/web/1.0/calendar/setoption", form, false, "")
	return err
}

func (c *Client) lifeEventsFromRecentOperation(ctx context.Context, row map[string]any, cache map[string][]LifeEvent) ([]LifeEvent, error) {
	behaviorName := recentBehaviorName(row)
	if _, ok := recentOperationDetailTypes[behaviorName]; !ok {
		return nil, nil
	}
	date := recentOperationDate(row)
	if date == "" {
		return nil, nil
	}
	cacheKey := behaviorName + "|" + date
	if events, ok := cache[cacheKey]; ok {
		return events, nil
	}
	base := lifeEventFromRecentMap(row, LifeEvent{})
	events := make([]LifeEvent, 0)
	for offset := 0; ; {
		payload, err := c.lifeRecentOperationItems(ctx, behaviorName, date, 1000, offset)
		if err != nil {
			return nil, err
		}
		rows := extractArray(payload)
		if len(rows) == 0 {
			break
		}
		for _, item := range rows {
			event := lifeEventFromRecentMap(item, base)
			if event.ID == 0 {
				event.ID = stableRecentEventID(event, item)
			}
			events = append(events, event)
		}
		if len(rows) < 1000 {
			break
		}
		offset += len(rows)
	}
	cache[cacheKey] = events
	return events, nil
}

func (c *Client) exportTreeByWeb(ctx context.Context, lib LibraryConfig) ([]TreeItem, error) {
	form := url.Values{}
	form.Set("file_ids", lib.CID)
	form.Set("target", "U_0_0")
	if lib.LayerLimit > 0 {
		form.Set("layer_limit", strconv.Itoa(lib.LayerLimit))
	}
	resp, _, err := c.requestJSONWithRateLimitRetry(ctx, http.MethodPost, "https://webapi.115.com/files/export_dir", form, false, "", "创建目录树导出任务")
	if err != nil {
		return nil, err
	}
	data := asMap(resp["data"])
	exportID := firstString(data, "export_id", "id")
	if exportID == "" {
		return nil, errors.New("115 未返回目录树导出任务 ID")
	}
	var result map[string]any
	deadline := time.Now().Add(10 * time.Minute)
	for {
		query := url.Values{}
		query.Set("export_id", exportID)
		payload, _, err := c.requestJSONWithRateLimitRetry(ctx, http.MethodGet, "https://webapi.115.com/files/export_dir", query, false, "", "读取目录树导出结果")
		if err != nil {
			return nil, err
		}
		if data := asMap(payload["data"]); len(data) > 0 {
			result = data
			break
		}
		if time.Now().After(deadline) {
			return nil, errors.New("115 目录树导出超时")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	pickcode := firstString(result, "pick_code", "pickcode", "pc")
	if pickcode == "" {
		return nil, errors.New("115 目录树导出结果缺少 pickcode")
	}
	downloadURL, err := c.directURLWebWithRateLimitRetry(ctx, pickcode, defaultUserAgent)
	if err != nil {
		return nil, err
	}
	body, err := c.downloadWithRateLimitRetry(ctx, downloadURL, defaultUserAgent)
	if err != nil {
		return nil, err
	}
	if fileID := firstString(result, "file_id", "fid", "id"); fileID != "" {
		_ = c.deleteWeb(ctx, fileID)
	}
	return parseExportTree(body)
}

func (c *Client) exportTreeByCookieList(ctx context.Context, lib LibraryConfig) ([]TreeItem, error) {
	return c.exportTreeByListWith(ctx, lib, c.listCookie)
}

func (c *Client) exportTreeByListWith(ctx context.Context, lib LibraryConfig, list func(context.Context, string) ([]FileInfo, error)) ([]TreeItem, error) {
	items := make([]TreeItem, 0)
	err := c.walk(ctx, lib.CID, "", 0, lib.LayerLimit, list, &items)
	return items, err
}

func (c *Client) walk(ctx context.Context, cid, prefix string, depth, limit int, list func(context.Context, string) ([]FileInfo, error), items *[]TreeItem) error {
	if limit > 0 && depth >= limit {
		return nil
	}
	children, err := list(ctx, cid)
	if err != nil {
		return err
	}
	for _, child := range children {
		rel := child.Name
		if prefix != "" {
			rel = prefix + "/" + child.Name
		}
		item := TreeItem{
			RelativePath:   rel,
			Name:           child.Name,
			RemoteFileID:   child.ID,
			ParentFileID:   cid,
			PickCode:       child.PickCode,
			SHA1:           child.SHA1,
			Size:           child.Size,
			Depth:          depth + 1,
			IsDirectory:    child.IsDirectory,
			SourceTreeHash: sourceHash(cid, rel, child.ID, child.PickCode, child.SHA1, child.Size),
		}
		*items = append(*items, item)
		if child.IsDirectory {
			if err := c.walk(ctx, child.ID, rel, depth+1, limit, list, items); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) listOpen(ctx context.Context, cid string) ([]FileInfo, error) {
	return c.listPaged(ctx, "https://proapi.115.com/open/ufile/files", cid, true, 1150)
}

func (c *Client) listCookie(ctx context.Context, cid string) ([]FileInfo, error) {
	items, err := c.listApp(ctx, cid)
	if err == nil {
		return items, nil
	}
	webItems, webErr := c.listWeb(ctx, cid)
	if webErr == nil {
		return webItems, nil
	}
	apsItems, apsErr := c.listAPS(ctx, cid)
	if apsErr == nil {
		return apsItems, nil
	}
	return nil, fmt.Errorf("App 分页失败：%v；Web 分页失败：%v；APS 分页失败：%w", err, webErr, apsErr)
}

func (c *Client) listApp(ctx context.Context, cid string) ([]FileInfo, error) {
	return c.listPaged(ctx, "https://proapi.115.com/android/2.0/ufile/files", cid, false, 7000)
}

func (c *Client) listWeb(ctx context.Context, cid string) ([]FileInfo, error) {
	return c.listPaged(ctx, "https://webapi.115.com/files", cid, false, 1150)
}

func (c *Client) listAPS(ctx context.Context, cid string) ([]FileInfo, error) {
	return c.listPaged(ctx, "https://aps.115.com/natsort/files.php", cid, false, 1200)
}

func (c *Client) listPaged(ctx context.Context, endpoint, cid string, open bool, pageSize int) ([]FileInfo, error) {
	if pageSize <= 0 {
		pageSize = 1150
	}
	out := make([]FileInfo, 0)
	for offset := 0; ; offset += pageSize {
		query := url.Values{}
		query.Set("cid", strings.TrimSpace(cid))
		query.Set("limit", strconv.Itoa(pageSize))
		query.Set("offset", strconv.Itoa(offset))
		query.Set("show_dir", "1")
		query.Set("count_folders", "1")
		query.Set("record_open_time", "0")
		payload, _, err := c.requestJSON(ctx, http.MethodGet, endpoint, query, open, "")
		if err != nil {
			return nil, err
		}
		rows := extractArray(payload)
		for _, row := range rows {
			info := fileInfoFromMap(row)
			if info.Name != "" {
				out = append(out, info)
			}
		}
		if len(rows) < pageSize {
			break
		}
	}
	return out, nil
}

func (c *Client) directURLOpen(ctx context.Context, pickcode, userAgentValue string) (string, error) {
	form := url.Values{}
	form.Set("pick_code", pickcode)
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://proapi.115.com/open/ufile/downurl", form, true, userAgentValue)
	if err != nil {
		return "", err
	}
	if value := extractDownloadURL(payload); value != "" {
		return value, nil
	}
	return "", errors.New("115 Open 未返回下载直链")
}

func (c *Client) directURLAppChrome(ctx context.Context, pickcode, userAgentValue string) (string, error) {
	payload, err := json.Marshal(map[string]string{"pickcode": pickcode})
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("data", p115RSAEncrypt(payload))
	resp, _, err := c.requestJSON(ctx, http.MethodPost, "https://proapi.115.com/app/chrome/downurl", form, false, userAgentValue)
	if err != nil {
		return "", err
	}
	rawData, ok := resp["data"].(string)
	if !ok || strings.TrimSpace(rawData) == "" {
		if value := extractDownloadURL(resp); value != "" {
			return value, nil
		}
		return "", errors.New("115 App 未返回加密直链")
	}
	plain, err := p115RSADecrypt(rawData)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.UseNumber()
	var data any
	if err := decoder.Decode(&data); err != nil {
		return "", err
	}
	if value := extractDownloadURL(data); value != "" {
		return value, nil
	}
	return "", errors.New("115 App 未返回下载直链")
}

func (c *Client) directURLWeb(ctx context.Context, pickcode, userAgentValue string) (string, error) {
	query := url.Values{}
	query.Set("pickcode", pickcode)
	query.Set("dl", "1")
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://webapi.115.com/files/download", query, false, userAgentValue)
	if err == nil {
		if value := extractDownloadURL(payload); value != "" {
			return value, nil
		}
	} else if isRateLimitError(err) {
		return "", err
	}
	raw := "https://115.com/?" + url.Values{"ct": []string{"download"}, "ac": []string{"video"}, "pickcode": []string{pickcode}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	c.decorate(req, false, userAgentValue)
	client := *c.http
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if err := p115RequestThrottle.Wait(ctx); err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", rateLimitError{message: "115 请求失败：已达到当前访问上限，请稍后再试"}
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if location := strings.TrimSpace(resp.Header.Get("Location")); location != "" {
			return location, nil
		}
	}
	if err != nil {
		return "", err
	}
	return "", errors.New("Cookies 模式未能解析 115 直链；大文件播放建议配置 Open Access Token")
}

func (c *Client) requestJSON(ctx context.Context, method, rawURL string, values url.Values, open bool, userAgentValue string) (map[string]any, http.Header, error) {
	reqURL := rawURL
	var body io.Reader
	if method == http.MethodGet {
		if len(values) > 0 {
			sep := "?"
			if strings.Contains(reqURL, "?") {
				sep = "&"
			}
			reqURL += sep + values.Encode()
		}
	} else if values != nil {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, nil, err
	}
	c.decorate(req, open, userAgentValue)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err := p115RequestThrottle.Wait(ctx); err != nil {
		return nil, nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, resp.Header, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, resp.Header, rateLimitError{message: "115 请求失败：已达到当前访问上限，请稍后再试"}
		}
		return nil, resp.Header, fmt.Errorf("115 请求失败：HTTP %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, resp.Header, err
	}
	if !responseOK(payload) {
		message := responseMessage(payload)
		if isRateLimitMessage(message) {
			return nil, resp.Header, rateLimitError{message: "115 请求失败：" + message}
		}
		return nil, resp.Header, fmt.Errorf("115 请求失败：%s", message)
	}
	return payload, resp.Header, nil
}

func (c *Client) requestJSONWithRateLimitRetry(ctx context.Context, method, rawURL string, values url.Values, open bool, userAgentValue, action string) (map[string]any, http.Header, error) {
	var lastErr error
	var header http.Header
	for attempt := 0; attempt < 2; attempt++ {
		payload, header, err := c.requestJSON(ctx, method, rawURL, values, open, userAgentValue)
		if !isRateLimitError(err) {
			return payload, header, err
		}
		lastErr = err
		if waitErr := sleepAfterRateLimit(ctx, attempt); waitErr != nil {
			return nil, header, fmt.Errorf("%s被 115 限流：%w", action, lastErr)
		}
	}
	return nil, header, fmt.Errorf("%s被 115 限流，请暂停一段时间后再试：%w", action, lastErr)
}

func (c *Client) decorate(req *http.Request, open bool, userAgentValue string) {
	req.Header.Set("User-Agent", userAgent(c.settings, userAgentValue))
	if open {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.settings.AccessToken))
		return
	}
	if cookies := strings.TrimSpace(c.settings.Cookies); cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
}

func (c *Client) download(ctx context.Context, rawURL, userAgentValue string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	c.decorate(req, false, userAgentValue)
	if err := p115RequestThrottle.Wait(ctx); err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, rateLimitError{message: "115 下载目录树失败：已达到当前访问上限，请稍后再试"}
		}
		return nil, fmt.Errorf("115 下载目录树失败：HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

func (c *Client) directURLWebWithRateLimitRetry(ctx context.Context, pickcode, userAgentValue string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		value, err := c.directURLWeb(ctx, pickcode, userAgentValue)
		if !isRateLimitError(err) {
			return value, err
		}
		lastErr = err
		if waitErr := sleepAfterRateLimit(ctx, attempt); waitErr != nil {
			return "", fmt.Errorf("获取目录树下载直链被 115 限流：%w", lastErr)
		}
	}
	return "", fmt.Errorf("获取目录树下载直链被 115 限流，请暂停一段时间后再试：%w", lastErr)
}

func (c *Client) downloadWithRateLimitRetry(ctx context.Context, rawURL, userAgentValue string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		value, err := c.download(ctx, rawURL, userAgentValue)
		if !isRateLimitError(err) {
			return value, err
		}
		lastErr = err
		if waitErr := sleepAfterRateLimit(ctx, attempt); waitErr != nil {
			return nil, fmt.Errorf("下载目录树被 115 限流：%w", lastErr)
		}
	}
	return nil, fmt.Errorf("下载目录树被 115 限流，请暂停一段时间后再试：%w", lastErr)
}

func (c *Client) deleteWeb(ctx context.Context, fileID string) error {
	form := url.Values{}
	form.Set("fid", fileID)
	_, _, err := c.requestJSON(ctx, http.MethodPost, "https://webapi.115.com/rb/delete", form, false, "")
	return err
}

func parseExportTree(data []byte) ([]TreeItem, error) {
	decoder := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
	decoded, _, err := transform.Bytes(decoder, data)
	if err != nil {
		decoded = data
	}
	text := strings.ReplaceAll(string(decoded), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	stack := map[int]string{}
	items := make([]TreeItem, 0)
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		idx := strings.LastIndex(line, "|-")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line[idx+2:], "-"))
		if name == "" {
			continue
		}
		depth := strings.Count(line[:idx], "|") + 1
		stack[depth] = name
		for key := range stack {
			if key > depth {
				delete(stack, key)
			}
		}
		parts := make([]string, 0, depth)
		for i := 1; i <= depth; i++ {
			if stack[i] != "" {
				parts = append(parts, stack[i])
			}
		}
		rel := path.Join(parts...)
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
		items = append(items, TreeItem{
			RelativePath:   rel,
			Name:           name,
			Depth:          depth,
			IsDirectory:    ext == "",
			SourceTreeHash: sourceHash("", rel, "", "", "", 0),
		})
	}
	return items, nil
}

func splitRelativePath(value string) []string {
	value = strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

func fileInfoFromMap(row map[string]any) FileInfo {
	info := FileInfo{
		ID:       firstString(row, "file_id", "fid", "id", "cid"),
		ParentID: firstString(row, "parent_id", "pid", "cid"),
		Name:     firstString(row, "file_name", "name", "n", "fn"),
		PickCode: firstString(row, "pick_code", "pickcode", "pc"),
		SHA1:     firstString(row, "sha1", "sha", "file_sha1"),
		Size:     firstInt64(row, "size", "s", "file_size", "fs"),
	}
	info.IsDirectory = firstBool(row, "is_dir", "is_directory", "is_folder")
	if !hasAny(row, "is_dir", "is_directory", "is_folder") {
		if fc := firstString(row, "fc"); fc != "" {
			info.IsDirectory = fc == "0"
		}
	}
	if !info.IsDirectory && info.PickCode == "" && firstString(row, "cid") != "" && firstString(row, "fid", "file_id") == "" {
		info.IsDirectory = true
	}
	return info
}

func lifeEventFromBehaviorMap(row map[string]any) LifeEvent {
	event := lifeEventFromRecentMap(row, LifeEvent{})
	if event.Type == 0 {
		event.Type = int(firstInt64(row, "type"))
	}
	if event.EventName == "" && event.Type != 0 {
		event.EventName = behaviorTypeNames[event.Type]
	}
	if event.FileID == "" {
		event.FileID = firstString(row, "file_id")
	}
	if event.ParentID == "" {
		event.ParentID = firstString(row, "parent_id")
	}
	if event.Name == "" {
		event.Name = firstString(row, "file_name")
	}
	if event.PickCode == "" {
		event.PickCode = firstString(row, "pick_code")
	}
	if event.SHA1 == "" {
		event.SHA1 = firstString(row, "sha1")
	}
	if event.Size == 0 {
		event.Size = firstInt64(row, "file_size")
	}
	if event.UpdateTime == 0 {
		event.UpdateTime = firstInt64(row, "update_time", "create_time")
	}
	if event.CreateTime == 0 {
		event.CreateTime = firstInt64(row, "create_time", "update_time")
	}
	if event.ID == 0 {
		event.ID = stableRecentEventID(event, row)
	}
	return event
}

func lifeEventFromRecentMap(row map[string]any, base LifeEvent) LifeEvent {
	behaviorName := recentBehaviorName(row)
	eventType := behaviorNameTypes[behaviorName]
	if eventType == 0 {
		eventType = int(firstInt64(row, "type", "event_type", "behavior_type_code"))
	}
	if eventType == 0 {
		eventType = base.Type
	}
	updateTime := recentEventUpdateTime(row, base)
	createTime := firstInt64(row, "create_time", "created_at", "time", "timestamp")
	if createTime == 0 {
		createTime = updateTime
	}
	event := LifeEvent{
		ID:         firstInt64(row, "id", "event_id", "relation_id", "rid"),
		Type:       eventType,
		EventName:  behaviorName,
		FileID:     firstString(row, "file_id", "fid", "file_id_str", "target_id", "target_file_id"),
		ParentID:   firstString(row, "parent_id", "pid", "cid", "to_cid", "target_cid", "target_parent_id"),
		Name:       firstString(row, "file_name", "name", "n", "fn", "target_name"),
		PickCode:   firstString(row, "pick_code", "pickcode", "pc"),
		SHA1:       firstString(row, "sha1", "sha", "file_sha1"),
		Size:       firstInt64(row, "file_size", "size", "fs"),
		CreateTime: createTime,
		UpdateTime: updateTime,
	}
	if event.EventName == "" && event.Type != 0 {
		event.EventName = behaviorTypeNames[event.Type]
	}
	if event.FileID == "" {
		event.FileID = base.FileID
	}
	if event.ParentID == "" {
		event.ParentID = base.ParentID
	}
	if event.Name == "" {
		event.Name = base.Name
	}
	if event.PickCode == "" {
		event.PickCode = base.PickCode
	}
	if event.SHA1 == "" {
		event.SHA1 = base.SHA1
	}
	if event.Size == 0 {
		event.Size = base.Size
	}
	if event.ID == 0 {
		event.ID = stableRecentEventID(event, row)
	}
	return event
}

func recentBehaviorName(row map[string]any) string {
	value := strings.ToLower(strings.TrimSpace(firstString(row, "behavior_type", "behavior", "event_name", "type_name", "operation_type")))
	if _, ok := behaviorNameTypes[value]; ok {
		return value
	}
	if code := int(firstInt64(row, "type", "event_type", "behavior_type_code")); code > 0 {
		return behaviorTypeNames[code]
	}
	return value
}

func recentOperationDate(row map[string]any) string {
	for _, key := range []string{"date", "day", "create_date", "update_date"} {
		value := strings.TrimSpace(firstString(row, key))
		if len(value) >= 10 {
			return value[:10]
		}
	}
	if ts := firstInt64(row, "update_time", "create_time", "time", "timestamp"); ts > 0 {
		return time.Unix(ts, 0).Format("2006-01-02")
	}
	return ""
}

func lifeEventBeforeCursor(event LifeEvent, fromID, fromTime int64) bool {
	if fromTime > 0 && event.UpdateTime > 0 {
		if event.UpdateTime < fromTime {
			return true
		}
		if event.UpdateTime > fromTime {
			return false
		}
		return fromID > 0 && event.ID > 0 && event.ID <= fromID
	}
	return fromTime == 0 && fromID > 0 && event.ID > 0 && event.ID <= fromID
}

func recentEventUpdateTime(row map[string]any, base LifeEvent) int64 {
	updateTime := firstInt64(row, "update_time", "updated_at", "create_time", "created_at", "time", "timestamp")
	if updateTime > 0 {
		return updateTime
	}
	if updateTime = parseRecentTime(firstString(row, "datetime", "create_time_str", "update_time_str", "created_at_str", "time_str")); updateTime > 0 {
		return updateTime
	}
	dateValue := firstString(row, "date", "day", "create_date", "update_date")
	if isRecentDateOnly(dateValue) {
		if base.UpdateTime > 0 {
			return base.UpdateTime
		}
		return parseRecentDateEnd(dateValue)
	}
	if updateTime = parseRecentTime(dateValue); updateTime > 0 {
		return updateTime
	}
	return base.UpdateTime
}

func parseRecentTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t.Unix()
		}
	}
	return 0
}

func isRecentDateOnly(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != len("2006-01-02") {
		return false
	}
	_, err := time.ParseInLocation("2006-01-02", value, time.Local)
	return err == nil
}

func parseRecentDateEnd(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	t, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return 0
	}
	return t.Add(24*time.Hour - time.Second).Unix()
}

func stableRecentEventID(event LifeEvent, row map[string]any) int64 {
	base := event.UpdateTime
	if base == 0 {
		base = event.CreateTime
	}
	if base == 0 {
		base = 1
	}
	key := strings.Join([]string{
		event.EventName,
		event.FileID,
		event.ParentID,
		event.Name,
		firstString(row, "relation_id", "id", "event_id", "rid"),
	}, "|")
	return base*1000000 + int64(crc32.ChecksumIEEE([]byte(key))%1000000)
}

func responseOK(payload map[string]any) bool {
	value, ok := payload["state"]
	if !ok {
		return true
	}
	switch v := value.(type) {
	case bool:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n != 0
	case float64:
		return v != 0
	case string:
		return v != "0" && !strings.EqualFold(v, "false")
	default:
		return true
	}
}

func responseMessage(payload map[string]any) string {
	if msg := firstString(payload, "error", "error_msg", "message", "msg"); msg != "" {
		return msg
	}
	if code := firstString(payload, "errno", "errNo", "errcode", "code"); code != "" {
		return code
	}
	return "未知错误"
}

func payloadError(payload map[string]any) error {
	if !responseOK(payload) {
		message := responseMessage(payload)
		if isRateLimitMessage(message) {
			return rateLimitError{message: "115 请求失败：" + message}
		}
		return fmt.Errorf("115 请求失败：%s", message)
	}
	for _, key := range []string{"code", "errno", "errNo", "errcode"} {
		code := firstString(payload, key)
		if code == "" || code == "0" || strings.EqualFold(code, "success") {
			continue
		}
		message := responseMessage(payload)
		if isRateLimitMessage(message) {
			return rateLimitError{message: "115 请求失败：" + message}
		}
		return fmt.Errorf("115 请求失败：%s", message)
	}
	return nil
}

func isRateLimitError(err error) bool {
	var target rateLimitError
	if errors.As(err, &target) {
		return true
	}
	return err != nil && isRateLimitMessage(err.Error())
}

func isRateLimitMessage(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	for _, token := range []string{
		"访问上限", "访问频繁", "请求频繁", "操作频繁", "频繁操作", "请求过快",
		"风控", "频率", "稍后再试", "稍候再试", "请稍后", "too many requests",
		"too frequent", "rate limit", "limited",
	} {
		if strings.Contains(message, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

func sleepAfterRateLimit(ctx context.Context, attempt int) error {
	wait := time.Duration(30+attempt*30) * time.Second
	if wait > 90*time.Second {
		wait = 90 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type p115Throttle struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func newP115Throttle(interval time.Duration) *p115Throttle {
	return &p115Throttle{interval: interval}
}

func (t *p115Throttle) Wait(ctx context.Context) error {
	t.mu.Lock()
	now := time.Now()
	wait := time.Duration(0)
	if now.Before(t.next) {
		wait = t.next.Sub(now)
		t.next = t.next.Add(t.interval)
	} else {
		t.next = now.Add(t.interval)
	}
	t.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func tokenPairFromPayload(payload map[string]any) (openTokenPair, error) {
	data := asMap(payload["data"])
	if len(data) == 0 {
		data = payload
	}
	token := openTokenPair{
		AccessToken:  firstString(data, "access_token", "accessToken"),
		RefreshToken: firstString(data, "refresh_token", "refreshToken"),
	}
	if token.AccessToken == "" {
		return openTokenPair{}, errors.New("115 未返回 Access Token")
	}
	return token, nil
}

func cookiesFromLoginResponse(payload map[string]any, header http.Header) string {
	for _, row := range []map[string]any{asMap(payload["data"]), payload} {
		if len(row) == 0 {
			continue
		}
		for _, key := range []string{"cookie", "cookies"} {
			if cookies := cookiesFromValue(row[key]); cookies != "" {
				return cookies
			}
		}
	}
	response := http.Response{Header: header}
	return cookiesFromHTTP(response.Cookies())
}

func cookiesFromValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, key := range []string{"UID", "CID", "SEID", "KID"} {
			if value := firstString(v, key, strings.ToLower(key)); value != "" {
				parts = append(parts, key+"="+value)
			}
		}
		for key, raw := range v {
			if hasCookieKey(parts, key) {
				continue
			}
			if value := cookieValue(raw); value != "" {
				parts = append(parts, key+"="+value)
			}
		}
		return strings.Join(parts, "; ")
	case []any:
		cookies := make([]*http.Cookie, 0, len(v))
		for _, item := range v {
			row := asMap(item)
			if len(row) == 0 {
				continue
			}
			name := firstString(row, "name", "Name")
			value := firstString(row, "value", "Value")
			if name != "" && value != "" {
				cookies = append(cookies, &http.Cookie{Name: name, Value: value})
			}
		}
		return cookiesFromHTTP(cookies)
	default:
		return ""
	}
}

func cookiesFromHTTP(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" || strings.TrimSpace(cookie.Value) == "" {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func cookieValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(strings.Trim(v, "\""))
	case json.Number:
		return v.String()
	case float64:
		if v != 0 {
			return strconv.FormatInt(int64(v), 10)
		}
	case int64:
		if v != 0 {
			return strconv.FormatInt(v, 10)
		}
	case int:
		if v != 0 {
			return strconv.Itoa(v)
		}
	}
	return ""
}

func hasCookieKey(parts []string, key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, part := range parts {
		name, _, _ := strings.Cut(part, "=")
		if strings.ToLower(strings.TrimSpace(name)) == key {
			return true
		}
	}
	return false
}

func qrStatusMessage(status, fallback string) string {
	switch status {
	case "0":
		return "等待扫码"
	case "1":
		return "已扫码，请在手机端确认"
	case "2":
		return "已确认，可以完成登录"
	case "-1", "-2":
		return "二维码已过期，请重新生成"
	}
	if fallback != "" && fallback != "未知错误" {
		return fallback
	}
	if status != "" {
		return "扫码状态：" + status
	}
	return "等待扫码"
}

func extractArray(payload map[string]any) []map[string]any {
	for _, key := range []string{"data", "list", "items", "files"} {
		switch value := payload[key].(type) {
		case []any:
			return mapsFromArray(value)
		case map[string]any:
			if rows := extractArray(value); len(rows) > 0 {
				return rows
			}
		}
	}
	return nil
}

func recentLastData(payload map[string]any) string {
	for _, row := range []map[string]any{payload, asMap(payload["data"])} {
		if len(row) == 0 {
			continue
		}
		if value := firstString(row, "last_data"); value != "" {
			return value
		}
		if value, ok := row["last_data"]; ok {
			if data, err := json.Marshal(value); err == nil {
				return string(data)
			}
		}
	}
	return ""
}

func mapsFromArray(values []any) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if row := asMap(value); len(row) > 0 {
			out = append(out, row)
		}
	}
	return out
}

func asMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	if row, ok := value.(map[string]any); ok {
		return row
	}
	return nil
}

func extractDownloadURL(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"url", "file_url", "download_url", "downurl"} {
			if raw := firstString(v, key); strings.HasPrefix(raw, "http") {
				return raw
			}
			if nested := asMap(v[key]); len(nested) > 0 {
				if raw := firstString(nested, "url", "file_url", "download_url"); strings.HasPrefix(raw, "http") {
					return raw
				}
			}
		}
		for _, child := range v {
			if raw := extractDownloadURL(child); raw != "" {
				return raw
			}
		}
	case []any:
		for _, child := range v {
			if raw := extractDownloadURL(child); raw != "" {
				return raw
			}
		}
	}
	return ""
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			switch v := value.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			case json.Number:
				return v.String()
			case float64:
				if v != 0 {
					return strconv.FormatInt(int64(v), 10)
				}
			case int64:
				if v != 0 {
					return strconv.FormatInt(v, 10)
				}
			case int:
				if v != 0 {
					return strconv.Itoa(v)
				}
			}
		}
	}
	return ""
}

func firstInt64(row map[string]any, keys ...string) int64 {
	raw := firstString(row, keys...)
	if raw == "" {
		return 0
	}
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}

func firstBool(row map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch v := row[key].(type) {
		case bool:
			return v
		case json.Number:
			n, _ := v.Int64()
			return n != 0
		case float64:
			return v != 0
		case string:
			return v == "1" || strings.EqualFold(v, "true")
		}
	}
	return false
}

func hasAny(row map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := row[key]; ok {
			return true
		}
	}
	return false
}
