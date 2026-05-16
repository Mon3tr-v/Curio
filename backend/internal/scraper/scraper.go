package scraper

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"curio/internal/matcher"
	"curio/internal/models"
	"curio/internal/parser"

	"github.com/redis/go-redis/v9"
)

const tmdbBase = "https://api.themoviedb.org/3"
const maxSearchQueries = 6
const tmdbHTTPTimeout = 20 * time.Second
const tmdbMaxAttempts = 5

var searchLanguages = []string{"en-US", "zh-CN", "zh-SG"}

type Client struct {
	mu           sync.RWMutex
	apiKey       string
	networkProxy string
	http         *http.Client
	redis        *redis.Client
	memoryCache  map[string][]byte
}

type Result struct {
	MediaType  string
	Movie      models.MovieMetadata
	TVShow     models.TVShowMetadata
	TVEpisode  models.TVEpisodeMetadata
	TVEpisodes []models.TVEpisodeMetadata
	Collection *models.CollectionMetadata
}

type Error struct {
	Code    string
	Message string
}

func (e Error) Error() string { return e.Message }

type tmdbStatusError struct {
	Code int
}

func (e tmdbStatusError) Error() string {
	return fmt.Sprintf("TMDB 返回状态码 %d", e.Code)
}

func New(apiKey, networkProxy string, redisClient *redis.Client) *Client {
	client := &Client{redis: redisClient, memoryCache: map[string][]byte{}}
	_ = client.Configure(apiKey, networkProxy)
	return client
}

func (c *Client) Configure(apiKey, networkProxy string) error {
	httpClient, err := httpClient(networkProxy)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.apiKey = strings.TrimSpace(apiKey)
	c.networkProxy = strings.TrimSpace(networkProxy)
	c.http = httpClient
	return nil
}

func (c *Client) Scrape(ctx context.Context, parsed parser.Result) (Result, error) {
	apiKey, _ := c.current()
	if apiKey == "" {
		return Result{}, Error{Code: models.ErrScrapeRequestFailed, Message: "未配置 TMDB API Key"}
	}
	if parsed.IsTV {
		return c.scrapeTV(ctx, parsed)
	}
	return c.scrapeMovie(ctx, parsed)
}

func (c *Client) RefreshTVShow(ctx context.Context, showID int) (models.TVShowMetadata, []models.TVEpisodeMetadata, error) {
	apiKey, _ := c.current()
	if apiKey == "" {
		return models.TVShowMetadata{}, nil, Error{Code: models.ErrScrapeRequestFailed, Message: "未配置 TMDB API Key"}
	}
	detail, err := c.localizedTVDetail(ctx, showID)
	if err != nil {
		return models.TVShowMetadata{}, nil, err
	}
	episodes, err := c.localizedTVShowEpisodes(ctx, detail)
	if err != nil && len(episodes) == 0 {
		return models.TVShowMetadata{}, nil, err
	}
	return tvShowMetadataFromDetail(detail), episodes, err
}

func (c *Client) scrapeMovie(ctx context.Context, parsed parser.Result) (Result, error) {
	if parsed.TMDBID > 0 {
		detail, err := c.localizedMovieDetail(ctx, parsed.TMDBID)
		if err != nil {
			return Result{}, Error{Code: models.ErrScrapeRequestFailed, Message: err.Error()}
		}
		return c.movieResultFromDetail(ctx, detail)
	}
	search, err := c.searchMovies(ctx, parsed.SearchTitles, parsed.Year)
	if err != nil {
		return Result{}, Error{Code: models.ErrScrapeRequestFailed, Message: err.Error()}
	}
	if len(search.Results) == 0 {
		return Result{}, Error{Code: models.ErrScrapeEmptyResult, Message: "电影搜索没有返回结果"}
	}
	candidate, ok, ambiguous := bestMovieCandidate(search.Results, parsed)
	if !ok {
		return Result{}, Error{Code: models.ErrMatchNotFound, Message: "未找到匹配的电影"}
	}
	if ambiguous {
		return Result{}, Error{Code: models.ErrMatchNotUnique, Message: "电影匹配结果不唯一"}
	}
	detail, err := c.localizedMovieDetail(ctx, candidate.ID)
	if err != nil {
		return Result{}, Error{Code: models.ErrScrapeRequestFailed, Message: err.Error()}
	}
	return c.movieResultFromDetail(ctx, detail)
}

func (c *Client) movieResultFromDetail(ctx context.Context, detail tmdbMovieDetail) (Result, error) {
	movie := models.MovieMetadata{
		TMDBID:              detail.ID,
		IMDBID:              detail.IMDBID,
		Title:               detail.Title,
		Original:            detail.OriginalTitle,
		Year:                matcher.ReleaseYear(detail.ReleaseDate),
		ReleaseDate:         detail.ReleaseDate,
		Overview:            detail.Overview,
		Runtime:             detail.Runtime,
		Genres:              joinGenres(detail.Genres),
		GenreIDs:            joinGenreIDs(detail.Genres),
		OriginalLanguage:    detail.OriginalLanguage,
		ProductionCountries: joinCountries(detail.ProductionCountries),
		Keywords:            joinKeywords(detail.Keywords.Keywords),
		Rating:              certification(detail.ReleaseDates.Results),
		PosterPath:          detail.PosterPath,
		BackdropPath:        detail.BackdropPath,
	}
	result := Result{MediaType: models.MediaMovie, Movie: movie}
	if detail.BelongsToCollection != nil {
		collection, err := c.collectionDetail(ctx, detail.BelongsToCollection.ID)
		if err != nil {
			return Result{}, Error{Code: models.ErrCollectionFetchFailed, Message: err.Error()}
		}
		movie.CollectionID = collection.TMDBID
		result.MediaType = models.MediaCollectionMovie
		result.Movie = movie
		result.Collection = &collection
	}
	return result, nil
}

func (c *Client) scrapeTV(ctx context.Context, parsed parser.Result) (Result, error) {
	var showDetail tmdbTVDetail
	if parsed.TMDBID > 0 {
		var err error
		showDetail, err = c.localizedTVDetail(ctx, parsed.TMDBID)
		if err != nil {
			return Result{}, Error{Code: models.ErrScrapeRequestFailed, Message: err.Error()}
		}
	} else {
		search, err := c.searchTV(ctx, parsed.SearchTitles)
		if err != nil {
			return Result{}, Error{Code: models.ErrScrapeRequestFailed, Message: err.Error()}
		}
		if len(search.Results) == 0 {
			return Result{}, Error{Code: models.ErrScrapeEmptyResult, Message: "剧集搜索没有返回结果"}
		}
		candidate, ok, ambiguous := bestTVCandidate(search.Results, parsed)
		if !ok {
			return Result{}, Error{Code: models.ErrMatchNotFound, Message: "未找到匹配的剧集"}
		}
		if ambiguous {
			return Result{}, Error{Code: models.ErrMatchNotUnique, Message: "剧集匹配结果不唯一"}
		}
		var detailErr error
		showDetail, detailErr = c.localizedTVDetail(ctx, candidate.ID)
		if detailErr != nil {
			return Result{}, Error{Code: models.ErrScrapeRequestFailed, Message: detailErr.Error()}
		}
	}
	episodeDetail, err := c.localizedTVEpisodeDetail(ctx, showDetail.ID, parsed.Season, parsed.Episode)
	if err != nil {
		if !strings.Contains(err.Error(), "TMDB 返回状态码 404") {
			return Result{}, Error{Code: models.ErrTVEpisodeNotFound, Message: err.Error()}
		}
		episodeDetail = tmdbTVEpisodeDetail{Name: fmt.Sprintf("第%02d集", parsed.Episode)}
	}
	show := tvShowMetadataFromDetail(showDetail)
	episode := models.TVEpisodeMetadata{
		ID:         fmt.Sprintf("%d:S%02dE%02d", showDetail.ID, parsed.Season, parsed.Episode),
		ShowTMDBID: showDetail.ID,
		TMDBID:     episodeDetail.ID,
		Season:     parsed.Season,
		Episode:    parsed.Episode,
		Title:      episodeDetail.Name,
		AirDate:    episodeDetail.AirDate,
		Released:   releasedOnOrBeforeToday(episodeDetail.AirDate),
		Overview:   episodeDetail.Overview,
		Runtime:    episodeDetail.Runtime,
		StillPath:  episodeDetail.StillPath,
	}
	episodes, _ := c.localizedTVShowEpisodes(ctx, showDetail)
	episodes = ensureTVEpisode(episodes, episode)
	return Result{MediaType: models.MediaTVEpisode, TVShow: show, TVEpisode: episode, TVEpisodes: episodes}, nil
}

func (c *Client) searchMovies(ctx context.Context, titles []string, year int) (tmdbMovieSearch, error) {
	merged := tmdbMovieSearch{Results: make([]tmdbMovieSearchItem, 0)}
	seen := map[int]int{}
	var lastErr error
	for _, title := range searchQueries(titles) {
		for _, withYear := range yearModes(year) {
			for _, language := range searchLanguages {
				params := map[string]string{"query": title, "language": language}
				if withYear {
					params["year"] = strconv.Itoa(year)
				}
				var response tmdbMovieSearch
				err := c.get(ctx, "/search/movie", params, &response)
				if err != nil {
					lastErr = err
					continue
				}
				for index, item := range response.Results {
					item.QueryMatches = appendTitle(item.QueryMatches, title)
					item.BestRank = index + 1
					if index, ok := seen[item.ID]; ok {
						merged.Results[index].AltTitles = appendTitle(merged.Results[index].AltTitles, item.Title, item.OriginalTitle)
						merged.Results[index].QueryMatches = appendTitle(merged.Results[index].QueryMatches, title)
						if item.BestRank > 0 && (merged.Results[index].BestRank == 0 || item.BestRank < merged.Results[index].BestRank) {
							merged.Results[index].BestRank = item.BestRank
						}
						continue
					}
					seen[item.ID] = len(merged.Results)
					merged.Results = append(merged.Results, item)
				}
			}
		}
	}
	if len(merged.Results) == 0 && lastErr != nil {
		return merged, lastErr
	}
	return merged, nil
}

func (c *Client) searchTV(ctx context.Context, titles []string) (tmdbTVSearch, error) {
	merged := tmdbTVSearch{Results: make([]tmdbTVSearchItem, 0)}
	seen := map[int]int{}
	var lastErr error
	for _, title := range searchQueries(titles) {
		for _, language := range searchLanguages {
			var response tmdbTVSearch
			err := c.get(ctx, "/search/tv", map[string]string{"query": title, "language": language}, &response)
			if err != nil {
				lastErr = err
				continue
			}
			for index, item := range response.Results {
				item.QueryMatches = appendTitle(item.QueryMatches, title)
				item.BestRank = index + 1
				if index, ok := seen[item.ID]; ok {
					merged.Results[index].AltNames = appendTitle(merged.Results[index].AltNames, item.Name, item.OriginalName)
					merged.Results[index].QueryMatches = appendTitle(merged.Results[index].QueryMatches, title)
					if item.BestRank > 0 && (merged.Results[index].BestRank == 0 || item.BestRank < merged.Results[index].BestRank) {
						merged.Results[index].BestRank = item.BestRank
					}
					continue
				}
				seen[item.ID] = len(merged.Results)
				merged.Results = append(merged.Results, item)
			}
		}
	}
	if len(merged.Results) == 0 && lastErr != nil {
		return merged, lastErr
	}
	return merged, nil
}

func (c *Client) movieDetail(ctx context.Context, id int, language string) (tmdbMovieDetail, error) {
	var response tmdbMovieDetail
	return response, c.cached(ctx, fmt.Sprintf("cache:tmdb:movie:%d:%s", id, language), fmt.Sprintf("/movie/%d", id), map[string]string{"append_to_response": "release_dates,keywords", "language": language}, &response)
}

func (c *Client) localizedMovieDetail(ctx context.Context, id int) (tmdbMovieDetail, error) {
	zh, err := c.movieDetail(ctx, id, "zh-CN")
	if err != nil {
		return zh, err
	}
	sg, _ := c.movieDetail(ctx, id, "zh-SG")
	en, err := c.movieDetail(ctx, id, "en-US")
	if err != nil {
		en = sg
	}
	zh.Title = preferChinese(zh.Title, sg.Title, en.Title)
	zh.OriginalTitle = prefer(zh.OriginalTitle, en.OriginalTitle)
	zh.Overview = preferLocalizedText(zh.Overview, sg.Overview, en.Overview)
	if zh.BelongsToCollection == nil && en.BelongsToCollection != nil {
		zh.BelongsToCollection = en.BelongsToCollection
	} else if zh.BelongsToCollection == nil && sg.BelongsToCollection != nil {
		zh.BelongsToCollection = sg.BelongsToCollection
	}
	return zh, nil
}

func (c *Client) tvDetail(ctx context.Context, id int, language string) (tmdbTVDetail, error) {
	var response tmdbTVDetail
	return response, c.cached(ctx, fmt.Sprintf("cache:tmdb:tv:v2:%d:%s", id, language), fmt.Sprintf("/tv/%d", id), map[string]string{"append_to_response": "external_ids,keywords", "language": language}, &response)
}

func (c *Client) localizedTVDetail(ctx context.Context, id int) (tmdbTVDetail, error) {
	zh, err := c.tvDetail(ctx, id, "zh-CN")
	if err != nil {
		return zh, err
	}
	sg, _ := c.tvDetail(ctx, id, "zh-SG")
	en, err := c.tvDetail(ctx, id, "en-US")
	if err != nil {
		en = sg
	}
	zh.Name = preferChinese(zh.Name, sg.Name, en.Name)
	zh.OriginalName = prefer(zh.OriginalName, en.OriginalName)
	zh.Overview = preferLocalizedText(zh.Overview, sg.Overview, en.Overview)
	if len(zh.Seasons) == 0 {
		zh.Seasons = en.Seasons
		if len(zh.Seasons) == 0 {
			zh.Seasons = sg.Seasons
		}
	}
	if zh.ExternalIDs.TVDBID == 0 {
		zh.ExternalIDs.TVDBID = en.ExternalIDs.TVDBID
	}
	return zh, nil
}

func (c *Client) tvEpisodeDetail(ctx context.Context, showID, season, episode int, language string) (tmdbTVEpisodeDetail, error) {
	var response tmdbTVEpisodeDetail
	key := fmt.Sprintf("cache:tmdb:episode:%d:%d:%d:%s", showID, season, episode, language)
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d", showID, season, episode)
	return response, c.cached(ctx, key, path, map[string]string{"language": language}, &response)
}

func (c *Client) localizedTVEpisodeDetail(ctx context.Context, showID, season, episode int) (tmdbTVEpisodeDetail, error) {
	zh, err := c.tvEpisodeDetail(ctx, showID, season, episode, "zh-CN")
	if err != nil {
		return zh, err
	}
	sg, _ := c.tvEpisodeDetail(ctx, showID, season, episode, "zh-SG")
	en, err := c.tvEpisodeDetail(ctx, showID, season, episode, "en-US")
	if err != nil {
		en = sg
	}
	zh.Name = preferChinese(zh.Name, sg.Name, en.Name)
	zh.Overview = preferLocalizedText(zh.Overview, sg.Overview, en.Overview)
	return zh, nil
}

func (c *Client) tvSeasonDetail(ctx context.Context, showID, season int, language string) (tmdbTVSeasonDetail, error) {
	var response tmdbTVSeasonDetail
	key := fmt.Sprintf("cache:tmdb:season:%d:%d:%s", showID, season, language)
	path := fmt.Sprintf("/tv/%d/season/%d", showID, season)
	return response, c.cached(ctx, key, path, map[string]string{"language": language}, &response)
}

func (c *Client) localizedTVSeasonDetail(ctx context.Context, showID, season int) (tmdbTVSeasonDetail, error) {
	zh, err := c.tvSeasonDetail(ctx, showID, season, "zh-CN")
	if err != nil {
		return zh, err
	}
	sg, _ := c.tvSeasonDetail(ctx, showID, season, "zh-SG")
	en, err := c.tvSeasonDetail(ctx, showID, season, "en-US")
	if err != nil {
		en = sg
	}
	if len(zh.Episodes) == 0 {
		if len(sg.Episodes) > 0 {
			zh.Episodes = sg.Episodes
		} else {
			zh.Episodes = en.Episodes
		}
	}
	sgByEpisode := seasonEpisodeMap(sg.Episodes)
	enByEpisode := seasonEpisodeMap(en.Episodes)
	for index, episode := range zh.Episodes {
		sgEpisode := sgByEpisode[episode.EpisodeNumber]
		enEpisode := enByEpisode[episode.EpisodeNumber]
		zh.Episodes[index].Name = preferChinese(episode.Name, sgEpisode.Name, enEpisode.Name)
		zh.Episodes[index].Overview = preferLocalizedText(episode.Overview, sgEpisode.Overview, enEpisode.Overview)
		if zh.Episodes[index].Runtime == 0 {
			zh.Episodes[index].Runtime = enEpisode.Runtime
		}
		if zh.Episodes[index].StillPath == "" {
			zh.Episodes[index].StillPath = enEpisode.StillPath
		}
	}
	return zh, nil
}

func (c *Client) localizedTVShowEpisodes(ctx context.Context, detail tmdbTVDetail) ([]models.TVEpisodeMetadata, error) {
	episodes := make([]models.TVEpisodeMetadata, 0, detail.NumberOfEpisodes)
	for _, season := range detail.Seasons {
		if season.SeasonNumber <= 0 || season.EpisodeCount <= 0 {
			continue
		}
		seasonDetail, err := c.localizedTVSeasonDetail(ctx, detail.ID, season.SeasonNumber)
		if err != nil {
			return episodes, err
		}
		for _, item := range seasonDetail.Episodes {
			if item.EpisodeNumber <= 0 {
				continue
			}
			episodes = append(episodes, models.TVEpisodeMetadata{
				ID:         fmt.Sprintf("%d:S%02dE%02d", detail.ID, season.SeasonNumber, item.EpisodeNumber),
				ShowTMDBID: detail.ID,
				TMDBID:     item.ID,
				Season:     season.SeasonNumber,
				Episode:    item.EpisodeNumber,
				Title:      item.Name,
				AirDate:    item.AirDate,
				Released:   releasedOnOrBeforeToday(item.AirDate),
				Overview:   item.Overview,
				Runtime:    item.Runtime,
				StillPath:  item.StillPath,
			})
		}
	}
	return episodes, nil
}

func tvShowMetadataFromDetail(detail tmdbTVDetail) models.TVShowMetadata {
	return models.TVShowMetadata{
		TMDBID:           detail.ID,
		TVDBID:           detail.ExternalIDs.TVDBID,
		Name:             detail.Name,
		Original:         detail.OriginalName,
		Year:             matcher.ReleaseYear(detail.FirstAirDate),
		FirstAirDate:     detail.FirstAirDate,
		Overview:         detail.Overview,
		SeasonCount:      detail.NumberOfSeasons,
		EpisodeCount:     detail.NumberOfEpisodes,
		Genres:           joinGenres(detail.Genres),
		GenreIDs:         joinGenreIDs(detail.Genres),
		OriginalLanguage: detail.OriginalLanguage,
		OriginCountry:    strings.Join(detail.OriginCountry, ","),
		Keywords:         joinKeywords(detail.Keywords.Results),
		PosterPath:       detail.PosterPath,
		BackdropPath:     detail.BackdropPath,
	}
}

func ensureTVEpisode(episodes []models.TVEpisodeMetadata, target models.TVEpisodeMetadata) []models.TVEpisodeMetadata {
	if len(episodes) == 0 {
		return []models.TVEpisodeMetadata{target}
	}
	for index, episode := range episodes {
		if episode.ID == target.ID || (episode.ShowTMDBID == target.ShowTMDBID && episode.Season == target.Season && episode.Episode == target.Episode) {
			episodes[index] = mergeTVEpisode(episode, target)
			return episodes
		}
	}
	return append(episodes, target)
}

func mergeTVEpisode(value, fallback models.TVEpisodeMetadata) models.TVEpisodeMetadata {
	if value.ID == "" {
		value.ID = fallback.ID
	}
	if value.TMDBID == 0 {
		value.TMDBID = fallback.TMDBID
	}
	if strings.TrimSpace(value.Title) == "" {
		value.Title = fallback.Title
	}
	if strings.TrimSpace(value.AirDate) == "" {
		value.AirDate = fallback.AirDate
		value.Released = fallback.Released
	}
	if strings.TrimSpace(value.Overview) == "" {
		value.Overview = fallback.Overview
	}
	if value.Runtime == 0 {
		value.Runtime = fallback.Runtime
	}
	if strings.TrimSpace(value.StillPath) == "" {
		value.StillPath = fallback.StillPath
	}
	return value
}

func seasonEpisodeMap(episodes []tmdbTVEpisodeDetail) map[int]tmdbTVEpisodeDetail {
	byEpisode := make(map[int]tmdbTVEpisodeDetail, len(episodes))
	for _, episode := range episodes {
		byEpisode[episode.EpisodeNumber] = episode
	}
	return byEpisode
}

func (c *Client) collectionDetail(ctx context.Context, id int) (models.CollectionMetadata, error) {
	var detail tmdbCollectionDetail
	if err := c.cached(ctx, fmt.Sprintf("cache:tmdb:collection:%d:zh-CN", id), fmt.Sprintf("/collection/%d", id), map[string]string{"language": "zh-CN"}, &detail); err != nil {
		return models.CollectionMetadata{}, err
	}
	var sg tmdbCollectionDetail
	_ = c.cached(ctx, fmt.Sprintf("cache:tmdb:collection:%d:zh-SG", id), fmt.Sprintf("/collection/%d", id), map[string]string{"language": "zh-SG"}, &sg)
	var fallback tmdbCollectionDetail
	_ = c.cached(ctx, fmt.Sprintf("cache:tmdb:collection:%d:en-US", id), fmt.Sprintf("/collection/%d", id), map[string]string{"language": "en-US"}, &fallback)
	collection := models.CollectionMetadata{
		TMDBID:       detail.ID,
		Name:         preferChinese(detail.Name, sg.Name, fallback.Name),
		Overview:     preferLocalizedText(detail.Overview, sg.Overview, fallback.Overview),
		Status:       "incomplete",
		PosterPath:   detail.PosterPath,
		BackdropPath: detail.BackdropPath,
		Parts:        make([]models.CollectionMovieMetadata, 0, len(detail.Parts)),
	}
	for index, part := range detail.Parts {
		title := part.Title
		var sgTitle, enTitle string
		if index < len(sg.Parts) {
			sgTitle = sg.Parts[index].Title
		}
		if index < len(fallback.Parts) {
			enTitle = fallback.Parts[index].Title
		}
		title = preferChinese(title, sgTitle, enTitle)
		released := releasedOnOrBeforeToday(part.ReleaseDate)
		if released {
			collection.MovieCount++
		} else {
			collection.UnreleasedCount++
		}
		collection.Parts = append(collection.Parts, models.CollectionMovieMetadata{
			CollectionID: detail.ID,
			MovieTMDBID:  part.ID,
			Title:        title,
			ReleaseDate:  part.ReleaseDate,
			Released:     released,
			SortOrder:    index + 1,
		})
	}
	return collection, nil
}

func (c *Client) cached(ctx context.Context, key, path string, params map[string]string, target any) error {
	if c.redis != nil {
		if payload, err := c.redis.Get(ctx, key).Bytes(); err == nil && json.Unmarshal(payload, target) == nil {
			return nil
		}
	}
	if err := c.get(ctx, path, params, target); err != nil {
		return err
	}
	if c.redis != nil {
		if payload, err := json.Marshal(target); err == nil {
			_ = c.redis.Set(ctx, key, payload, 24*time.Hour).Err()
		}
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, params map[string]string, target any) error {
	apiKey, httpClient := c.current()
	cacheKey := tmdbGetCacheKey(path, params)
	if payload, ok := c.cachedPayload(ctx, cacheKey); ok {
		return json.Unmarshal(payload, target)
	}
	values := url.Values{"api_key": []string{apiKey}}
	for key, value := range params {
		values.Set(key, value)
	}
	endpoint := tmdbBase + path + "?" + values.Encode()
	attempts := tmdbMaxAttempts
	if c.proxyEnabled() {
		attempts = 1
	}
	if err := c.getWithClient(ctx, httpClient, endpoint, cacheKey, target, attempts); err == nil {
		return nil
	} else if !c.proxyEnabled() || !canFallbackDirect(err) {
		return err
	}
	return c.getWithClient(ctx, directHTTPClient(), endpoint, cacheKey, target, tmdbMaxAttempts)
}

func (c *Client) getWithClient(ctx context.Context, httpClient *http.Client, endpoint, cacheKey string, target any, attempts int) error {
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		response, err := httpClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			if !retryableRequestError(err) || attempt == attempts-1 {
				return err
			}
			if waitErr := sleepBeforeRetry(ctx, "", attempt); waitErr != nil {
				return waitErr
			}
			continue
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt == attempts-1 {
				return readErr
			}
			if waitErr := sleepBeforeRetry(ctx, "", attempt); waitErr != nil {
				return waitErr
			}
			continue
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			lastErr = tmdbStatusError{Code: response.StatusCode}
			if attempt == attempts-1 {
				return lastErr
			}
			if waitErr := sleepBeforeRetry(ctx, response.Header.Get("Retry-After"), attempt); waitErr != nil {
				return waitErr
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return tmdbStatusError{Code: response.StatusCode}
		}
		if err := json.Unmarshal(body, target); err != nil {
			return err
		}
		c.storePayload(ctx, cacheKey, body)
		return nil
	}
	return lastErr
}

func (c *Client) current() (string, *http.Client) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.http == nil {
		return c.apiKey, &http.Client{Timeout: tmdbHTTPTimeout}
	}
	return c.apiKey, c.http
}

func (c *Client) proxyEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.networkProxy != ""
}

func directHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Timeout: tmdbHTTPTimeout, Transport: transport}
}

func httpClient(proxy string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxy = strings.TrimSpace(proxy)
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
			return nil, fmt.Errorf("网络代理地址无效")
		}
		if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
			return nil, fmt.Errorf("网络代理协议必须是 http 或 https")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Timeout: tmdbHTTPTimeout, Transport: transport}, nil
}

func canFallbackDirect(err error) bool {
	var statusErr tmdbStatusError
	if errors.As(err, &statusErr) {
		return statusErr.Code == http.StatusTooManyRequests || statusErr.Code >= 500
	}
	return retryableRequestError(err)
}

func retryableRequestError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection reset") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "temporary failure") ||
		strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "tls handshake timeout")
}

func sleepBeforeRetry(ctx context.Context, retryAfter string, attempt int) error {
	wait := time.Duration(attempt+1) * 2 * time.Second
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds > 0 {
		wait = time.Duration(seconds) * time.Second
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

func tmdbGetCacheKey(path string, params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	sum := sha1.Sum([]byte(path + "?" + values.Encode()))
	return "cache:tmdb:get:" + hex.EncodeToString(sum[:])
}

func (c *Client) cachedPayload(ctx context.Context, key string) ([]byte, bool) {
	if c.redis != nil {
		if payload, err := c.redis.Get(ctx, key).Bytes(); err == nil {
			return payload, true
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	payload, ok := c.memoryCache[key]
	if !ok {
		return nil, false
	}
	copied := append([]byte(nil), payload...)
	return copied, true
}

func (c *Client) storePayload(ctx context.Context, key string, payload []byte) {
	if c.redis != nil {
		_ = c.redis.Set(ctx, key, payload, 24*time.Hour).Err()
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.memoryCache) > 2048 {
		c.memoryCache = map[string][]byte{}
	}
	c.memoryCache[key] = append([]byte(nil), payload...)
}

func prefer(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func preferChinese(zhCN, zhSG, fallback string) string {
	if containsHan(zhCN) {
		return strings.TrimSpace(zhCN)
	}
	if containsHan(zhSG) {
		return strings.TrimSpace(zhSG)
	}
	return prefer(fallback, prefer(zhCN, zhSG))
}

func preferLocalizedText(zhCN, zhSG, fallback string) string {
	if strings.TrimSpace(zhCN) != "" && zhCN != fallback {
		return strings.TrimSpace(zhCN)
	}
	if strings.TrimSpace(zhSG) != "" && zhSG != fallback {
		return strings.TrimSpace(zhSG)
	}
	return prefer(fallback, prefer(zhCN, zhSG))
}

func containsHan(value string) bool {
	for _, r := range value {
		if (r >= '\u4e00' && r <= '\u9fff') || (r >= '\u3400' && r <= '\u4dbf') {
			return true
		}
	}
	return false
}

func bestMovieCandidate(items []tmdbMovieSearchItem, parsed parser.Result) (tmdbMovieSearchItem, bool, bool) {
	targets := normalizedTargets(append([]string{parsed.Title}, parsed.SearchTitles...))
	bestScore := 0
	ambiguous := false
	var best tmdbMovieSearchItem
	for _, item := range items {
		score := movieScore(item, targets, parsed.Year)
		if score <= 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			best = item
			ambiguous = false
		} else if score == bestScore && item.ID != best.ID {
			if movieTieBreak(item, best) {
				best = item
				ambiguous = false
			} else if !movieTieBreak(best, item) {
				ambiguous = true
			}
		}
	}
	return best, bestScore >= 70, ambiguous
}

func bestTVCandidate(items []tmdbTVSearchItem, parsed parser.Result) (tmdbTVSearchItem, bool, bool) {
	targets := normalizedTargets(append([]string{parsed.ShowTitle}, parsed.SearchTitles...))
	bestScore := 0
	ambiguous := false
	var best tmdbTVSearchItem
	for _, item := range items {
		score := tvScore(item, targets, parsed.ShowYear)
		if score <= 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			best = item
			ambiguous = false
		} else if score == bestScore && item.ID != best.ID {
			if tvTieBreak(item, best) {
				best = item
				ambiguous = false
			} else if !tvTieBreak(best, item) {
				ambiguous = true
			}
		}
	}
	return best, bestScore >= 70, ambiguous
}

func movieTieBreak(a, b tmdbMovieSearchItem) bool {
	if a.BestRank > 0 && b.BestRank > 0 && a.BestRank != b.BestRank {
		return a.BestRank < b.BestRank
	}
	if a.VoteCount != b.VoteCount {
		return a.VoteCount > b.VoteCount
	}
	return a.Popularity > b.Popularity
}

func tvTieBreak(a, b tmdbTVSearchItem) bool {
	if a.BestRank > 0 && b.BestRank > 0 && a.BestRank != b.BestRank {
		return a.BestRank < b.BestRank
	}
	if a.VoteCount != b.VoteCount {
		return a.VoteCount > b.VoteCount
	}
	return a.Popularity > b.Popularity
}

func movieScore(item tmdbMovieSearchItem, targets []string, year int) int {
	score := titleScore(targets, item.Title, item.OriginalTitle, item.AltTitles)
	if score == 0 && queryMatchScore(targets, item.QueryMatches) > 0 {
		score = 70
	}
	if score == 0 {
		return 0
	}
	releaseYear := matcher.ReleaseYear(item.ReleaseDate)
	return score + yearScore(year, releaseYear)
}

func tvScore(item tmdbTVSearchItem, targets []string, year int) int {
	match := titleMatchScore(targets, item.Name, item.OriginalName, item.AltNames)
	if match.score == 0 && queryMatchScore(targets, item.QueryMatches) > 0 {
		match.score = 70
	}
	if match.score == 0 {
		return 0
	}
	releaseYear := matcher.ReleaseYear(item.FirstAirDate)
	score := match.score + yearScore(year, releaseYear)
	if match.exact {
		score += 60
	}
	return score
}

func titleScore(targets []string, primary, original string, aliases []string) int {
	return titleMatchScore(targets, primary, original, aliases).score
}

type titleMatch struct {
	score int
	exact bool
}

func titleMatchScore(targets []string, primary, original string, aliases []string) titleMatch {
	names := append([]string{primary, original}, aliases...)
	best := titleMatch{}
	for _, target := range targets {
		if target == "" {
			continue
		}
		for _, name := range names {
			current := matcher.NormalizeTitle(name)
			if current == "" {
				continue
			}
			switch {
			case current == target:
				return titleMatch{score: 100, exact: true}
			case looseTitleContains(current, target):
				if best.score < 75 {
					best.score = 75
				}
			}
		}
	}
	return best
}

func looseTitleContains(current, target string) bool {
	if current == target || current == "" || target == "" {
		return false
	}
	currentWords := strings.Fields(current)
	targetWords := strings.Fields(target)
	if len(currentWords) <= 1 || len(targetWords) <= 1 {
		return false
	}
	return strings.Contains(current, target) || strings.Contains(target, current)
}

func queryMatchScore(targets []string, queries []string) int {
	for _, query := range queries {
		normalized := matcher.NormalizeTitle(query)
		if normalized == "" {
			continue
		}
		for _, target := range targets {
			if normalized == target {
				return 70
			}
		}
	}
	return 0
}

func yearScore(target, candidate int) int {
	if target == 0 || candidate == 0 {
		return 0
	}
	delta := target - candidate
	if delta < 0 {
		delta = -delta
	}
	switch {
	case delta == 0:
		return 40
	case delta == 1:
		return 20
	default:
		return -25
	}
}

func normalizedTargets(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := matcher.NormalizeTitle(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func searchQueries(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) == maxSearchQueries {
			break
		}
	}
	return result
}

func yearModes(year int) []bool {
	if year == 0 {
		return []bool{false}
	}
	return []bool{true, false}
}

func appendTitle(values []string, candidates ...string) []string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		exists := false
		for _, existing := range values {
			if existing == candidate {
				exists = true
				break
			}
		}
		if !exists {
			values = append(values, candidate)
		}
	}
	return values
}

func joinGenres(genres []tmdbGenre) string {
	names := make([]string, 0, len(genres))
	for _, genre := range genres {
		if genre.Name != "" {
			names = append(names, genre.Name)
		}
	}
	return strings.Join(names, ",")
}

func joinGenreIDs(genres []tmdbGenre) string {
	values := make([]string, 0, len(genres))
	for _, genre := range genres {
		if genre.ID > 0 {
			values = append(values, strconv.Itoa(genre.ID))
		}
	}
	return strings.Join(values, ",")
}

func joinKeywords(keywords []tmdbKeyword) string {
	values := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		if keyword.Name != "" {
			values = append(values, keyword.Name)
		}
	}
	return strings.Join(values, ",")
}

func joinCountries(countries []tmdbCountry) string {
	values := make([]string, 0, len(countries))
	for _, country := range countries {
		if country.ISO31661 != "" {
			values = append(values, country.ISO31661)
		}
	}
	return strings.Join(values, ",")
}

func releasedOnOrBeforeToday(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return false
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return !date.After(today)
}

func certification(results []tmdbReleaseDateResult) string {
	for _, result := range results {
		if result.Country != "US" {
			continue
		}
		for _, release := range result.ReleaseDates {
			if release.Certification != "" {
				return release.Certification
			}
		}
	}
	return ""
}

type tmdbMovieSearch struct {
	Results []tmdbMovieSearchItem `json:"results"`
}

type tmdbMovieSearchItem struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	ReleaseDate   string  `json:"release_date"`
	Popularity    float64 `json:"popularity"`
	VoteCount     int     `json:"vote_count"`
	AltTitles     []string
	QueryMatches  []string
	BestRank      int
}

type tmdbTVSearch struct {
	Results []tmdbTVSearchItem `json:"results"`
}

type tmdbTVSearchItem struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name"`
	FirstAirDate string  `json:"first_air_date"`
	Popularity   float64 `json:"popularity"`
	VoteCount    int     `json:"vote_count"`
	AltNames     []string
	QueryMatches []string
	BestRank     int
}

type tmdbMovieDetail struct {
	ID                  int           `json:"id"`
	IMDBID              string        `json:"imdb_id"`
	Title               string        `json:"title"`
	OriginalTitle       string        `json:"original_title"`
	OriginalLanguage    string        `json:"original_language"`
	ReleaseDate         string        `json:"release_date"`
	Overview            string        `json:"overview"`
	Runtime             int           `json:"runtime"`
	Genres              []tmdbGenre   `json:"genres"`
	ProductionCountries []tmdbCountry `json:"production_countries"`
	PosterPath          string        `json:"poster_path"`
	BackdropPath        string        `json:"backdrop_path"`
	BelongsToCollection *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"belongs_to_collection"`
	ReleaseDates struct {
		Results []tmdbReleaseDateResult `json:"results"`
	} `json:"release_dates"`
	Keywords struct {
		Keywords []tmdbKeyword `json:"keywords"`
	} `json:"keywords"`
}

type tmdbReleaseDateResult struct {
	Country      string `json:"iso_3166_1"`
	ReleaseDates []struct {
		Certification string `json:"certification"`
	} `json:"release_dates"`
}

type tmdbGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbKeyword struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbCountry struct {
	ISO31661 string `json:"iso_3166_1"`
}

type tmdbTVDetail struct {
	ID               int         `json:"id"`
	Name             string      `json:"name"`
	OriginalName     string      `json:"original_name"`
	OriginalLanguage string      `json:"original_language"`
	OriginCountry    []string    `json:"origin_country"`
	FirstAirDate     string      `json:"first_air_date"`
	Overview         string      `json:"overview"`
	NumberOfSeasons  int         `json:"number_of_seasons"`
	NumberOfEpisodes int         `json:"number_of_episodes"`
	Genres           []tmdbGenre `json:"genres"`
	Seasons          []struct {
		SeasonNumber int `json:"season_number"`
		EpisodeCount int `json:"episode_count"`
	} `json:"seasons"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
	ExternalIDs  struct {
		TVDBID int `json:"tvdb_id"`
	} `json:"external_ids"`
	Keywords struct {
		Results []tmdbKeyword `json:"results"`
	} `json:"keywords"`
}

type tmdbTVSeasonDetail struct {
	ID           int                   `json:"id"`
	SeasonNumber int                   `json:"season_number"`
	Episodes     []tmdbTVEpisodeDetail `json:"episodes"`
}

type tmdbTVEpisodeDetail struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
	Overview      string `json:"overview"`
	Runtime       int    `json:"runtime"`
	StillPath     string `json:"still_path"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
}

type tmdbCollectionDetail struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
	Parts        []struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		ReleaseDate string `json:"release_date"`
	} `json:"parts"`
}
