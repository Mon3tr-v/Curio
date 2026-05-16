package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"curio/internal/classifier"
	"curio/internal/clouddrive"
	"curio/internal/collection"
	"curio/internal/mediainfo"
	"curio/internal/models"
	"curio/internal/naming"
	"curio/internal/organizer"
	"curio/internal/parser"
	"curio/internal/repository"
	"curio/internal/scanner"
	"curio/internal/scraper"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	store   *repository.Store
	scraper *scraper.Client
	checker *collection.Checker
	redis   *redis.Client
	mu      sync.Mutex
	active  map[string]*activeTask
}

type activeTask struct {
	batchID string
	source  string
	cancel  context.CancelFunc
	unlock  func()
}

type RearchiveOptions struct {
	TMDBID        int
	MediaType     string
	Season        int
	Episode       int
	SeasonOffset  int
	EpisodeOffset int
}

type RearchiveFailure struct {
	FileID  string `json:"file_id"`
	Message string `json:"message"`
}

var ErrTaskAlreadyRunning = errors.New("已有扫描任务正在运行")
var ErrTaskNotFound = errors.New("没有正在运行的任务")
var ErrCloudDriveNotReady = errors.New("CloudDrive2 未就绪")

const scanFileConcurrency = 2

var (
	chsSubtitleRE     = regexp.MustCompile(`(?i)(^|[ ._\-\[\]()])(?:chs|sc|zh[-_]?cn|zh[-_]?sg|zh[-_]?hans|hans|gb2312|gbk|简体|简中)(?:$|[ ._\-\[\]()])`)
	chtSubtitleRE     = regexp.MustCompile(`(?i)(^|[ ._\-\[\]()])(?:cht|tc|zh[-_]?tw|zh[-_]?hk|zh[-_]?hant|hant|big5|繁体|繁中)(?:$|[ ._\-\[\]()])`)
	chsSubtitleTokens = map[string]struct{}{
		"chs": {}, "sc": {}, "hans": {}, "gb": {}, "gbk": {}, "gb2312": {}, "gb18030": {},
		"zhcn": {}, "zhsg": {}, "zhhans": {}, "jpsc": {}, "jpchs": {}, "jpschs": {},
		"scjp": {}, "chsjp": {}, "jphans": {}, "simplified": {},
	}
	chtSubtitleTokens = map[string]struct{}{
		"cht": {}, "tc": {}, "hant": {}, "big5": {},
		"zhtw": {}, "zhhk": {}, "zhmo": {}, "zhhant": {}, "jptc": {}, "jpcht": {}, "jptcht": {},
		"tcjp": {}, "chtjp": {}, "jphant": {}, "traditional": {},
	}
)

func New(store *repository.Store, scraperClient *scraper.Client, checker *collection.Checker, redisClient *redis.Client) *Service {
	return &Service{store: store, scraper: scraperClient, checker: checker, redis: redisClient, active: map[string]*activeTask{}}
}

func (s *Service) StartScan(ctx context.Context) (string, error) {
	dirs, err := s.store.Directories(ctx)
	if err != nil {
		return "", err
	}
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return "", err
	}
	batchID, runCtx, err := s.start(ctx, models.BatchSourceLocal)
	if err != nil {
		return "", err
	}
	go s.runBatch(runCtx, batchID, dirs, settings.ClassificationYAML)
	return batchID, nil
}

func (s *Service) StartCloudDriveScan(ctx context.Context) (string, error) {
	settings, err := s.store.CloudDriveSettings(ctx)
	if err != nil {
		return "", err
	}
	if err := s.checkCloudDrive(ctx, settings); err != nil {
		return "", fmt.Errorf("%w：%v", ErrCloudDriveNotReady, err)
	}
	systemSettings, err := s.store.Settings(ctx)
	if err != nil {
		return "", err
	}
	batchID, runCtx, err := s.start(ctx, models.BatchSourceCloud)
	if err != nil {
		return "", err
	}
	go s.runCloudDriveBatch(runCtx, batchID, settings, systemSettings.ClassificationYAML)
	return batchID, nil
}

func (s *Service) start(ctx context.Context, source string) (string, context.Context, error) {
	s.mu.Lock()
	if len(s.active) > 0 {
		s.mu.Unlock()
		return "", nil, ErrTaskAlreadyRunning
	}
	s.mu.Unlock()
	if _, ok, err := s.store.ActiveBatch(ctx); err != nil {
		return "", nil, err
	} else if ok {
		return "", nil, ErrTaskAlreadyRunning
	}
	locked, unlockGlobal, err := s.acquireLock(ctx, "lock:scan:active", 24*time.Hour)
	if err != nil {
		return "", nil, err
	}
	if !locked {
		return "", nil, ErrTaskAlreadyRunning
	}
	locked, unlockSource, err := s.acquireLock(ctx, "lock:scan:"+source, 24*time.Hour)
	if err != nil {
		unlockGlobal()
		return "", nil, err
	}
	if !locked {
		unlockGlobal()
		return "", nil, ErrTaskAlreadyRunning
	}
	unlock := func() {
		unlockSource()
		unlockGlobal()
	}
	batchID := uuid.NewString()
	runCtx, cancel := context.WithCancel(context.Background())
	if err := s.store.CreateBatch(ctx, batchID, source); err != nil {
		cancel()
		unlock()
		return "", nil, err
	}
	s.mu.Lock()
	if len(s.active) > 0 {
		s.mu.Unlock()
		cancel()
		unlock()
		_ = s.store.FinishBatch(ctx, batchID, models.BatchStatusCancelled)
		return "", nil, ErrTaskAlreadyRunning
	}
	s.active[batchID] = &activeTask{batchID: batchID, source: source, cancel: cancel, unlock: unlock}
	s.mu.Unlock()
	return batchID, runCtx, nil
}

func (s *Service) ActiveBatch(ctx context.Context) (models.Batch, bool, error) {
	s.mu.Lock()
	for batchID := range s.active {
		s.mu.Unlock()
		batch, err := s.store.Batch(ctx, batchID)
		return batch, err == nil, err
	}
	s.mu.Unlock()
	return s.store.ActiveBatch(ctx)
}

func (s *Service) Stop(ctx context.Context, batchID string) (models.Batch, error) {
	s.mu.Lock()
	if batchID == "" && len(s.active) == 1 {
		for id := range s.active {
			batchID = id
		}
	}
	task := s.active[batchID]
	s.mu.Unlock()
	if batchID == "" {
		return models.Batch{}, ErrTaskNotFound
	}
	if task != nil {
		_ = s.store.SetBatchStatus(ctx, batchID, models.BatchStatusCancelling)
		task.cancel()
		return s.store.Batch(ctx, batchID)
	}
	batch, err := s.store.Batch(ctx, batchID)
	if err != nil {
		return models.Batch{}, err
	}
	if isActiveBatchStatus(batch.Status) {
		if err := s.store.FinishBatch(ctx, batchID, models.BatchStatusInterrupted); err != nil {
			return models.Batch{}, err
		}
		return s.store.Batch(ctx, batchID)
	}
	return batch, nil
}

func (s *Service) RearchiveByTMDBID(ctx context.Context, fileID string, tmdbID int) (models.MediaFile, error) {
	return s.RearchiveMedia(ctx, fileID, RearchiveOptions{TMDBID: tmdbID})
}

func (s *Service) RearchiveMediaBatch(ctx context.Context, fileIDs []string, options RearchiveOptions) ([]models.MediaFile, []RearchiveFailure, error) {
	files := make([]models.MediaFile, 0, len(fileIDs))
	failures := make([]RearchiveFailure, 0)
	for _, fileID := range fileIDs {
		if err := ctx.Err(); err != nil {
			return files, failures, err
		}
		file, err := s.RearchiveMedia(ctx, fileID, options)
		if err != nil {
			failures = append(failures, RearchiveFailure{FileID: fileID, Message: err.Error()})
			continue
		}
		files = append(files, file)
	}
	return files, failures, nil
}

func (s *Service) RearchiveMedia(ctx context.Context, fileID string, options RearchiveOptions) (models.MediaFile, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return models.MediaFile{}, errors.New("媒体文件 ID 不能为空")
	}
	if options.TMDBID < 0 {
		return models.MediaFile{}, errors.New("TMDB ID 不能小于 0")
	}
	file, err := s.store.MediaFile(ctx, fileID)
	if err != nil {
		return models.MediaFile{}, err
	}
	if file.ProcessStatus != models.StatusFailed && file.ProcessStatus != models.StatusDone && file.ProcessStatus != models.StatusIncompleteCollection {
		return models.MediaFile{}, errors.New("仅支持失败或已归档记录重新归档")
	}
	sourcePath := mediaSourcePath(file)
	if sourcePath == "" {
		return models.MediaFile{}, errors.New("媒体文件缺少可移动路径")
	}
	file.CurrentPath = sourcePath
	parsed, err := rearchiveParsed(file, options)
	if err != nil {
		return models.MediaFile{}, err
	}
	if file.Extension == "" {
		file.Extension = parsed.Extension
	}
	dirs, err := s.rearchiveDirs(ctx, file)
	if err != nil {
		return models.MediaFile{}, err
	}
	systemSettings, err := s.store.Settings(ctx)
	if err != nil {
		return models.MediaFile{}, err
	}
	var drive *clouddrive.DriveSession
	if clouddrive.IsURI(file.CurrentPath) {
		settings, err := s.store.CloudDriveSettings(ctx)
		if err != nil {
			return models.MediaFile{}, err
		}
		drive, err = clouddrive.New(settings).Open(ctx)
		if err != nil {
			return models.MediaFile{}, err
		}
		defer drive.Close()
	}
	result, err := s.scraper.Scrape(ctx, parsed)
	if err != nil {
		return models.MediaFile{}, err
	}
	if err := s.persistMetadata(ctx, result); err != nil {
		return models.MediaFile{}, err
	}
	technical := mediaTechnical(file)
	templateType := result.MediaType
	root := dirs.StagingPath
	finalStatus := models.StatusDone
	completeCollection := false
	values := namingValues(parsed, technical, result)
	category, err := classifier.Match(systemSettings.ClassificationYAML, result.MediaType, classifierItem(result))
	if err != nil {
		return models.MediaFile{}, err
	}
	if category != "" {
		values["category"] = category
	}
	if result.MediaType == models.MediaCollectionMovie {
		check, err := s.checker.Check(ctx, result.Collection.TMDBID, result.Movie.TMDBID)
		if err != nil {
			return models.MediaFile{}, err
		}
		status := "incomplete"
		if check.Complete {
			status = "complete"
			completeCollection = true
		}
		_ = s.store.UpdateCollectionStatus(ctx, result.Collection.TMDBID, check.LocalCount, status)
		if !check.Complete {
			templateType = models.TemplateIncompleteCollection
			root = dirs.IncompleteCollectionsPath
			finalStatus = models.StatusIncompleteCollection
		} else {
			templateType = models.TemplateCollectionMovie
		}
	}
	template, err := s.store.Template(ctx, templateType)
	if err != nil {
		return models.MediaFile{}, err
	}
	renderTemplate := templateWithImplicitCategory(template.Template, values)
	technical, err = s.ensureTechnicalForTemplate(ctx, file, technical, templateType, renderTemplate, drive)
	if err != nil {
		return models.MediaFile{}, err
	}
	values = namingValues(parsed, technical, result)
	if category != "" {
		values["category"] = category
	}
	renderTemplate = templateWithImplicitCategory(template.Template, values)
	_, targetPath, err := naming.Render(templateType, renderTemplate, values, root)
	if err != nil {
		return models.MediaFile{}, err
	}
	cloudSource := clouddrive.IsURI(file.CurrentPath)
	if !cloudSource {
		if _, err := os.Stat(targetPath); err == nil && filepath.Clean(file.CurrentPath) != filepath.Clean(targetPath) {
			return models.MediaFile{}, errors.New(models.ErrTargetPathExists)
		}
	}
	_ = s.store.IncrementMoveAttempt(ctx, file.ID)
	movedPath := targetPath
	if cloudSource {
		movedPath, err = drive.MoveFile(ctx, file.CurrentPath, targetPath)
	} else {
		err = organizer.MoveFileContext(ctx, file.CurrentPath, targetPath)
	}
	if err != nil {
		return models.MediaFile{}, err
	}
	finalName := filepath.Base(targetPath)
	if cloudSource {
		finalName = path.Base(clouddrive.FromURI(movedPath))
	}
	if err := s.persistMatch(ctx, file.ID, result); err != nil {
		return models.MediaFile{}, err
	}
	parseTitle, parseYear := parsedDisplay(parsed)
	for attempt := 0; attempt < 3; attempt++ {
		err = s.store.UpdateMediaRearchiveFinal(ctx, file.ID, parseTitle, parseYear, parsed.Season, parsed.Episode, parsed.Source, technical, result.MediaType, targetPath, movedPath, finalName, finalStatus)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	if err != nil {
		return models.MediaFile{}, err
	}
	_ = s.store.AddHistory(ctx, uuid.NewString(), "", file.ID, file.BatchID, file.CurrentPath, movedPath, "manual_rearchive", finalStatus, "", "")
	_ = s.store.RecountBatch(ctx, file.BatchID)
	_ = s.store.RefreshCollectionLocalCounts(ctx)
	if completeCollection {
		if cloudSource {
			s.migrateCompleteCloudCollection(ctx, dirs, result, values)
		} else {
			s.migrateCompleteCollection(ctx, dirs, result, values)
		}
	}
	return s.store.MediaFile(ctx, file.ID)
}

func (s *Service) Recover(ctx context.Context) error {
	if err := s.store.InterruptActiveBatches(ctx); err != nil {
		return err
	}
	if s.redis == nil {
		return nil
	}
	return s.redis.Del(ctx,
		"queue:scan",
		"queue:parse",
		"queue:scrape",
		"queue:match",
		"queue:collection_check",
		"queue:organize",
		"queue:failed",
		"lock:scan:active",
		"lock:scan:"+models.BatchSourceLocal,
		"lock:scan:"+models.BatchSourceCloud,
	).Err()
}

func (s *Service) runBatch(ctx context.Context, batchID string, dirs models.DirectoryConfig, classificationYAML string) {
	defer s.finishActive(batchID)
	err := s.runSafely(batchID, func() error {
		return s.withQueue(ctx, "queue:scan", batchID, func() error {
			if err := s.store.SetBatchStatus(ctx, batchID, models.BatchStatusRunning); err != nil && ctx.Err() == nil {
				return err
			}
			files, err := scanner.Scan(ctx, dirs.IncomingPath)
			if err != nil {
				return err
			}
			_ = s.store.SetBatchTotal(ctx, batchID, len(files))
			_ = s.progress(ctx, batchID)
			if err := s.processFiles(ctx, batchID, dirs, files, classificationYAML, nil); err != nil {
				return err
			}
			return s.progress(ctx, batchID)
		})
	})
	s.finishBatch(ctx, batchID, err)
}

func (s *Service) runCloudDriveBatch(ctx context.Context, batchID string, settings models.CloudDriveSettings, classificationYAML string) {
	dirs := cloudDriveDirs(settings)
	defer s.finishActive(batchID)
	err := s.runSafely(batchID, func() error {
		return s.withQueue(ctx, "queue:scan", batchID, func() error {
			if err := s.store.SetBatchStatus(ctx, batchID, models.BatchStatusRunning); err != nil && ctx.Err() == nil {
				return err
			}
			drive, err := clouddrive.New(settings).Open(ctx)
			if err != nil {
				return err
			}
			defer drive.Close()
			files, err := drive.Scan(ctx, settings.RootPath, settings.StagingPath, settings.FailedPath, settings.IncompleteCollectionsPath)
			if err != nil {
				return err
			}
			_ = s.store.SetBatchTotal(ctx, batchID, len(files))
			_ = s.progress(ctx, batchID)
			if err := s.processFiles(ctx, batchID, dirs, files, classificationYAML, drive); err != nil {
				return err
			}
			return s.progress(ctx, batchID)
		})
	})
	s.finishBatch(ctx, batchID, err)
}

func (s *Service) processFiles(ctx context.Context, batchID string, dirs models.DirectoryConfig, files []scanner.File, classificationYAML string, drive *clouddrive.DriveSession) error {
	if len(files) == 0 {
		return nil
	}
	workers := scanFileConcurrency
	if len(files) < workers {
		workers = len(files)
	}
	jobs := make(chan scanner.File)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for scanned := range jobs {
				if err := ctx.Err(); err != nil {
					sendFirstErr(errs, err)
					return
				}
				if err := s.processFile(ctx, batchID, dirs, scanned, classificationYAML, drive); isContextStopped(ctx, err) {
					sendFirstErr(errs, err)
					return
				}
			}
		}()
	}
	for _, scanned := range files {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- scanned:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

func sendFirstErr(errs chan<- error, err error) {
	if err == nil {
		return
	}
	select {
	case errs <- err:
	default:
	}
}

func (s *Service) runSafely(batchID string, fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("任务异常退出：%v", recovered)
			log.Printf("batch %s panic: %v\n%s", batchID, recovered, debug.Stack())
		}
	}()
	return fn()
}

func (s *Service) checkCloudDrive(ctx context.Context, settings models.CloudDriveSettings) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	drive, err := clouddrive.New(settings).Open(ctx)
	if err != nil {
		return fmt.Errorf("连接失败：%w%s", err, cloudDriveAddressHint(settings.Address))
	}
	defer drive.Close()
	root := clouddrive.NormalizePath(settings.RootPath)
	if _, err := drive.List(ctx, root); err != nil {
		return fmt.Errorf("读取根目录 %s 失败：%w%s", root, err, cloudDriveAddressHint(settings.Address))
	}
	return nil
}

func cloudDriveAddressHint(address string) string {
	value := strings.ToLower(strings.TrimSpace(address))
	if strings.Contains(value, "localhost") || strings.Contains(value, "127.0.0.1") || strings.Contains(value, "::1") {
		return "；当前 CloudDrive2 地址指向容器自身，在 NAS Docker 中请改为 http://host.docker.internal:19798 或 http://100.66.1.1:19798"
	}
	return ""
}

func (s *Service) finishActive(batchID string) {
	s.mu.Lock()
	task := s.active[batchID]
	delete(s.active, batchID)
	s.mu.Unlock()
	if task != nil && task.unlock != nil {
		task.unlock()
	}
}

func (s *Service) finishBatch(runCtx context.Context, batchID string, runErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status := models.BatchStatusComplete
	if runErr != nil {
		status = models.BatchStatusFailed
		if runCtx.Err() != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			status = models.BatchStatusCancelled
		}
	}
	_ = s.store.FinishBatch(ctx, batchID, status)
	_ = s.progress(ctx, batchID)
}

func isActiveBatchStatus(status string) bool {
	return status == models.BatchStatusQueued || status == models.BatchStatusRunning || status == models.BatchStatusCancelling
}

func isContextStopped(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *Service) processFile(ctx context.Context, batchID string, dirs models.DirectoryConfig, scanned scanner.File, classificationYAML string, drive *clouddrive.DriveSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file := models.MediaFile{
		ID:            uuid.NewString(),
		BatchID:       batchID,
		OriginalPath:  scanned.Path,
		CurrentPath:   scanned.Path,
		OriginalName:  scanned.Name,
		Extension:     scanned.Extension,
		FileSize:      scanned.Size,
		FileHash:      scanned.Hash,
		HashType:      scanned.HashType,
		ProcessStatus: models.StatusIncoming,
	}
	if err := s.store.CreateMediaFile(ctx, file); err != nil {
		_ = s.store.IncrementBatch(ctx, batchID, models.StatusFailed)
		_ = s.progress(ctx, batchID)
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if file.FileHash != "" {
		locked, unlock, err := s.acquireLock(ctx, "lock:file:"+file.FileHash, time.Hour)
		if err != nil {
			s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
			return err
		}
		if !locked {
			s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, "文件正在被其他任务处理")
			return nil
		}
		defer unlock()
	}
	if scanned.ErrorCode != "" {
		s.fail(ctx, dirs, file, "", scanned.ErrorCode, scanned.ErrorMessage)
		return nil
	}
	return s.withQueue(ctx, "queue:parse", file.ID, func() error {
		if err := s.store.UpdateMediaStatus(ctx, file.ID, models.StatusScanned); err != nil {
			s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
			return err
		}
		parsed, err := parser.ParsePath(file.OriginalPath)
		if err != nil {
			code, message := parseErr(err)
			s.fail(ctx, dirs, file, "", code, message)
			return err
		}
		technical := unknownTechnical()
		parseTitle := parsed.Title
		parseYear := parsed.Year
		if parsed.IsTV {
			parseTitle = parsed.ShowTitle
			parseYear = parsed.ShowYear
		}
		if err := s.store.UpdateMediaParsed(ctx, file.ID, parseTitle, parseYear, parsed.Season, parsed.Episode, parsed.Source, technical); err != nil {
			s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
			return err
		}
		return s.scrapeAndOrganize(ctx, dirs, file, parsed, technical, classificationYAML, scanned.Sidecars, drive)
	})
}

func (s *Service) scrapeAndOrganize(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, parsed parser.Result, technical models.MediaTechnicalInfo, classificationYAML string, sidecars []scanner.Sidecar, drive *clouddrive.DriveSession) error {
	var result scraper.Result
	err := s.withQueue(ctx, "queue:scrape", file.ID, func() error {
		var scrapeErr error
		result, scrapeErr = s.scraper.Scrape(ctx, parsed)
		return scrapeErr
	})
	if err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		code, message := scrapeErr(err)
		s.fail(ctx, dirs, file, "", code, message)
		return err
	}
	if err := s.persistMatch(ctx, file.ID, result); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	if err := s.store.UpdateMediaStatus(ctx, file.ID, models.StatusScraped); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	if err := s.withQueue(ctx, "queue:match", file.ID, func() error {
		return s.store.UpdateMediaMatched(ctx, file.ID, result.MediaType)
	}); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	return s.planAndMove(ctx, dirs, file, parsed, technical, result, classificationYAML, sidecars, drive)
}

func (s *Service) planAndMove(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, parsed parser.Result, technical models.MediaTechnicalInfo, result scraper.Result, classificationYAML string, sidecars []scanner.Sidecar, drive *clouddrive.DriveSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cloudSource := clouddrive.IsURI(file.CurrentPath)
	templateType := result.MediaType
	root := dirs.StagingPath
	finalStatus := models.StatusDone
	completeCollection := false
	values := namingValues(parsed, technical, result)
	category, err := classifier.Match(classificationYAML, result.MediaType, classifierItem(result))
	if err != nil {
		s.fail(ctx, dirs, file, "", models.ErrTemplateFieldInvalid, err.Error())
		return err
	}
	if category != "" {
		values["category"] = category
	}
	if result.MediaType == models.MediaCollectionMovie {
		var check collection.CheckResult
		err := s.withQueue(ctx, "queue:collection_check", file.ID, func() error {
			var checkErr error
			check, checkErr = s.checker.Check(ctx, result.Collection.TMDBID, result.Movie.TMDBID)
			return checkErr
		})
		if err != nil {
			if isContextStopped(ctx, err) {
				return err
			}
			s.fail(ctx, dirs, file, "", models.ErrCollectionCheckFailed, err.Error())
			return err
		}
		status := "incomplete"
		if check.Complete {
			status = "complete"
			completeCollection = true
		}
		_ = s.store.UpdateCollectionStatus(ctx, result.Collection.TMDBID, check.LocalCount, status)
		if !check.Complete {
			templateType = models.TemplateIncompleteCollection
			root = dirs.IncompleteCollectionsPath
			finalStatus = models.StatusIncompleteCollection
		} else {
			templateType = models.TemplateCollectionMovie
		}
	}
	if err := s.store.UpdateMediaStatus(ctx, file.ID, models.StatusCollectionChecked); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	template, err := s.store.Template(ctx, templateType)
	if err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", models.ErrTemplateNotFound, err.Error())
		return err
	}
	var targetPath string
	renderTemplate := templateWithImplicitCategory(template.Template, values)
	technical, err = s.ensureTechnicalForTemplate(ctx, file, technical, templateType, renderTemplate, drive)
	if err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", workerErrorCode(err, models.ErrMediaProbeFailed), err.Error())
		return err
	}
	values = namingValues(parsed, technical, result)
	if category != "" {
		values["category"] = category
	}
	renderTemplate = templateWithImplicitCategory(template.Template, values)
	if err := s.withQueue(ctx, "queue:organize", file.ID, func() error {
		_, target, renderErr := naming.Render(templateType, renderTemplate, values, root)
		targetPath = target
		return renderErr
	}); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, "", templateErrCode(err), err.Error())
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !cloudSource {
		if _, err := os.Stat(targetPath); err == nil {
			s.fail(ctx, dirs, file, "", models.ErrTargetPathExists, "目标路径已存在")
			return err
		}
	}
	taskID := uuid.NewString()
	if err := s.store.CreateTask(ctx, taskID, file.ID, file.BatchID, template.TemplateType, file.CurrentPath, targetPath, models.StatusPlanned); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, taskID, models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	if err := s.store.UpdateMediaPlanned(ctx, file.ID, targetPath); err != nil {
		if isContextStopped(ctx, err) {
			return err
		}
		s.fail(ctx, dirs, file, taskID, models.ErrDatabaseWriteFailed, err.Error())
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sourceDir := filepath.Dir(file.CurrentPath)
	if cloudSource {
		sourceDir = path.Dir(clouddrive.FromURI(file.CurrentPath))
	}
	_ = s.store.IncrementMoveAttempt(ctx, file.ID)
	movedPath := targetPath
	var moveErr error
	if cloudSource {
		if drive != nil {
			movedPath, moveErr = drive.MoveFile(ctx, file.CurrentPath, targetPath)
		} else {
			movedPath, moveErr = s.moveCloudFile(ctx, file.CurrentPath, targetPath)
		}
	} else {
		moveErr = organizer.MoveFileContext(ctx, file.CurrentPath, targetPath)
	}
	if moveErr != nil {
		if isContextStopped(ctx, moveErr) {
			return moveErr
		}
		code := models.ErrMoveToStagingFailed
		if finalStatus == models.StatusIncompleteCollection {
			code = models.ErrMoveToIncompleteCollectionFailed
		}
		message := moveErr.Error()
		if moveErr.Error() == models.ErrTargetPathExists {
			code = models.ErrTargetPathExists
			message = "目标路径已存在"
		}
		_ = s.store.FinishTask(ctx, taskID, models.StatusFailed, code, message)
		s.fail(ctx, dirs, file, taskID, code, message)
		return moveErr
	}
	finalName := filepath.Base(targetPath)
	if cloudSource {
		finalName = path.Base(clouddrive.FromURI(movedPath))
	}
	if err := s.finishMovedMedia(ctx, file, taskID, movedPath, finalName, finalStatus, cloudSource, drive); err != nil {
		return err
	}
	_ = s.store.FinishTask(ctx, taskID, models.StatusDone, "", "")
	_ = s.store.AddHistory(ctx, uuid.NewString(), taskID, file.ID, file.BatchID, file.CurrentPath, movedPath, "move", finalStatus, "", "")
	if err := s.moveSidecars(ctx, file, taskID, sidecars, targetPath, cloudSource, drive); err != nil {
		_ = s.store.AddHistory(ctx, uuid.NewString(), taskID, file.ID, file.BatchID, file.CurrentPath, targetPath, "subtitle_move", models.StatusFailed, models.ErrSubtitleMoveFailed, err.Error())
	}
	s.cleanupEmptyParents(ctx, dirs, sourceDir, sidecars, cloudSource, drive)
	_ = s.store.IncrementBatch(ctx, file.BatchID, finalStatus)
	if completeCollection {
		if cloudSource {
			s.migrateCompleteCloudCollection(ctx, dirs, result, values)
		} else {
			s.migrateCompleteCollection(ctx, dirs, result, values)
		}
	}
	return s.progress(ctx, file.BatchID)
}

func (s *Service) persistMetadata(ctx context.Context, result scraper.Result) error {
	switch result.MediaType {
	case models.MediaMovie:
		return s.store.UpsertMovie(ctx, result.Movie)
	case models.MediaCollectionMovie:
		collection := *result.Collection
		if err := s.store.UpsertCollection(ctx, collection); err != nil {
			return err
		}
		for _, part := range collection.Parts {
			if err := s.store.UpsertCollectionMovie(ctx, part); err != nil {
				return err
			}
		}
		if err := s.store.UpsertMovie(ctx, result.Movie); err != nil {
			return err
		}
		return nil
	case models.MediaTVEpisode:
		if err := s.store.UpsertTVShow(ctx, result.TVShow); err != nil {
			return err
		}
		episodes := result.TVEpisodes
		if len(episodes) == 0 {
			episodes = []models.TVEpisodeMetadata{result.TVEpisode}
		}
		for _, episode := range episodes {
			if err := s.store.UpsertTVEpisode(ctx, episode); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("未知媒体类型 %s", result.MediaType)
	}
}

func (s *Service) persistMatch(ctx context.Context, fileID string, result scraper.Result) error {
	if err := s.persistMetadata(ctx, result); err != nil {
		return err
	}
	switch result.MediaType {
	case models.MediaMovie, models.MediaCollectionMovie:
		return s.store.UpsertMediaMatch(ctx, fileID, result.MediaType, result.Movie.TMDBID, 0, "")
	case models.MediaTVEpisode:
		return s.store.UpsertMediaMatch(ctx, fileID, result.MediaType, 0, result.TVShow.TMDBID, result.TVEpisode.ID)
	default:
		return fmt.Errorf("未知媒体类型 %s", result.MediaType)
	}
}

func (s *Service) migrateCompleteCollection(ctx context.Context, dirs models.DirectoryConfig, result scraper.Result, values map[string]string) {
	_ = s.withLock(ctx, fmt.Sprintf("lock:collection:%d", result.Collection.TMDBID), time.Minute, func() error {
		collectionTemplate, err := s.store.Template(ctx, models.TemplateCollectionMovie)
		if err != nil {
			return err
		}
		incompleteTemplate, err := s.store.Template(ctx, models.TemplateIncompleteCollection)
		if err != nil {
			return err
		}
		incompleteRenderTemplate := templateWithImplicitCategory(incompleteTemplate.Template, values)
		collectionRenderTemplate := templateWithImplicitCategory(collectionTemplate.Template, values)
		sourceRoot, err := naming.TemplateRoot(models.TemplateIncompleteCollection, incompleteRenderTemplate, values, dirs.IncompleteCollectionsPath)
		if err != nil {
			return err
		}
		targetRoot, err := naming.CollectionRoot(collectionRenderTemplate, values, dirs.StagingPath)
		if err != nil {
			return err
		}
		if err := organizer.MigrateCollection(sourceRoot, targetRoot); err != nil {
			_ = s.store.AddHistory(ctx, uuid.NewString(), "", "", "", sourceRoot, targetRoot, "collection_complete_move", models.StatusFailed, models.ErrCollectionCompleteMoveFailed, err.Error())
			return err
		}
		_ = s.store.UpdateCollectionPathPrefix(ctx, result.Collection.TMDBID, sourceRoot, targetRoot)
		_ = s.store.AddCollectionCompletionHistory(ctx, uuid.NewString(), result.Collection.TMDBID, result.Collection.Name, sourceRoot, targetRoot, result.Collection.MovieCount)
		return nil
	})
}

func (s *Service) fail(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, taskID, code, message string) {
	if ctx.Err() != nil {
		return
	}
	var failedPath string
	archiveErr := s.withQueue(ctx, "queue:failed", file.ID, func() error {
		var err error
		if clouddrive.IsURI(file.CurrentPath) {
			failedPath, err = s.archiveCloudFailure(ctx, dirs, file, code, message)
		} else {
			failedPath, err = organizer.ArchiveFailure(ctx, dirs, file, code, message)
		}
		return err
	})
	finalCode := code
	finalMessage := message
	currentPath := failedPath
	if archiveErr != nil {
		finalCode = models.ErrMoveToFailedDirFailed
		finalMessage = fmt.Sprintf("%s; original %s: %s", archiveErr.Error(), code, message)
		currentPath = file.CurrentPath
	} else if !clouddrive.IsURI(file.CurrentPath) {
		organizer.RemoveEmptyParents(filepath.Dir(file.CurrentPath), dirs.IncomingPath)
	}
	_ = s.store.UpdateMediaFailure(ctx, file.ID, currentPath, failedPath, finalCode, finalMessage)
	_ = s.store.AddHistory(ctx, uuid.NewString(), taskID, file.ID, file.BatchID, file.CurrentPath, failedPath, "failed_archive", models.StatusFailed, finalCode, finalMessage)
	_ = s.store.IncrementBatch(ctx, file.BatchID, models.StatusFailed)
	_ = s.progress(ctx, file.BatchID)
}

func (s *Service) moveCloudFile(ctx context.Context, sourceURI, targetPath string) (string, error) {
	settings, err := s.store.CloudDriveSettings(ctx)
	if err != nil {
		return "", err
	}
	return clouddrive.New(settings).MoveFile(ctx, sourceURI, targetPath)
}

func (s *Service) archiveCloudFailure(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, code, message string) (string, error) {
	settings, err := s.store.CloudDriveSettings(ctx)
	if err != nil {
		return "", err
	}
	return clouddrive.New(settings).ArchiveFailure(ctx, dirs.FailedPath, file, code, message)
}

func (s *Service) probeTechnical(ctx context.Context, file models.MediaFile, drive *clouddrive.DriveSession) (models.MediaTechnicalInfo, error) {
	source := mediainfo.Source{
		Path:      file.CurrentPath,
		Extension: file.Extension,
		Size:      file.FileSize,
	}
	if clouddrive.IsURI(file.CurrentPath) {
		if drive == nil {
			settings, err := s.store.CloudDriveSettings(ctx)
			if err != nil {
				return models.MediaTechnicalInfo{}, err
			}
			opened, err := clouddrive.New(settings).Open(ctx)
			if err != nil {
				return models.MediaTechnicalInfo{}, err
			}
			defer opened.Close()
			drive = opened
		}
		mode := clouddrive.ProbeDownloadMode()
		if strings.EqualFold(file.Extension, "iso") && mode == clouddrive.DownloadModeProxy && clouddrive.ProbePrefetchEnabled() {
			_ = drive.Prefetch(ctx, file.CurrentPath, []clouddrive.ByteRange{{Start: 0, Length: 16 * 1024 * 1024}})
		}
		download, err := drive.DownloadURLWithMode(ctx, file.CurrentPath, mode)
		if err != nil {
			return models.MediaTechnicalInfo{}, err
		}
		if download.Mode == string(clouddrive.DownloadModeProxy) {
			defer func() {
				closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = drive.CloseFileReader(closeCtx, file.CurrentPath)
			}()
		}
		source.Path = ""
		source.URL = mediainfo.CleanURL(download.URL)
		source.Headers = download.Headers
		source.UserAgent = download.UserAgent
	}
	info, err := mediainfo.Probe(ctx, source)
	if err != nil {
		return models.MediaTechnicalInfo{}, err
	}
	return models.MediaTechnicalInfo{
		Resolution:    info.Resolution,
		VideoCodec:    info.VideoCodec,
		AudioCodec:    info.AudioCodec,
		AudioChannels: info.AudioChannels,
		HDRFormat:     info.HDRFormat,
	}, nil
}

func (s *Service) ensureTechnicalForTemplate(ctx context.Context, file models.MediaFile, technical models.MediaTechnicalInfo, templateType, template string, drive *clouddrive.DriveSession) (models.MediaTechnicalInfo, error) {
	fields, err := naming.Fields(templateType, template)
	if err != nil {
		return technical, codedWorkerError{code: models.ErrTemplateFieldInvalid, err: err}
	}
	needsProbe := false
	for _, field := range fields {
		if isTechnicalField(field) && technicalFieldMissing(technical, field) {
			needsProbe = true
			break
		}
	}
	if !needsProbe {
		return technical, nil
	}
	probed, err := s.probeTechnical(ctx, file, drive)
	if err != nil {
		return technical, codedWorkerError{code: models.ErrMediaProbeFailed, err: err}
	}
	if err := s.store.UpdateMediaTechnical(ctx, file.ID, probed); err != nil {
		return probed, codedWorkerError{code: models.ErrDatabaseWriteFailed, err: err}
	}
	return probed, nil
}

func unknownTechnical() models.MediaTechnicalInfo {
	return models.MediaTechnicalInfo{
		Resolution:    mediainfo.Unknown,
		VideoCodec:    mediainfo.Unknown,
		AudioCodec:    mediainfo.Unknown,
		AudioChannels: mediainfo.Unknown,
		HDRFormat:     mediainfo.Unknown,
	}
}

func mediaTechnical(file models.MediaFile) models.MediaTechnicalInfo {
	technical := models.MediaTechnicalInfo{
		Resolution:    file.Resolution,
		VideoCodec:    file.VideoCodec,
		AudioCodec:    file.AudioCodec,
		AudioChannels: file.AudioChannels,
		HDRFormat:     file.HDRFormat,
	}
	if technical.Resolution == "" {
		technical.Resolution = mediainfo.Unknown
	}
	if technical.VideoCodec == "" {
		technical.VideoCodec = mediainfo.Unknown
	}
	if technical.AudioCodec == "" {
		technical.AudioCodec = mediainfo.Unknown
	}
	if technical.AudioChannels == "" {
		technical.AudioChannels = mediainfo.Unknown
	}
	if technical.HDRFormat == "" {
		technical.HDRFormat = mediainfo.Unknown
	}
	return technical
}

func isTechnicalField(field string) bool {
	switch field {
	case "resolution", "video_codec", "audio_codec", "audio_channels", "hdr_format":
		return true
	default:
		return false
	}
}

func technicalFieldMissing(technical models.MediaTechnicalInfo, field string) bool {
	value := ""
	switch field {
	case "resolution":
		value = technical.Resolution
	case "video_codec":
		value = technical.VideoCodec
	case "audio_codec":
		value = technical.AudioCodec
	case "audio_channels":
		value = technical.AudioChannels
	case "hdr_format":
		value = technical.HDRFormat
	}
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, mediainfo.Unknown)
}

func mediaSourcePath(file models.MediaFile) string {
	for _, value := range []string{file.CurrentPath, file.FinalPath, file.LastVerified, file.OriginalPath} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func rearchiveParsed(file models.MediaFile, options RearchiveOptions) (parser.Result, error) {
	var parsed parser.Result
	var err error
	for _, value := range []string{file.OriginalPath, file.CurrentPath, file.FinalPath, file.OriginalName} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err = parser.ParsePath(value)
		if err == nil {
			break
		}
	}
	if err != nil {
		parsed = parser.Result{
			Title:      fallbackTitle(file),
			Year:       file.ParseYear,
			Source:     valueOrUnknown(file.Source),
			Extension:  file.Extension,
			Resolution: mediainfo.Unknown,
			VideoCodec: mediainfo.Unknown,
		}
	}
	options.MediaType = strings.TrimSpace(options.MediaType)
	if options.MediaType == models.MediaMovie {
		parsed.IsTV = false
		parsed.Season = 0
		parsed.Episode = 0
		parsed.Season2 = ""
		parsed.Episode2 = ""
		if parsed.Title == "" {
			parsed.Title = fallbackTitle(file)
		}
		if parsed.Year == 0 {
			parsed.Year = file.ParseYear
		}
	}
	if parsed.Extension == "" {
		parsed.Extension = strings.TrimPrefix(strings.ToLower(filepath.Ext(file.OriginalName)), ".")
	}
	if parsed.Source == "" || strings.EqualFold(parsed.Source, mediainfo.Unknown) {
		parsed.Source = valueOrUnknown(file.Source)
	}
	forceTV := options.MediaType == models.MediaTVEpisode ||
		options.Season > 0 || options.Episode > 0 ||
		options.SeasonOffset != 0 || options.EpisodeOffset != 0
	if parsed.IsTV || forceTV || file.MediaType == models.MediaTVEpisode || file.Season > 0 || file.Episode > 0 {
		parsed.IsTV = true
		if parsed.Season == 0 {
			parsed.Season = file.Season
		}
		if parsed.Episode == 0 {
			parsed.Episode = file.Episode
		}
		parsed.Season += options.SeasonOffset
		parsed.Episode += options.EpisodeOffset
		if options.Season > 0 {
			parsed.Season = options.Season
		}
		if options.Episode > 0 {
			parsed.Episode = options.Episode
		}
		if parsed.Season < 0 || parsed.Episode <= 0 {
			return parser.Result{}, errors.New("季号不能小于 0，集号必须大于 0")
		}
		parsed.Season2 = fmt.Sprintf("%02d", parsed.Season)
		parsed.Episode2 = fmt.Sprintf("%02d", parsed.Episode)
		if parsed.ShowTitle == "" {
			parsed.ShowTitle = fallbackTitle(file)
		}
		if parsed.ShowYear == 0 {
			parsed.ShowYear = file.ParseYear
		}
		if parsed.Episode == 0 {
			return parser.Result{}, errors.New("剧集重新归档需要可识别的季号和集号")
		}
	}
	parsed.TMDBID = options.TMDBID
	return parsed, nil
}

func fallbackTitle(file models.MediaFile) string {
	if strings.TrimSpace(file.ParseTitle) != "" {
		return strings.TrimSpace(file.ParseTitle)
	}
	name := file.OriginalName
	if name == "" {
		name = filepath.Base(mediaSourcePath(file))
	}
	ext := filepath.Ext(name)
	return strings.TrimSpace(strings.TrimSuffix(name, ext))
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return mediainfo.Unknown
	}
	return strings.TrimSpace(value)
}

func parsedDisplay(parsed parser.Result) (string, int) {
	if parsed.IsTV {
		return parsed.ShowTitle, parsed.ShowYear
	}
	return parsed.Title, parsed.Year
}

func (s *Service) rearchiveDirs(ctx context.Context, file models.MediaFile) (models.DirectoryConfig, error) {
	if clouddrive.IsURI(file.CurrentPath) {
		settings, err := s.store.CloudDriveSettings(ctx)
		if err != nil {
			return models.DirectoryConfig{}, err
		}
		return cloudDriveDirs(settings), nil
	}
	return s.store.Directories(ctx)
}

func (s *Service) finishMovedMedia(ctx context.Context, file models.MediaFile, taskID, movedPath, finalName, finalStatus string, cloudSource bool, drive *clouddrive.DriveSession) error {
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.store.UpdateMediaFinal(ctx, file.ID, movedPath, movedPath, finalName, finalStatus); err == nil {
			return nil
		} else if attempt == 2 {
			verified := false
			if cloudSource && drive != nil {
				verified = drive.PathExists(ctx, movedPath)
			} else if !cloudSource {
				verified = organizer.PathExists(movedPath)
			}
			message := err.Error()
			if verified {
				message = "文件已移动到目标路径，但数据库状态更新失败：" + message
				_ = s.store.UpdateMediaFailure(ctx, file.ID, movedPath, movedPath, models.ErrDatabaseWriteFailed, message)
			}
			_ = s.store.FinishTask(ctx, taskID, models.StatusFailed, models.ErrDatabaseWriteFailed, message)
			_ = s.store.IncrementBatch(ctx, file.BatchID, models.StatusFailed)
			_ = s.progress(ctx, file.BatchID)
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	return nil
}

func (s *Service) moveSidecars(ctx context.Context, file models.MediaFile, taskID string, sidecars []scanner.Sidecar, mediaTarget string, cloudSource bool, drive *clouddrive.DriveSession) error {
	if len(sidecars) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, sidecar := range sidecars {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := subtitleTarget(mediaTarget, sidecar, counts)
		var err error
		var moved string
		if cloudSource {
			if drive != nil {
				moved, err = drive.MoveFile(ctx, sidecar.Path, target)
			} else {
				moved, err = s.moveCloudFile(ctx, sidecar.Path, target)
			}
		} else {
			err = organizer.MoveFileContext(ctx, sidecar.Path, target)
			moved = target
		}
		if err != nil {
			return err
		}
		_ = s.store.AddHistory(ctx, uuid.NewString(), taskID, file.ID, file.BatchID, sidecar.Path, moved, "subtitle_move", models.StatusDone, "", "")
	}
	return nil
}

func (s *Service) cleanupEmptyParents(ctx context.Context, dirs models.DirectoryConfig, sourceDir string, sidecars []scanner.Sidecar, cloudSource bool, drive *clouddrive.DriveSession) {
	sidecarDirs := make([]string, 0, len(sidecars))
	seen := map[string]struct{}{}
	for _, sidecar := range sidecars {
		dir := filepath.Dir(sidecar.Path)
		if cloudSource {
			dir = path.Dir(clouddrive.FromURI(sidecar.Path))
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		sidecarDirs = append(sidecarDirs, dir)
	}
	for _, dir := range sidecarDirs {
		if cloudSource && drive != nil {
			drive.DeleteEmptyParents(ctx, dir, dirs.IncomingPath)
		} else if !cloudSource {
			organizer.RemoveEmptyParents(dir, dirs.IncomingPath)
		}
	}
	if cloudSource && drive != nil {
		drive.DeleteEmptyParents(ctx, sourceDir, dirs.IncomingPath)
	} else if !cloudSource {
		organizer.RemoveEmptyParents(sourceDir, dirs.IncomingPath)
	}
}

func subtitleTarget(mediaTarget string, sidecar scanner.Sidecar, counts map[string]int) string {
	mediaExt := path.Ext(mediaTarget)
	if mediaExt == "" {
		mediaExt = filepath.Ext(mediaTarget)
	}
	base := strings.TrimSuffix(mediaTarget, mediaExt)
	ext := "." + strings.TrimPrefix(strings.ToLower(sidecar.Extension), ".")
	lang := subtitleLanguageSuffix(sidecar)
	key := lang + ext
	counts[key]++
	if counts[key] > 1 {
		return fmt.Sprintf("%s%s.%d%s", base, lang, counts[key], ext)
	}
	return base + lang + ext
}

func subtitleLanguageSuffix(sidecar scanner.Sidecar) string {
	if suffix := subtitleLanguageSuffixFromText(sidecar.Name); suffix != "" {
		return suffix
	}
	return subtitleLanguageSuffixFromText(sidecar.Path)
}

func subtitleLanguageSuffixFromText(text string) string {
	value := strings.ToLower(text)
	chs := hasSimplifiedSubtitleHint(value)
	cht := hasTraditionalSubtitleHint(value)
	switch {
	case chs && !cht:
		return ".chs"
	case cht && !chs:
		return ".cht"
	default:
		return ""
	}
}

func hasSimplifiedSubtitleHint(value string) bool {
	if chsSubtitleRE.MatchString(value) ||
		strings.Contains(value, "简体") ||
		strings.Contains(value, "簡體") ||
		strings.Contains(value, "简中") ||
		strings.Contains(value, "簡中") ||
		strings.Contains(value, "简日") ||
		strings.Contains(value, "日简") {
		return true
	}
	return hasSubtitleToken(value, chsSubtitleTokens)
}

func hasTraditionalSubtitleHint(value string) bool {
	if chtSubtitleRE.MatchString(value) ||
		strings.Contains(value, "繁体") ||
		strings.Contains(value, "繁體") ||
		strings.Contains(value, "繁中") ||
		strings.Contains(value, "繁日") ||
		strings.Contains(value, "日繁") {
		return true
	}
	return hasSubtitleToken(value, chtSubtitleTokens)
}

func hasSubtitleToken(value string, accepted map[string]struct{}) bool {
	for _, token := range subtitleTokens(value) {
		if _, ok := accepted[token]; ok {
			return true
		}
	}
	return false
}

func subtitleTokens(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func (s *Service) migrateCompleteCloudCollection(ctx context.Context, dirs models.DirectoryConfig, result scraper.Result, values map[string]string) {
	_ = s.withLock(ctx, fmt.Sprintf("lock:collection:%d", result.Collection.TMDBID), time.Minute, func() error {
		collectionTemplate, err := s.store.Template(ctx, models.TemplateCollectionMovie)
		if err != nil {
			return err
		}
		incompleteTemplate, err := s.store.Template(ctx, models.TemplateIncompleteCollection)
		if err != nil {
			return err
		}
		incompleteRenderTemplate := templateWithImplicitCategory(incompleteTemplate.Template, values)
		collectionRenderTemplate := templateWithImplicitCategory(collectionTemplate.Template, values)
		sourceRoot, err := naming.TemplateRoot(models.TemplateIncompleteCollection, incompleteRenderTemplate, values, dirs.IncompleteCollectionsPath)
		if err != nil {
			return err
		}
		targetRoot, err := naming.CollectionRoot(collectionRenderTemplate, values, dirs.StagingPath)
		if err != nil {
			return err
		}
		settings, err := s.store.CloudDriveSettings(ctx)
		if err != nil {
			return err
		}
		if err := clouddrive.New(settings).MigrateCollection(ctx, sourceRoot, targetRoot); err != nil {
			_ = s.store.AddHistory(ctx, uuid.NewString(), "", "", "", clouddrive.URI(sourceRoot), clouddrive.URI(targetRoot), "collection_complete_move", models.StatusFailed, models.ErrCollectionCompleteMoveFailed, err.Error())
			return err
		}
		_ = s.store.UpdateCollectionPathPrefix(ctx, result.Collection.TMDBID, clouddrive.URI(sourceRoot), clouddrive.URI(targetRoot))
		_ = s.store.AddCollectionCompletionHistory(ctx, uuid.NewString(), result.Collection.TMDBID, result.Collection.Name, clouddrive.URI(sourceRoot), clouddrive.URI(targetRoot), result.Collection.MovieCount)
		return nil
	})
}

func cloudDriveDirs(settings models.CloudDriveSettings) models.DirectoryConfig {
	return models.DirectoryConfig{
		IncomingPath:              clouddrive.NormalizePath(settings.RootPath),
		StagingPath:               clouddrive.NormalizePath(settings.StagingPath),
		FailedPath:                clouddrive.NormalizePath(settings.FailedPath),
		IncompleteCollectionsPath: clouddrive.NormalizePath(settings.IncompleteCollectionsPath),
	}
}

func (s *Service) progress(ctx context.Context, batchID string) error {
	if s.redis == nil {
		return nil
	}
	batch, err := s.store.Batch(ctx, batchID)
	if err != nil {
		return err
	}
	return s.redis.HSet(ctx, "progress:batch:"+batchID, map[string]any{
		"total":                 batch.Total,
		"done":                  batch.Done,
		"failed":                batch.Failed,
		"incomplete_collection": batch.IncompleteCollection,
	}).Err()
}

func (s *Service) withQueue(ctx context.Context, queue, value string, fn func() error) error {
	if s.redis != nil {
		_ = s.redis.LPush(ctx, queue, value).Err()
		defer s.redis.LRem(context.Background(), queue, 1, value)
	}
	return fn()
}

func (s *Service) withLock(ctx context.Context, key string, ttl time.Duration, fn func() error) error {
	locked, unlock, err := s.acquireLock(ctx, key, ttl)
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("%s 正在被其他任务占用", key)
	}
	defer unlock()
	return fn()
}

func (s *Service) acquireLock(ctx context.Context, key string, ttl time.Duration) (bool, func(), error) {
	if s.redis == nil {
		return true, func() {}, nil
	}
	locked, err := s.redis.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, func() {}, err
	}
	return locked, func() { _ = s.redis.Del(context.Background(), key).Err() }, nil
}

func namingValues(parsed parser.Result, technical models.MediaTechnicalInfo, result scraper.Result) map[string]string {
	values := map[string]string{
		"resolution":     technical.Resolution,
		"source":         parsed.Source,
		"video_codec":    technical.VideoCodec,
		"audio_codec":    technical.AudioCodec,
		"audio_channels": technical.AudioChannels,
		"hdr_format":     technical.HDRFormat,
		"extension":      parsed.Extension,
		"season":         strconv.Itoa(parsed.Season),
		"season_2":       parsed.Season2,
		"episode":        strconv.Itoa(parsed.Episode),
		"episode_2":      parsed.Episode2,
	}
	switch result.MediaType {
	case models.MediaMovie, models.MediaCollectionMovie:
		values["title"] = result.Movie.Title
		values["year"] = strconv.Itoa(result.Movie.Year)
		if result.Collection != nil {
			values["collection_name"] = result.Collection.Name
			values["collection_id"] = strconv.Itoa(result.Collection.TMDBID)
		}
	case models.MediaTVEpisode:
		values["show_title"] = result.TVShow.Name
		values["show_year"] = strconv.Itoa(result.TVShow.Year)
		values["episode_title"] = result.TVEpisode.Title
	}
	return values
}

func classifierItem(result scraper.Result) classifier.Item {
	switch result.MediaType {
	case models.MediaMovie, models.MediaCollectionMovie:
		return classifier.Item{
			GenreIDs:            splitCSV(result.Movie.GenreIDs),
			OriginalLanguage:    result.Movie.OriginalLanguage,
			ProductionCountries: splitCSV(result.Movie.ProductionCountries),
			Keywords:            splitCSV(result.Movie.Keywords),
		}
	case models.MediaTVEpisode:
		return classifier.Item{
			GenreIDs:         splitCSV(result.TVShow.GenreIDs),
			OriginalLanguage: result.TVShow.OriginalLanguage,
			OriginCountry:    splitCSV(result.TVShow.OriginCountry),
			Keywords:         splitCSV(result.TVShow.Keywords),
		}
	default:
		return classifier.Item{}
	}
}

func templateWithImplicitCategory(template string, values map[string]string) string {
	if strings.Contains(template, "{category}") || strings.TrimSpace(values["category"]) == "" {
		return template
	}
	template = strings.ReplaceAll(strings.TrimSpace(template), `\`, "/")
	parts := strings.Split(template, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return template
	}
	if isMediaTopDirectory(parts[0]) && len(parts) > 1 {
		return strings.Join(append([]string{parts[0], "{category}"}, parts[1:]...), "/")
	}
	return "{category}/" + template
}

func isMediaTopDirectory(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "movies", "movie", "tv", "collections", "collection":
		return true
	default:
		return false
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func parseErr(err error) (string, string) {
	var target parser.ParseError
	if errors.As(err, &target) {
		return target.Code, target.Message
	}
	return models.ErrParseTitleEmpty, err.Error()
}

func scrapeErr(err error) (string, string) {
	var target scraper.Error
	if errors.As(err, &target) {
		return target.Code, target.Message
	}
	return models.ErrScrapeRequestFailed, err.Error()
}

func templateErrCode(err error) string {
	message := err.Error()
	if strings.Contains(message, "escape") {
		return models.ErrTemplatePathEscape
	}
	return models.ErrTemplateFieldInvalid
}

type codedWorkerError struct {
	code string
	err  error
}

func (e codedWorkerError) Error() string {
	if e.err == nil {
		return e.code
	}
	return e.err.Error()
}

func (e codedWorkerError) Unwrap() error {
	return e.err
}

func workerErrorCode(err error, fallback string) string {
	var coded codedWorkerError
	if errors.As(err, &coded) && coded.code != "" {
		return coded.code
	}
	return fallback
}
