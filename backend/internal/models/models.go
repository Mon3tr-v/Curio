package models

import "time"

const (
	StatusIncoming             = "incoming"
	StatusScanned              = "scanned"
	StatusParsed               = "parsed"
	StatusScraped              = "scraped"
	StatusMatched              = "matched"
	StatusCollectionChecked    = "collection_checked"
	StatusPlanned              = "planned"
	StatusMoved                = "moved"
	StatusDone                 = "done"
	StatusFailed               = "failed"
	StatusIncompleteCollection = "incomplete_collection"
)

const (
	BatchStatusQueued      = "queued"
	BatchStatusRunning     = "running"
	BatchStatusCancelling  = "cancelling"
	BatchStatusCancelled   = "cancelled"
	BatchStatusComplete    = "complete"
	BatchStatusFailed      = "failed"
	BatchStatusInterrupted = "interrupted"
)

const (
	BatchSourceLocal = "local"
	BatchSourceCloud = "cloud"
)

const (
	STRMProvider115 = "115"

	STRMStatusGenerated = "generated"
	STRMStatusStale     = "stale"
	STRMStatusDeleted   = "deleted"
	STRMStatusFailed    = "failed"

	STRMResolvePending  = "pending"
	STRMResolveResolved = "resolved"
	STRMResolveFailed   = "failed"
	STRMResolveStale    = "stale"
)

const (
	P115SyncTriggerManualExport  = "manual_export"
	P115SyncTriggerManualSync    = "manual_sync"
	P115SyncTriggerManualCleanup = "manual_cleanup"
	P115SyncTriggerCron          = "cron"

	P115SyncStatusRunning = "running"
	P115SyncStatusSuccess = "success"
	P115SyncStatusPartial = "partial"
	P115SyncStatusFailed  = "failed"
)

const (
	TemplateMovie                = "movie"
	TemplateTVEpisode            = "tv_episode"
	TemplateCollectionMovie      = "collection_movie"
	TemplateIncompleteCollection = "incomplete_collection_movie"
)

const (
	MediaMovie           = "movie"
	MediaTVEpisode       = "tv_episode"
	MediaCollectionMovie = "collection_movie"
)

const (
	ErrUnsupportedExtension             = "UNSUPPORTED_EXTENSION"
	ErrFileTooSmall                     = "FILE_TOO_SMALL"
	ErrFileNotReadable                  = "FILE_NOT_READABLE"
	ErrFileHashFailed                   = "FILE_HASH_FAILED"
	ErrParseTitleEmpty                  = "PARSE_TITLE_EMPTY"
	ErrParseYearEmpty                   = "PARSE_YEAR_EMPTY"
	ErrParseSeasonEmpty                 = "PARSE_SEASON_EMPTY"
	ErrParseEpisodeEmpty                = "PARSE_EPISODE_EMPTY"
	ErrScrapeEmptyResult                = "SCRAPE_EMPTY_RESULT"
	ErrScrapeRequestFailed              = "SCRAPE_REQUEST_FAILED"
	ErrMatchNotFound                    = "MATCH_NOT_FOUND"
	ErrMatchNotUnique                   = "MATCH_NOT_UNIQUE"
	ErrTVEpisodeNotFound                = "TV_EPISODE_NOT_FOUND"
	ErrCollectionFetchFailed            = "COLLECTION_FETCH_FAILED"
	ErrCollectionCheckFailed            = "COLLECTION_CHECK_FAILED"
	ErrTemplateNotFound                 = "TEMPLATE_NOT_FOUND"
	ErrTemplateFieldInvalid             = "TEMPLATE_FIELD_INVALID"
	ErrTemplatePathEscape               = "TEMPLATE_PATH_ESCAPE"
	ErrTargetPathExists                 = "TARGET_PATH_EXISTS"
	ErrTargetDirCreateFailed            = "TARGET_DIR_CREATE_FAILED"
	ErrMoveToStagingFailed              = "MOVE_TO_STAGING_FAILED"
	ErrMoveToFailedDirFailed            = "MOVE_TO_FAILED_DIR_FAILED"
	ErrMoveToIncompleteCollectionFailed = "MOVE_TO_INCOMPLETE_COLLECTION_FAILED"
	ErrCollectionCompleteMoveFailed     = "COLLECTION_COMPLETE_MOVE_FAILED"
	ErrCloudDriveRequestFailed          = "CLOUDDRIVE_REQUEST_FAILED"
	ErrDatabaseWriteFailed              = "DATABASE_WRITE_FAILED"
	ErrSubtitleMoveFailed               = "SUBTITLE_MOVE_FAILED"
	ErrMediaProbeFailed                 = "MEDIA_PROBE_FAILED"
)

type DirectoryConfig struct {
	IncomingPath              string `json:"incoming_path"`
	StagingPath               string `json:"staging_path"`
	FailedPath                string `json:"failed_path"`
	IncompleteCollectionsPath string `json:"incomplete_collections_path"`
}

type SystemSettings struct {
	TMDBAPIKey               string    `json:"tmdb_api_key"`
	NetworkProxy             string    `json:"network_proxy"`
	ClassificationYAML       string    `json:"classification_yaml"`
	CloudDriveAddress        string    `json:"clouddrive_address"`
	CloudDriveUsername       string    `json:"clouddrive_username"`
	CloudDrivePassword       string    `json:"clouddrive_password"`
	CloudDriveToken          string    `json:"clouddrive_token"`
	CloudDriveRootPath       string    `json:"clouddrive_root_path"`
	CloudDriveStagingPath    string    `json:"clouddrive_staging_path"`
	CloudDriveFailedPath     string    `json:"clouddrive_failed_path"`
	CloudDriveIncompletePath string    `json:"clouddrive_incomplete_path"`
	UpdatedAt                time.Time `json:"updated_at"`
}

type CloudDriveSettings struct {
	Address                   string    `json:"address"`
	Username                  string    `json:"username"`
	Password                  string    `json:"password"`
	Token                     string    `json:"token"`
	RootPath                  string    `json:"root_path"`
	StagingPath               string    `json:"staging_path"`
	FailedPath                string    `json:"failed_path"`
	IncompleteCollectionsPath string    `json:"incomplete_collections_path"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type P115Settings struct {
	Enabled              bool      `json:"enabled"`
	AuthMode             string    `json:"-"`
	AppID                string    `json:"app_id"`
	AppSecret            string    `json:"app_secret"`
	AccessToken          string    `json:"-"`
	RefreshToken         string    `json:"-"`
	Cookies              string    `json:"cookies"`
	CookieLoginApp       string    `json:"cookie_login_app"`
	STRMOutputPath       string    `json:"strm_output_path"`
	PublicBaseURL        string    `json:"public_base_url"`
	DirectURLTTLSeconds  int       `json:"-"`
	UserAgentMode        string    `json:"-"`
	FixedUserAgent       string    `json:"-"`
	LibrariesYAML        string    `json:"libraries_yaml"`
	DeleteMissingSTRM    bool      `json:"delete_missing_strm"`
	StaleBeforeDelete    bool      `json:"stale_before_delete"`
	KeepDeletedDays      int       `json:"-"`
	RefreshEmbyAfterSync bool      `json:"refresh_emby_after_sync"`
	SyncCronEnabled      bool      `json:"sync_cron_enabled"`
	SyncIntervalMinutes  int       `json:"sync_interval_minutes"`
	EmbyUpstreamURL      string    `json:"emby_upstream_url"`
	EmbyPublicURL        string    `json:"emby_public_url"`
	EmbyProxyPort        int       `json:"emby_proxy_port"`
	EmbyProxyBasePath    string    `json:"emby_proxy_base_path"`
	EmbyAPIKey           string    `json:"emby_api_key"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type P115Status struct {
	Ready     bool   `json:"ready"`
	Message   string `json:"message"`
	UserName  string `json:"user_name"`
	CanExport bool   `json:"can_export"`
	CanPlay   bool   `json:"can_play"`
}

type P115QRCodeSession struct {
	UID       string    `json:"uid"`
	QRCodeURL string    `json:"qrcode_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type P115QRCodeStatus struct {
	UID     string `json:"uid"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type P115OAuthStart struct {
	AuthorizeURL string `json:"authorize_url"`
	RedirectURI  string `json:"redirect_uri"`
	State        string `json:"state"`
}

type P115AuthResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type STRMLink struct {
	ID             string     `json:"id"`
	Provider       string     `json:"provider"`
	LibraryCID     string     `json:"library_cid"`
	LibraryName    string     `json:"library_name"`
	LibraryType    string     `json:"library_type"`
	RelativePath   string     `json:"relative_path"`
	RemotePath     string     `json:"remote_path"`
	RemoteFileID   string     `json:"remote_file_id"`
	PickCode       string     `json:"pickcode"`
	SHA1           string     `json:"sha1"`
	Size           int64      `json:"size"`
	STRMPath       string     `json:"strm_path"`
	PlayPath       string     `json:"play_path"`
	SourceTreeHash string     `json:"source_tree_hash"`
	TreeVersion    string     `json:"tree_version"`
	ResolveStatus  string     `json:"resolve_status"`
	Status         string     `json:"status"`
	ErrorCode      string     `json:"error_code"`
	ErrorMessage   string     `json:"error_message"`
	GeneratedAt    time.Time  `json:"generated_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type STRMSyncResult struct {
	TreeVersion string `json:"tree_version"`
	Mode        string `json:"mode"`
	Exported    int    `json:"exported"`
	Generated   int    `json:"generated"`
	Restored    int    `json:"restored"`
	Updated     int    `json:"updated"`
	Deleted     int    `json:"deleted"`
	Skipped     int    `json:"skipped"`
	Failed      int    `json:"failed"`
}

type P115SyncRun struct {
	ID           string     `json:"id"`
	Trigger      string     `json:"trigger"`
	Status       string     `json:"status"`
	Mode         string     `json:"mode"`
	TreeVersion  string     `json:"tree_version"`
	Exported     int        `json:"exported"`
	Generated    int        `json:"generated"`
	Restored     int        `json:"restored"`
	Updated      int        `json:"updated"`
	Deleted      int        `json:"deleted"`
	Skipped      int        `json:"skipped"`
	Failed       int        `json:"failed"`
	ErrorMessage string     `json:"error_message"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	DurationMS   int64      `json:"duration_ms"`
}

type P115TreeSnapshotItem struct {
	LibraryCID     string `json:"library_cid"`
	TreeVersion    string `json:"tree_version"`
	RelativePath   string `json:"relative_path"`
	Name           string `json:"name"`
	RemoteFileID   string `json:"remote_file_id"`
	ParentFileID   string `json:"parent_file_id"`
	PickCode       string `json:"pickcode"`
	SHA1           string `json:"sha1"`
	Size           int64  `json:"size"`
	Extension      string `json:"extension"`
	Depth          int    `json:"depth"`
	IsDirectory    bool   `json:"is_directory"`
	IsMedia        bool   `json:"is_media"`
	SourceTreeHash string `json:"source_tree_hash"`
}

type P115Node struct {
	LibraryCID     string    `json:"library_cid"`
	TreeVersion    string    `json:"tree_version"`
	RemoteFileID   string    `json:"remote_file_id"`
	ParentFileID   string    `json:"parent_file_id"`
	RelativePath   string    `json:"relative_path"`
	Name           string    `json:"name"`
	PickCode       string    `json:"pickcode"`
	SHA1           string    `json:"sha1"`
	Size           int64     `json:"size"`
	IsDirectory    bool      `json:"is_directory"`
	IsMedia        bool      `json:"is_media"`
	IsAlive        bool      `json:"is_alive"`
	SourceTreeHash string    `json:"source_tree_hash"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type P115EventCursor struct {
	LibraryCID     string    `json:"library_cid"`
	LastEventID    int64     `json:"last_event_id"`
	LastEventTime  int64     `json:"last_event_time"`
	LastSyncStatus string    `json:"last_sync_status"`
	LastSyncError  string    `json:"last_sync_error"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type EmbySTRMItem struct {
	ID           string    `json:"id"`
	EmbyServerID string    `json:"emby_server_id"`
	EmbyItemID   string    `json:"emby_item_id"`
	STRMLinkID   string    `json:"strm_link_id"`
	STRMPath     string    `json:"strm_path"`
	Status       string    `json:"status"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type NamingTemplate struct {
	TemplateType string    `json:"template_type"`
	Name         string    `json:"name"`
	Template     string    `json:"template"`
	Enabled      bool      `json:"enabled"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Batch struct {
	ID                   string     `json:"batch_id"`
	Source               string     `json:"source"`
	Status               string     `json:"status"`
	Total                int        `json:"total"`
	Done                 int        `json:"done"`
	Failed               int        `json:"failed"`
	IncompleteCollection int        `json:"incomplete_collection"`
	StartedAt            time.Time  `json:"started_at"`
	EndedAt              *time.Time `json:"ended_at,omitempty"`
}

type MediaStats struct {
	Total                 int `json:"total"`
	Done                  int `json:"done"`
	Failed                int `json:"failed"`
	IncompleteCollection  int `json:"incomplete_collection"`
	MissingTVSeasonCount  int `json:"missing_tv_season_count"`
	MissingTVEpisodeCount int `json:"missing_tv_episode_count"`
}

type MediaFile struct {
	ID            string    `json:"file_id"`
	BatchID       string    `json:"batch_id"`
	OriginalPath  string    `json:"original_path"`
	CurrentPath   string    `json:"current_path"`
	FinalPath     string    `json:"final_path"`
	OriginalName  string    `json:"original_file_name"`
	FinalName     string    `json:"final_file_name"`
	Extension     string    `json:"extension"`
	FileSize      int64     `json:"file_size"`
	FileHash      string    `json:"file_hash"`
	HashType      string    `json:"hash_type"`
	MediaType     string    `json:"media_type"`
	ProcessStatus string    `json:"process_status"`
	MatchStatus   string    `json:"match_status"`
	ParseTitle    string    `json:"parse_title"`
	ParseYear     int       `json:"parse_year"`
	Season        int       `json:"season"`
	Episode       int       `json:"episode"`
	Resolution    string    `json:"resolution"`
	Source        string    `json:"source"`
	VideoCodec    string    `json:"video_codec"`
	AudioCodec    string    `json:"audio_codec"`
	AudioChannels string    `json:"audio_channels"`
	HDRFormat     string    `json:"hdr_format"`
	PlannedTarget string    `json:"planned_target"`
	MoveAttempts  int       `json:"move_attempts"`
	LastVerified  string    `json:"last_verified_path"`
	ErrorCode     string    `json:"error_code"`
	ErrorMessage  string    `json:"error_message"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type MediaFilePage struct {
	Items  []MediaFile `json:"items"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

type MediaTechnicalInfo struct {
	Resolution    string
	VideoCodec    string
	AudioCodec    string
	AudioChannels string
	HDRFormat     string
}

type MovieMetadata struct {
	TMDBID              int    `json:"tmdb_id"`
	IMDBID              string `json:"imdb_id"`
	Title               string `json:"title"`
	Original            string `json:"original_title"`
	Year                int    `json:"year"`
	ReleaseDate         string `json:"release_date"`
	Overview            string `json:"overview"`
	Runtime             int    `json:"runtime"`
	Genres              string `json:"genres"`
	GenreIDs            string `json:"genre_ids"`
	OriginalLanguage    string `json:"original_language"`
	ProductionCountries string `json:"production_countries"`
	Keywords            string `json:"keywords"`
	Rating              string `json:"rating"`
	PosterPath          string `json:"poster_path"`
	BackdropPath        string `json:"backdrop_path"`
	CollectionID        int    `json:"collection_id"`
}

type TVShowMetadata struct {
	TMDBID           int    `json:"tmdb_id"`
	TVDBID           int    `json:"tvdb_id"`
	Name             string `json:"name"`
	Original         string `json:"original_name"`
	Year             int    `json:"year"`
	FirstAirDate     string `json:"first_air_date"`
	Overview         string `json:"overview"`
	SeasonCount      int    `json:"season_count"`
	EpisodeCount     int    `json:"episode_count"`
	Genres           string `json:"genres"`
	GenreIDs         string `json:"genre_ids"`
	OriginalLanguage string `json:"original_language"`
	OriginCountry    string `json:"origin_country"`
	Keywords         string `json:"keywords"`
	PosterPath       string `json:"poster_path"`
	BackdropPath     string `json:"backdrop_path"`
}

type TVEpisodeMetadata struct {
	ID            string `json:"id"`
	ShowTMDBID    int    `json:"show_tmdb_id"`
	TMDBID        int    `json:"tmdb_id"`
	Season        int    `json:"season"`
	Episode       int    `json:"episode"`
	Title         string `json:"title"`
	AirDate       string `json:"air_date"`
	Released      bool   `json:"released"`
	Overview      string `json:"overview"`
	Runtime       int    `json:"runtime"`
	StillPath     string `json:"still_path"`
	Local         bool   `json:"local"`
	FileID        string `json:"file_id"`
	FilePath      string `json:"file_path"`
	ProcessStatus string `json:"process_status"`
}

type TVShowStatus struct {
	TMDBID                 int              `json:"tmdb_id"`
	Name                   string           `json:"name"`
	Original               string           `json:"original_name"`
	Year                   int              `json:"year"`
	FirstAirDate           string           `json:"first_air_date"`
	Overview               string           `json:"overview"`
	SeasonCount            int              `json:"season_count"`
	EpisodeCount           int              `json:"episode_count"`
	ReleasedEpisodeCount   int              `json:"released_episode_count"`
	UnreleasedEpisodeCount int              `json:"unreleased_episode_count"`
	LocalEpisodeCount      int              `json:"local_episode_count"`
	MissingEpisodeCount    int              `json:"missing_episode_count"`
	MissingSeasonCount     int              `json:"missing_season_count"`
	Status                 string           `json:"status"`
	PosterPath             string           `json:"poster_path"`
	BackdropPath           string           `json:"backdrop_path"`
	Seasons                []TVSeasonStatus `json:"seasons,omitempty"`
}

type TVShowPage struct {
	Items  []TVShowStatus `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type TVSeasonStatus struct {
	Season                 int                 `json:"season"`
	EpisodeCount           int                 `json:"episode_count"`
	ReleasedEpisodeCount   int                 `json:"released_episode_count"`
	UnreleasedEpisodeCount int                 `json:"unreleased_episode_count"`
	LocalEpisodeCount      int                 `json:"local_episode_count"`
	MissingEpisodeCount    int                 `json:"missing_episode_count"`
	Status                 string              `json:"status"`
	Episodes               []TVEpisodeMetadata `json:"episodes,omitempty"`
}

type CollectionMetadata struct {
	TMDBID          int                       `json:"tmdb_id"`
	Name            string                    `json:"name"`
	Overview        string                    `json:"overview"`
	MovieCount      int                       `json:"movie_count"`
	UnreleasedCount int                       `json:"unreleased_count"`
	LocalCount      int                       `json:"local_count"`
	Status          string                    `json:"status"`
	PosterPath      string                    `json:"poster_path"`
	BackdropPath    string                    `json:"backdrop_path"`
	Parts           []CollectionMovieMetadata `json:"parts,omitempty"`
}

type CollectionPage struct {
	Items  []CollectionMetadata `json:"items"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

type CollectionMovieMetadata struct {
	CollectionID  int    `json:"collection_id"`
	MovieTMDBID   int    `json:"movie_tmdb_id"`
	Title         string `json:"title"`
	ReleaseDate   string `json:"release_date"`
	Released      bool   `json:"released"`
	SortOrder     int    `json:"sort_order"`
	Local         bool   `json:"local"`
	FileID        string `json:"file_id"`
	FilePath      string `json:"file_path"`
	ProcessStatus string `json:"process_status"`
}
