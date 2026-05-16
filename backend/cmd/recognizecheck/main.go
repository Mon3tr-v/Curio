package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"curio/internal/clouddrive"
	"curio/internal/models"
	"curio/internal/parser"
	"curio/internal/scanner"
	"curio/internal/scraper"

	"github.com/redis/go-redis/v9"
)

type pathFlags []string

func (p *pathFlags) String() string { return strings.Join(*p, ",") }
func (p *pathFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("path is empty")
	}
	*p = append(*p, value)
	return nil
}

type report struct {
	Roots              []string     `json:"roots"`
	Scanned            int          `json:"scanned"`
	Media              int          `json:"media"`
	Matched            int          `json:"matched"`
	Failed             int          `json:"failed"`
	SkippedUnsupported int          `json:"skipped_unsupported"`
	SkippedSmall       int          `json:"skipped_small"`
	Results            []itemResult `json:"results,omitempty"`
}

type itemResult struct {
	Path        string `json:"path"`
	ParsedType  string `json:"parsed_type"`
	ParsedTitle string `json:"parsed_title"`
	Year        int    `json:"year,omitempty"`
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	TargetTitle string `json:"target_title,omitempty"`
	TMDBID      int    `json:"tmdb_id,omitempty"`
	Error       string `json:"error,omitempty"`
}

func main() {
	ctx := context.Background()
	var roots pathFlags
	apiBase := flag.String("api", "http://localhost:8080", "Curio API base URL")
	limit := flag.Int("limit", 0, "maximum media files to test, 0 means all")
	workers := flag.Int("workers", 3, "concurrent TMDB workers")
	failLimit := flag.Int("fail-limit", 30, "maximum failed rows printed to stdout")
	out := flag.String("out", "", "optional JSON report path")
	pathsFile := flag.String("paths-file", "", "optional UTF-8 file with one CloudDrive2 path per line")
	redisAddr := flag.String("redis", "", "optional Redis address for TMDB response cache")
	flag.Var(&roots, "path", "CloudDrive2 path to test; repeat for multiple roots")
	flag.Parse()
	if strings.TrimSpace(*pathsFile) != "" {
		loaded, err := loadPathFile(*pathsFile)
		if err != nil {
			fatal(err.Error())
		}
		roots = append(roots, loaded...)
	}
	if len(roots) == 0 {
		fatal("at least one -path is required")
	}
	if *workers < 1 {
		*workers = 1
	}

	settings, err := loadSettings(ctx, strings.TrimRight(*apiBase, "/"))
	if err != nil {
		fatal(err.Error())
	}
	cd := models.CloudDriveSettings{
		Address:                   settings.CloudDriveAddress,
		Username:                  settings.CloudDriveUsername,
		Password:                  settings.CloudDrivePassword,
		Token:                     settings.CloudDriveToken,
		RootPath:                  settings.CloudDriveRootPath,
		StagingPath:               settings.CloudDriveStagingPath,
		FailedPath:                settings.CloudDriveFailedPath,
		IncompleteCollectionsPath: settings.CloudDriveIncompletePath,
	}
	files, err := scanRoots(ctx, cd, roots, *limit)
	if err != nil {
		fatal(err.Error())
	}

	var redisClient *redis.Client
	if strings.TrimSpace(*redisAddr) != "" {
		redisClient = redis.NewClient(&redis.Options{Addr: strings.TrimSpace(*redisAddr)})
		defer redisClient.Close()
	}
	client := scraper.New(settings.TMDBAPIKey, settings.NetworkProxy, redisClient)
	rep := runRecognition(ctx, client, roots, files, *limit, *workers)
	if *out != "" {
		payload, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(*out, payload, 0o644); err != nil {
			fatal(err.Error())
		}
	}
	printReport(rep, *failLimit)
}

func loadPathFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var paths []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}
	return paths, scanner.Err()
}

func loadSettings(ctx context.Context, apiBase string) (models.SystemSettings, error) {
	var settings models.SystemSettings
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/api/settings/system", nil)
	if err != nil {
		return settings, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return settings, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return settings, fmt.Errorf("settings request returned %d", res.StatusCode)
	}
	return settings, json.NewDecoder(res.Body).Decode(&settings)
}

func scanRoots(ctx context.Context, settings models.CloudDriveSettings, roots []string, mediaLimit int) ([]scanner.File, error) {
	client := clouddrive.New(settings)
	files := make([]scanner.File, 0)
	media := 0
	for _, root := range roots {
		fmt.Fprintf(os.Stderr, "scan %s\n", root)
		err := scanWalk(ctx, client, root, &files, &media, mediaLimit)
		if err != nil && !errors.Is(err, errScanLimit) {
			return nil, fmt.Errorf("%s: %w", root, err)
		}
		fmt.Fprintf(os.Stderr, "scan done %s media=%d files=%d\n", root, media, len(files))
		if errors.Is(err, errScanLimit) {
			break
		}
	}
	return files, nil
}

var errScanLimit = errors.New("scan limit reached")

func scanWalk(ctx context.Context, client *clouddrive.Client, dir string, files *[]scanner.File, media *int, mediaLimit int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	children, err := client.List(ctx, dir)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.IsDirectory {
			if err := scanWalk(ctx, client, child.Path, files, media, mediaLimit); err != nil {
				return err
			}
			continue
		}
		scanned := scanner.File{
			Path:      child.URI,
			Name:      child.Name,
			Extension: child.Extension,
			Size:      child.Size,
			Hash:      child.Hash,
			HashType:  child.HashType,
		}
		if _, ok := scanner.SupportedExtensions["."+child.Extension]; !ok {
			scanned.ErrorCode = models.ErrUnsupportedExtension
			scanned.ErrorMessage = "unsupported extension"
		} else if child.Size < scanner.MinFileSize {
			scanned.ErrorCode = models.ErrFileTooSmall
			scanned.ErrorMessage = fmt.Sprintf("file size %d has no valid content", child.Size)
		} else {
			*media = *media + 1
			if *media%100 == 0 {
				fmt.Fprintf(os.Stderr, "scan media=%d current=%s\n", *media, child.Path)
			}
		}
		*files = append(*files, scanned)
		if mediaLimit > 0 && *media >= mediaLimit {
			return errScanLimit
		}
	}
	return nil
}

func runRecognition(ctx context.Context, client *scraper.Client, roots []string, files []scanner.File, limit, workers int) report {
	rep := report{Roots: roots, Scanned: len(files), Results: make([]itemResult, 0)}
	media := make([]scanner.File, 0, len(files))
	for _, file := range files {
		switch file.ErrorCode {
		case models.ErrUnsupportedExtension:
			rep.SkippedUnsupported++
			continue
		case models.ErrFileTooSmall:
			rep.SkippedSmall++
			continue
		}
		if file.ErrorCode != "" {
			rep.Failed++
			rep.Results = append(rep.Results, itemResult{Path: file.Path, Error: file.ErrorCode + ": " + file.ErrorMessage})
			continue
		}
		if limit > 0 && len(media) >= limit {
			continue
		}
		media = append(media, file)
	}
	rep.Media = len(media)

	jobs := make(chan scanner.File)
	results := make(chan itemResult)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				results <- recognizeOne(ctx, client, file)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, file := range media {
			jobs <- file
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	processed := 0
	for result := range results {
		if result.Error == "" {
			rep.Matched++
		} else {
			rep.Failed++
		}
		rep.Results = append(rep.Results, result)
		processed++
		if processed%50 == 0 || processed == rep.Media {
			fmt.Fprintf(os.Stderr, "recognize %d/%d matched=%d failed=%d\n", processed, rep.Media, rep.Matched, rep.Failed)
		}
	}
	return rep
}

func recognizeOne(ctx context.Context, client *scraper.Client, file scanner.File) itemResult {
	item := itemResult{Path: file.Path}
	parsed, err := parser.ParsePath(file.Path)
	if err != nil {
		item.Error = err.Error()
		return item
	}
	item.ParsedType = "movie"
	item.ParsedTitle = parsed.Title
	item.Year = parsed.Year
	if parsed.IsTV {
		item.ParsedType = "tv"
		item.ParsedTitle = parsed.ShowTitle
		item.Year = parsed.ShowYear
		item.Season = parsed.Season
		item.Episode = parsed.Episode
	}
	scrapeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	match, err := client.Scrape(scrapeCtx, parsed)
	if err != nil {
		item.Error = err.Error()
		return item
	}
	switch match.MediaType {
	case models.MediaTVEpisode:
		item.TMDBID = match.TVShow.TMDBID
		item.TargetTitle = fmt.Sprintf("%s S%02dE%02d", match.TVShow.Name, match.TVEpisode.Season, match.TVEpisode.Episode)
	default:
		item.TMDBID = match.Movie.TMDBID
		item.TargetTitle = match.Movie.Title
	}
	return item
}

func printReport(rep report, failLimit int) {
	fmt.Printf("scanned=%d media=%d matched=%d failed=%d skipped_unsupported=%d skipped_small=%d\n",
		rep.Scanned, rep.Media, rep.Matched, rep.Failed, rep.SkippedUnsupported, rep.SkippedSmall)
	printed := 0
	for _, item := range rep.Results {
		if item.Error == "" {
			continue
		}
		if printed >= failLimit {
			break
		}
		fmt.Printf("FAIL %s | %s | %s\n", item.Error, item.ParsedTitle, item.Path)
		printed++
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
