package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"curio/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS system_directories (
			id INT PRIMARY KEY,
			incoming_path TEXT NOT NULL,
			staging_path TEXT NOT NULL,
			failed_path TEXT NOT NULL,
			incomplete_collections_path TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS system_settings (
			id INT PRIMARY KEY,
			tmdb_api_key TEXT NOT NULL DEFAULT '',
			network_proxy TEXT NOT NULL DEFAULT '',
			classification_yaml TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS classification_yaml TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_address TEXT NOT NULL DEFAULT 'http://localhost:19798'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_password TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_token TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_root_path TEXT NOT NULL DEFAULT '/'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_staging_path TEXT NOT NULL DEFAULT '/Curio/staging'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_failed_path TEXT NOT NULL DEFAULT '/Curio/failed'`,
		`ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS clouddrive_incomplete_path TEXT NOT NULL DEFAULT '/Curio/incomplete_collections'`,
		`CREATE TABLE IF NOT EXISTS p115_settings (
			id INT PRIMARY KEY,
			enabled BOOLEAN NOT NULL DEFAULT true,
			auth_mode TEXT NOT NULL DEFAULT 'cookies',
			app_id TEXT NOT NULL DEFAULT '',
			app_secret TEXT NOT NULL DEFAULT '',
			access_token TEXT NOT NULL DEFAULT '',
			refresh_token TEXT NOT NULL DEFAULT '',
			cookies TEXT NOT NULL DEFAULT '',
			cookie_login_app TEXT NOT NULL DEFAULT 'wechatmini',
			strm_output_path TEXT NOT NULL DEFAULT '/data/Curio/strm',
			public_base_url TEXT NOT NULL DEFAULT '',
			direct_url_ttl_seconds INT NOT NULL DEFAULT 300,
			user_agent_mode TEXT NOT NULL DEFAULT 'inherit',
			fixed_user_agent TEXT NOT NULL DEFAULT '',
			libraries_yaml TEXT NOT NULL DEFAULT '',
			delete_missing_strm BOOLEAN NOT NULL DEFAULT true,
			stale_before_delete BOOLEAN NOT NULL DEFAULT false,
			keep_deleted_days INT NOT NULL DEFAULT 7,
			refresh_emby_after_sync BOOLEAN NOT NULL DEFAULT false,
			sync_cron_enabled BOOLEAN NOT NULL DEFAULT false,
			sync_interval_minutes INT NOT NULL DEFAULT 60,
			emby_upstream_url TEXT NOT NULL DEFAULT '',
			emby_public_url TEXT NOT NULL DEFAULT '',
			emby_proxy_port INT NOT NULL DEFAULT 8097,
			emby_proxy_base_path TEXT NOT NULL DEFAULT '/emby',
			emby_api_key TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS cookie_login_app TEXT NOT NULL DEFAULT 'wechatmini'`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS emby_proxy_port INT NOT NULL DEFAULT 8097`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS sync_cron_enabled BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE p115_settings ADD COLUMN IF NOT EXISTS sync_interval_minutes INT NOT NULL DEFAULT 60`,
		`CREATE TABLE IF NOT EXISTS strm_links (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			library_cid TEXT NOT NULL,
			library_name TEXT NOT NULL DEFAULT '',
			library_type TEXT NOT NULL DEFAULT '',
			relative_path TEXT NOT NULL,
			remote_path TEXT NOT NULL DEFAULT '',
			remote_file_id TEXT NOT NULL DEFAULT '',
			pickcode TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			size BIGINT NOT NULL DEFAULT 0,
			strm_path TEXT NOT NULL,
			play_path TEXT NOT NULL DEFAULT '',
			source_tree_hash TEXT NOT NULL DEFAULT '',
			tree_version TEXT NOT NULL DEFAULT '',
			resolve_status TEXT NOT NULL DEFAULT 'pending',
			status TEXT NOT NULL DEFAULT 'generated',
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			resolved_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(provider, library_cid, relative_path)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_strm_links_path ON strm_links(strm_path)`,
		`CREATE INDEX IF NOT EXISTS idx_strm_links_library ON strm_links(provider, library_cid, status)`,
		`CREATE TABLE IF NOT EXISTS p115_tree_snapshots (
			library_cid TEXT NOT NULL,
			content_key TEXT NOT NULL,
			tree_version TEXT NOT NULL,
			relative_path TEXT NOT NULL,
			name TEXT NOT NULL,
			remote_file_id TEXT NOT NULL DEFAULT '',
			parent_file_id TEXT NOT NULL DEFAULT '',
			pickcode TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			size BIGINT NOT NULL DEFAULT 0,
			extension TEXT NOT NULL DEFAULT '',
			depth INT NOT NULL DEFAULT 0,
			is_directory BOOLEAN NOT NULL DEFAULT false,
			is_media BOOLEAN NOT NULL DEFAULT false,
			source_tree_hash TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY(library_cid, content_key)
		)`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS remote_file_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS parent_file_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS pickcode TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS sha1 TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS size BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE p115_tree_snapshots ADD COLUMN IF NOT EXISTS is_directory BOOLEAN NOT NULL DEFAULT false`,
		`CREATE INDEX IF NOT EXISTS idx_p115_tree_snapshots_version ON p115_tree_snapshots(library_cid, tree_version)`,
		`CREATE TABLE IF NOT EXISTS p115_nodes (
			library_cid TEXT NOT NULL,
			remote_file_id TEXT NOT NULL,
			parent_file_id TEXT NOT NULL DEFAULT '',
			tree_version TEXT NOT NULL DEFAULT '',
			relative_path TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			pickcode TEXT NOT NULL DEFAULT '',
			sha1 TEXT NOT NULL DEFAULT '',
			size BIGINT NOT NULL DEFAULT 0,
			is_directory BOOLEAN NOT NULL DEFAULT false,
			is_media BOOLEAN NOT NULL DEFAULT false,
			is_alive BOOLEAN NOT NULL DEFAULT true,
			source_tree_hash TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY(library_cid, remote_file_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_nodes_library_alive ON p115_nodes(library_cid, is_alive)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_nodes_library_parent ON p115_nodes(library_cid, parent_file_id)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_nodes_library_path ON p115_nodes(library_cid, relative_path)`,
		`CREATE TABLE IF NOT EXISTS p115_event_cursors (
			library_cid TEXT PRIMARY KEY,
			last_event_id BIGINT NOT NULL DEFAULT 0,
			last_event_time BIGINT NOT NULL DEFAULT 0,
			last_sync_status TEXT NOT NULL DEFAULT '',
			last_sync_error TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS p115_sync_runs (
			id TEXT PRIMARY KEY,
			trigger TEXT NOT NULL DEFAULT 'manual_sync',
			status TEXT NOT NULL DEFAULT 'running',
			mode TEXT NOT NULL DEFAULT '',
			tree_version TEXT NOT NULL DEFAULT '',
			exported INT NOT NULL DEFAULT 0,
			generated INT NOT NULL DEFAULT 0,
			restored INT NOT NULL DEFAULT 0,
			updated_count INT NOT NULL DEFAULT 0,
			deleted_count INT NOT NULL DEFAULT 0,
			skipped INT NOT NULL DEFAULT 0,
			failed INT NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			ended_at TIMESTAMPTZ,
			duration_ms BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_sync_runs_started ON p115_sync_runs(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_p115_sync_runs_trigger ON p115_sync_runs(trigger, started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS emby_strm_items (
			id TEXT PRIMARY KEY,
			emby_server_id TEXT NOT NULL DEFAULT 'default',
			emby_item_id TEXT NOT NULL,
			strm_link_id TEXT NOT NULL REFERENCES strm_links(id) ON DELETE CASCADE,
			strm_path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(emby_server_id, emby_item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS naming_templates (
			template_type TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			template TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT true,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`UPDATE naming_templates SET template='movies/{category}/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}', updated_at=now()
			WHERE template_type='movie' AND template='movies/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}'`,
		`UPDATE naming_templates SET template='tv/{category}/{show_title} ({show_year})/Season {season_2}/{show_title} - S{season_2}E{episode_2} - {episode_title} - {resolution} {source} {video_codec}.{extension}', updated_at=now()
			WHERE template_type='tv_episode' AND template='tv/{show_title} ({show_year})/Season {season_2}/{show_title} - S{season_2}E{episode_2} - {episode_title} - {resolution} {source} {video_codec}.{extension}'`,
		`UPDATE naming_templates SET template='collections/{category}/{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}', updated_at=now()
			WHERE template_type='collection_movie' AND template='collections/{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}'`,
		`UPDATE naming_templates SET template='{category}/{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}', updated_at=now()
			WHERE template_type='incomplete_collection_movie' AND template='{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}'`,
		`UPDATE naming_templates SET template=CASE
				WHEN template LIKE 'movies/%' THEN 'movies/{category}/' || substring(template from 8)
				ELSE '{category}/' || template
			END, updated_at=now()
			WHERE template_type='movie' AND template NOT LIKE '%{category}%'`,
		`UPDATE naming_templates SET template=CASE
				WHEN template LIKE 'tv/%' THEN 'tv/{category}/' || substring(template from 4)
				ELSE '{category}/' || template
			END, updated_at=now()
			WHERE template_type='tv_episode' AND template NOT LIKE '%{category}%'`,
		`UPDATE naming_templates SET template=CASE
				WHEN template LIKE 'collections/%' THEN 'collections/{category}/' || substring(template from 13)
				ELSE '{category}/' || template
			END, updated_at=now()
			WHERE template_type='collection_movie' AND template NOT LIKE '%{category}%'`,
		`UPDATE naming_templates SET template='{category}/' || template, updated_at=now()
			WHERE template_type='incomplete_collection_movie' AND template NOT LIKE '%{category}%'`,
		`CREATE TABLE IF NOT EXISTS batches (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL DEFAULT 'local',
			status TEXT NOT NULL,
			total INT NOT NULL DEFAULT 0,
			done INT NOT NULL DEFAULT 0,
			failed INT NOT NULL DEFAULT 0,
			incomplete_collection INT NOT NULL DEFAULT 0,
			started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			ended_at TIMESTAMPTZ
		)`,
		`ALTER TABLE batches ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'local'`,
		`CREATE INDEX IF NOT EXISTS idx_batches_active ON batches(status, ended_at) WHERE ended_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS media_files (
			id TEXT PRIMARY KEY,
			batch_id TEXT NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
			original_path TEXT NOT NULL,
			current_path TEXT NOT NULL,
			final_path TEXT NOT NULL DEFAULT '',
			original_file_name TEXT NOT NULL,
			final_file_name TEXT NOT NULL DEFAULT '',
			extension TEXT NOT NULL,
			file_size BIGINT NOT NULL,
			file_hash TEXT NOT NULL DEFAULT '',
			hash_type TEXT NOT NULL DEFAULT '',
			media_type TEXT NOT NULL DEFAULT '',
			process_status TEXT NOT NULL,
			match_status TEXT NOT NULL DEFAULT '',
			parse_title TEXT NOT NULL DEFAULT '',
			parse_year INT,
			season INT,
			episode INT,
			resolution TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			video_codec TEXT NOT NULL DEFAULT '',
			audio_codec TEXT NOT NULL DEFAULT '',
			audio_channels TEXT NOT NULL DEFAULT '',
			hdr_format TEXT NOT NULL DEFAULT '',
			planned_target TEXT NOT NULL DEFAULT '',
			move_attempts INT NOT NULL DEFAULT 0,
			last_verified_path TEXT NOT NULL DEFAULT '',
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS planned_target TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS move_attempts INT NOT NULL DEFAULT 0`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS last_verified_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS audio_codec TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS audio_channels TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE media_files ADD COLUMN IF NOT EXISTS hdr_format TEXT NOT NULL DEFAULT ''`,
		`UPDATE media_files SET video_codec='AVC' WHERE upper(replace(replace(video_codec, '.', ''), '-', '')) IN ('H264', 'X264', 'AVC')`,
		`UPDATE media_files SET video_codec='HEVC' WHERE upper(replace(replace(video_codec, '.', ''), '-', '')) IN ('H265', 'X265', 'HEVC')`,
		`UPDATE media_files SET video_codec='VVC' WHERE upper(replace(replace(video_codec, '.', ''), '-', '')) IN ('H266', 'X266', 'VVC')`,
		`UPDATE media_files SET video_codec='MPEG-2' WHERE upper(replace(video_codec, '-', ''))='MPEG2'`,
		`UPDATE media_files SET video_codec='MPEG-4' WHERE upper(replace(video_codec, '-', ''))='MPEG4'`,
		`UPDATE media_files SET video_codec='VC-1' WHERE upper(replace(video_codec, '-', ''))='VC1'`,
		`DELETE FROM media_files WHERE process_status='failed' AND error_code='UNSUPPORTED_EXTENSION' AND media_type=''`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_batch ON media_files(batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_status ON media_files(process_status)`,
		`CREATE TABLE IF NOT EXISTS movies (
			tmdb_id INT PRIMARY KEY,
			imdb_id TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL,
			original_title TEXT NOT NULL DEFAULT '',
			year INT NOT NULL DEFAULT 0,
			release_date TEXT NOT NULL DEFAULT '',
			overview TEXT NOT NULL DEFAULT '',
			runtime INT NOT NULL DEFAULT 0,
			genres TEXT NOT NULL DEFAULT '',
			genre_ids TEXT NOT NULL DEFAULT '',
			original_language TEXT NOT NULL DEFAULT '',
			production_countries TEXT NOT NULL DEFAULT '',
			keywords TEXT NOT NULL DEFAULT '',
			rating TEXT NOT NULL DEFAULT '',
			poster_path TEXT NOT NULL DEFAULT '',
			backdrop_path TEXT NOT NULL DEFAULT '',
			collection_id INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE movies ADD COLUMN IF NOT EXISTS genre_ids TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE movies ADD COLUMN IF NOT EXISTS original_language TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE movies ADD COLUMN IF NOT EXISTS production_countries TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE movies ADD COLUMN IF NOT EXISTS keywords TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS tv_shows (
			tmdb_id INT PRIMARY KEY,
			tvdb_id INT NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			original_name TEXT NOT NULL DEFAULT '',
			year INT NOT NULL DEFAULT 0,
			first_air_date TEXT NOT NULL DEFAULT '',
			overview TEXT NOT NULL DEFAULT '',
			season_count INT NOT NULL DEFAULT 0,
			episode_count INT NOT NULL DEFAULT 0,
			genres TEXT NOT NULL DEFAULT '',
			genre_ids TEXT NOT NULL DEFAULT '',
			original_language TEXT NOT NULL DEFAULT '',
			origin_country TEXT NOT NULL DEFAULT '',
			keywords TEXT NOT NULL DEFAULT '',
			poster_path TEXT NOT NULL DEFAULT '',
			backdrop_path TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS genres TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS genre_ids TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS original_language TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS origin_country TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tv_shows ADD COLUMN IF NOT EXISTS keywords TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS tv_episodes (
			id TEXT PRIMARY KEY,
			show_tmdb_id INT NOT NULL REFERENCES tv_shows(tmdb_id) ON DELETE CASCADE,
			tmdb_id INT NOT NULL,
			season INT NOT NULL,
			episode INT NOT NULL,
			title TEXT NOT NULL,
			air_date TEXT NOT NULL DEFAULT '',
			released BOOLEAN NOT NULL DEFAULT true,
			overview TEXT NOT NULL DEFAULT '',
			runtime INT NOT NULL DEFAULT 0,
			still_path TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(show_tmdb_id, season, episode)
		)`,
		`ALTER TABLE tv_episodes ADD COLUMN IF NOT EXISTS released BOOLEAN NOT NULL DEFAULT true`,
		`UPDATE tv_episodes SET released = CASE WHEN air_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN air_date::date <= CURRENT_DATE ELSE false END`,
		`CREATE TABLE IF NOT EXISTS collections (
			tmdb_id INT PRIMARY KEY,
			name TEXT NOT NULL,
			overview TEXT NOT NULL DEFAULT '',
			movie_count INT NOT NULL DEFAULT 0,
			unreleased_count INT NOT NULL DEFAULT 0,
			local_count INT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'incomplete',
			poster_path TEXT NOT NULL DEFAULT '',
			backdrop_path TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE collections ADD COLUMN IF NOT EXISTS unreleased_count INT NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS collection_movies (
			collection_id INT NOT NULL REFERENCES collections(tmdb_id) ON DELETE CASCADE,
			movie_tmdb_id INT NOT NULL,
			title TEXT NOT NULL,
			release_date TEXT NOT NULL DEFAULT '',
			released BOOLEAN NOT NULL DEFAULT true,
			sort_order INT NOT NULL DEFAULT 0,
			PRIMARY KEY(collection_id, movie_tmdb_id)
		)`,
		`ALTER TABLE collection_movies ADD COLUMN IF NOT EXISTS released BOOLEAN NOT NULL DEFAULT true`,
		`UPDATE collection_movies SET released = CASE WHEN release_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN release_date::date <= CURRENT_DATE ELSE false END`,
		`UPDATE collections c SET
			movie_count = COALESCE((SELECT COUNT(*)::INT FROM collection_movies cm WHERE cm.collection_id=c.tmdb_id AND cm.released=true), 0),
			unreleased_count = COALESCE((SELECT COUNT(*)::INT FROM collection_movies cm WHERE cm.collection_id=c.tmdb_id AND cm.released=false), 0)`,
		`CREATE TABLE IF NOT EXISTS media_matches (
			file_id TEXT PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
			target_type TEXT NOT NULL,
			movie_tmdb_id INT NOT NULL DEFAULT 0,
			show_tmdb_id INT NOT NULL DEFAULT 0,
			episode_id TEXT NOT NULL DEFAULT '',
			confidence NUMERIC NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS organize_tasks (
			id TEXT PRIMARY KEY,
			file_id TEXT NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
			batch_id TEXT NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
			template_id TEXT NOT NULL DEFAULT '',
			source_path TEXT NOT NULL,
			target_path TEXT NOT NULL,
			task_status TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			executed_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS operation_histories (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL DEFAULT '',
			file_id TEXT NOT NULL DEFAULT '',
			batch_id TEXT NOT NULL DEFAULT '',
			source_path TEXT NOT NULL DEFAULT '',
			target_path TEXT NOT NULL DEFAULT '',
			operation_name TEXT NOT NULL,
			operation_status TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS collection_completion_histories (
			id TEXT PRIMARY KEY,
			collection_id INT NOT NULL,
			collection_tmdb_id INT NOT NULL,
			collection_name TEXT NOT NULL,
			source_path TEXT NOT NULL,
			target_path TEXT NOT NULL,
			movie_count INT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`UPDATE batches b SET
			total=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id), 0),
			done=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status='done'), 0),
			failed=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status='failed'), 0),
			incomplete_collection=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status='incomplete_collection'), 0)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Seed(ctx context.Context, dirs models.DirectoryConfig, settings models.SystemSettings, templates []models.NamingTemplate) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO system_directories (id, incoming_path, staging_path, failed_path, incomplete_collections_path)
		VALUES (1, $1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		dirs.IncomingPath, dirs.StagingPath, dirs.FailedPath, dirs.IncompleteCollectionsPath); err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO system_settings (id, tmdb_api_key, network_proxy, classification_yaml,
		clouddrive_address, clouddrive_username, clouddrive_password, clouddrive_token,
		clouddrive_root_path, clouddrive_staging_path, clouddrive_failed_path, clouddrive_incomplete_path)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) ON CONFLICT (id) DO NOTHING`,
		settings.TMDBAPIKey, settings.NetworkProxy, settings.ClassificationYAML,
		settings.CloudDriveAddress, settings.CloudDriveUsername, settings.CloudDrivePassword, settings.CloudDriveToken,
		settings.CloudDriveRootPath, settings.CloudDriveStagingPath, settings.CloudDriveFailedPath, settings.CloudDriveIncompletePath); err != nil {
		return err
	}
	if settings.ClassificationYAML != "" {
		if _, err := s.db.Exec(ctx, `UPDATE system_settings SET classification_yaml=$1 WHERE id=1 AND classification_yaml=''`, settings.ClassificationYAML); err != nil {
			return err
		}
	}
	for _, template := range templates {
		if _, err := s.db.Exec(ctx, `INSERT INTO naming_templates (template_type, name, template, enabled)
			VALUES ($1, $2, $3, $4) ON CONFLICT (template_type) DO NOTHING`,
			template.TemplateType, template.Name, template.Template, template.Enabled); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Settings(ctx context.Context) (models.SystemSettings, error) {
	var settings models.SystemSettings
	err := s.db.QueryRow(ctx, `SELECT tmdb_api_key, network_proxy, classification_yaml,
		clouddrive_address, clouddrive_username, clouddrive_password, clouddrive_token,
		clouddrive_root_path, clouddrive_staging_path, clouddrive_failed_path, clouddrive_incomplete_path, updated_at
		FROM system_settings WHERE id=1`).
		Scan(&settings.TMDBAPIKey, &settings.NetworkProxy, &settings.ClassificationYAML,
			&settings.CloudDriveAddress, &settings.CloudDriveUsername, &settings.CloudDrivePassword, &settings.CloudDriveToken,
			&settings.CloudDriveRootPath, &settings.CloudDriveStagingPath, &settings.CloudDriveFailedPath, &settings.CloudDriveIncompletePath, &settings.UpdatedAt)
	return settings, err
}

func (s *Store) SaveSettings(ctx context.Context, settings models.SystemSettings) (models.SystemSettings, error) {
	_, err := s.db.Exec(ctx, `INSERT INTO system_settings (id, tmdb_api_key, network_proxy, classification_yaml)
		VALUES (1, $1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET tmdb_api_key=$1, network_proxy=$2, classification_yaml=$3, updated_at=now()`,
		settings.TMDBAPIKey, settings.NetworkProxy, settings.ClassificationYAML)
	if err != nil {
		return models.SystemSettings{}, err
	}
	return s.Settings(ctx)
}

func (s *Store) SaveClassification(ctx context.Context, yaml string) (models.SystemSettings, error) {
	current, err := s.Settings(ctx)
	if err != nil {
		return models.SystemSettings{}, err
	}
	current.ClassificationYAML = yaml
	return s.SaveSettings(ctx, current)
}

func (s *Store) CloudDriveSettings(ctx context.Context) (models.CloudDriveSettings, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return models.CloudDriveSettings{}, err
	}
	return cloudDriveFromSystem(settings), nil
}

func (s *Store) SaveCloudDriveSettings(ctx context.Context, settings models.CloudDriveSettings) (models.CloudDriveSettings, error) {
	_, err := s.db.Exec(ctx, `UPDATE system_settings SET clouddrive_address=$1,
		clouddrive_username=$2, clouddrive_password=$3, clouddrive_token=$4, clouddrive_root_path=$5,
		clouddrive_staging_path=$6, clouddrive_failed_path=$7, clouddrive_incomplete_path=$8, updated_at=now()
		WHERE id=1`,
		settings.Address, settings.Username, settings.Password, settings.Token, settings.RootPath,
		settings.StagingPath, settings.FailedPath, settings.IncompleteCollectionsPath)
	if err != nil {
		return models.CloudDriveSettings{}, err
	}
	return s.CloudDriveSettings(ctx)
}

func cloudDriveFromSystem(settings models.SystemSettings) models.CloudDriveSettings {
	return models.CloudDriveSettings{
		Address:                   settings.CloudDriveAddress,
		Username:                  settings.CloudDriveUsername,
		Password:                  settings.CloudDrivePassword,
		Token:                     settings.CloudDriveToken,
		RootPath:                  settings.CloudDriveRootPath,
		StagingPath:               settings.CloudDriveStagingPath,
		FailedPath:                settings.CloudDriveFailedPath,
		IncompleteCollectionsPath: settings.CloudDriveIncompletePath,
		UpdatedAt:                 settings.UpdatedAt,
	}
}

func (s *Store) P115Settings(ctx context.Context) (models.P115Settings, error) {
	var settings models.P115Settings
	err := s.db.QueryRow(ctx, `SELECT enabled, auth_mode, app_id, app_secret, access_token, refresh_token, cookies, cookie_login_app,
		strm_output_path, public_base_url, direct_url_ttl_seconds, user_agent_mode, fixed_user_agent, libraries_yaml,
		delete_missing_strm, stale_before_delete, keep_deleted_days, refresh_emby_after_sync, sync_cron_enabled, sync_interval_minutes,
		emby_upstream_url, emby_public_url, emby_proxy_port, emby_proxy_base_path, emby_api_key, updated_at
		FROM p115_settings WHERE id=1`).
		Scan(&settings.Enabled, &settings.AuthMode, &settings.AppID, &settings.AppSecret, &settings.AccessToken, &settings.RefreshToken, &settings.Cookies, &settings.CookieLoginApp,
			&settings.STRMOutputPath, &settings.PublicBaseURL, &settings.DirectURLTTLSeconds, &settings.UserAgentMode, &settings.FixedUserAgent, &settings.LibrariesYAML,
			&settings.DeleteMissingSTRM, &settings.StaleBeforeDelete, &settings.KeepDeletedDays, &settings.RefreshEmbyAfterSync, &settings.SyncCronEnabled, &settings.SyncIntervalMinutes,
			&settings.EmbyUpstreamURL, &settings.EmbyPublicURL, &settings.EmbyProxyPort, &settings.EmbyProxyBasePath, &settings.EmbyAPIKey, &settings.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		settings = models.P115Settings{
			Enabled:             true,
			AuthMode:            "cookies",
			CookieLoginApp:      "wechatmini",
			STRMOutputPath:      "/data/Curio/strm",
			DirectURLTTLSeconds: 300,
			UserAgentMode:       "inherit",
			DeleteMissingSTRM:   true,
			KeepDeletedDays:     7,
			SyncIntervalMinutes: 60,
			EmbyProxyPort:       8097,
			EmbyProxyBasePath:   "/emby",
		}
		if _, saveErr := s.SaveP115Settings(ctx, settings); saveErr != nil {
			return models.P115Settings{}, saveErr
		}
		return s.P115Settings(ctx)
	}
	return settings, err
}

func (s *Store) SaveP115Settings(ctx context.Context, settings models.P115Settings) (models.P115Settings, error) {
	_, err := s.db.Exec(ctx, `INSERT INTO p115_settings (
		id, enabled, auth_mode, app_id, app_secret, access_token, refresh_token, cookies, cookie_login_app,
		strm_output_path, public_base_url, direct_url_ttl_seconds, user_agent_mode, fixed_user_agent, libraries_yaml,
		delete_missing_strm, stale_before_delete, keep_deleted_days, refresh_emby_after_sync, sync_cron_enabled, sync_interval_minutes,
		emby_upstream_url, emby_public_url, emby_proxy_port, emby_proxy_base_path, emby_api_key
	) VALUES (1,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
	ON CONFLICT (id) DO UPDATE SET
		enabled=$1, auth_mode=$2, app_id=$3, app_secret=$4, access_token=$5, refresh_token=$6, cookies=$7, cookie_login_app=$8,
		strm_output_path=$9, public_base_url=$10, direct_url_ttl_seconds=$11, user_agent_mode=$12, fixed_user_agent=$13, libraries_yaml=$14,
		delete_missing_strm=$15, stale_before_delete=$16, keep_deleted_days=$17, refresh_emby_after_sync=$18, sync_cron_enabled=$19, sync_interval_minutes=$20,
		emby_upstream_url=$21, emby_public_url=$22, emby_proxy_port=$23, emby_proxy_base_path=$24, emby_api_key=$25, updated_at=now()`,
		settings.Enabled, settings.AuthMode, settings.AppID, settings.AppSecret, settings.AccessToken, settings.RefreshToken, settings.Cookies, settings.CookieLoginApp,
		settings.STRMOutputPath, settings.PublicBaseURL, settings.DirectURLTTLSeconds, settings.UserAgentMode, settings.FixedUserAgent, settings.LibrariesYAML,
		settings.DeleteMissingSTRM, settings.StaleBeforeDelete, settings.KeepDeletedDays, settings.RefreshEmbyAfterSync, settings.SyncCronEnabled, settings.SyncIntervalMinutes,
		settings.EmbyUpstreamURL, settings.EmbyPublicURL, settings.EmbyProxyPort, settings.EmbyProxyBasePath, settings.EmbyAPIKey)
	if err != nil {
		return models.P115Settings{}, err
	}
	return s.P115Settings(ctx)
}

func (s *Store) UpsertSTRMLink(ctx context.Context, link models.STRMLink) error {
	_, err := s.db.Exec(ctx, `INSERT INTO strm_links (
			id, provider, library_cid, library_name, library_type, relative_path, remote_path, remote_file_id, pickcode, sha1, size,
			strm_path, play_path, source_tree_hash, tree_version, resolve_status, status, error_code, error_message, generated_at, resolved_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		ON CONFLICT (provider, library_cid, relative_path) DO UPDATE SET
			library_name=$4, library_type=$5, remote_path=$7,
			remote_file_id=COALESCE(NULLIF($8, ''), strm_links.remote_file_id),
			pickcode=COALESCE(NULLIF($9, ''), strm_links.pickcode),
			sha1=COALESCE(NULLIF($10, ''), strm_links.sha1),
			size=CASE WHEN $11 > 0 THEN $11 ELSE strm_links.size END,
			strm_path=$12, play_path=$13, source_tree_hash=$14, tree_version=$15,
			resolve_status=CASE WHEN NULLIF($9, '') IS NULL THEN strm_links.resolve_status ELSE $16 END,
			status=$17, error_code=$18, error_message=$19, updated_at=now()`,
		link.ID, link.Provider, link.LibraryCID, link.LibraryName, link.LibraryType, link.RelativePath, link.RemotePath, link.RemoteFileID, link.PickCode, link.SHA1, link.Size,
		link.STRMPath, link.PlayPath, link.SourceTreeHash, link.TreeVersion, link.ResolveStatus, link.Status, link.ErrorCode, link.ErrorMessage, link.GeneratedAt, link.ResolvedAt)
	return err
}

func (s *Store) STRMLink(ctx context.Context, id string) (models.STRMLink, error) {
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, generated_at, resolved_at, updated_at
		FROM strm_links WHERE id=$1`, id)
	if err != nil {
		return models.STRMLink{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	return scanSTRMLink(rows)
}

func (s *Store) STRMLinkByPath(ctx context.Context, path string) (models.STRMLink, error) {
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, generated_at, resolved_at, updated_at
		FROM strm_links WHERE strm_path=$1 OR play_path=$1 ORDER BY updated_at DESC LIMIT 1`, path)
	if err != nil {
		return models.STRMLink{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	return scanSTRMLink(rows)
}

func (s *Store) STRMLinkByPlayRoute(ctx context.Context, provider, route string, playPaths []string) (models.STRMLink, error) {
	route = strings.Trim(strings.ReplaceAll(route, "\\", "/"), "/")
	paths := make([]string, 0, len(playPaths)+1)
	seen := map[string]struct{}{}
	for _, value := range append(playPaths, "/play/115/"+route) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		paths = append(paths, value)
	}
	if route == "" || len(paths) == 0 {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	playSuffix := "/play/115/" + route
	relativeSuffix := "/" + route
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, generated_at, resolved_at, updated_at
		FROM strm_links
		WHERE provider=$1 AND status=$2 AND (
			play_path=ANY($3)
			OR right(play_path, length($4))=$4
			OR relative_path=$5
			OR right(relative_path, length($6))=$6
		)
		ORDER BY CASE
			WHEN play_path=ANY($3) THEN 0
			WHEN right(play_path, length($4))=$4 THEN 1
			WHEN relative_path=$5 THEN 2
			ELSE 3
		END, updated_at DESC
		LIMIT 1`, provider, models.STRMStatusGenerated, paths, playSuffix, route, relativeSuffix)
	if err != nil {
		return models.STRMLink{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	return scanSTRMLink(rows)
}

func (s *Store) STRMLinkByEmbyItem(ctx context.Context, serverID, itemID string) (models.STRMLink, error) {
	rows, err := s.db.Query(ctx, `SELECT sl.id, sl.provider, sl.library_cid, sl.library_name, sl.library_type, sl.relative_path, sl.remote_path,
		sl.remote_file_id, sl.pickcode, sl.sha1, sl.size, sl.strm_path, sl.play_path, sl.source_tree_hash, sl.tree_version, sl.resolve_status, sl.status,
		sl.error_code, sl.error_message, sl.generated_at, sl.resolved_at, sl.updated_at
		FROM emby_strm_items ei
		JOIN strm_links sl ON sl.id=ei.strm_link_id
		WHERE ei.emby_server_id=$1 AND ei.emby_item_id=$2 AND ei.status='active'
		ORDER BY ei.last_seen_at DESC LIMIT 1`, serverID, itemID)
	if err != nil {
		return models.STRMLink{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.STRMLink{}, pgx.ErrNoRows
	}
	return scanSTRMLink(rows)
}

func (s *Store) ActiveSTRMLinksByLibrary(ctx context.Context, provider, libraryCID string) ([]models.STRMLink, error) {
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, generated_at, resolved_at, updated_at
		FROM strm_links WHERE provider=$1 AND library_cid=$2 AND status IN ($3,$4)`, provider, libraryCID, models.STRMStatusGenerated, models.STRMStatusStale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]models.STRMLink, 0)
	for rows.Next() {
		link, err := scanSTRMLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) STRMLinksByStatuses(ctx context.Context, statuses []string) ([]models.STRMLink, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT id, provider, library_cid, library_name, library_type, relative_path, remote_path,
		remote_file_id, pickcode, sha1, size, strm_path, play_path, source_tree_hash, tree_version, resolve_status, status,
		error_code, error_message, generated_at, resolved_at, updated_at
		FROM strm_links WHERE status=ANY($1)`, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]models.STRMLink, 0)
	for rows.Next() {
		link, err := scanSTRMLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) MarkSTRMLinkStatus(ctx context.Context, id, status, resolveStatus, code, message string) error {
	_, err := s.db.Exec(ctx, `UPDATE strm_links SET status=$2, resolve_status=$3, error_code=$4, error_message=$5, updated_at=now() WHERE id=$1`,
		id, status, resolveStatus, code, message)
	return err
}

func (s *Store) UpdateSTRMLinkResolved(ctx context.Context, id, remoteFileID, pickcode, sha1 string, size int64) error {
	_, err := s.db.Exec(ctx, `UPDATE strm_links SET remote_file_id=$2, pickcode=$3, sha1=$4, size=$5,
		resolve_status=$6, error_code='', error_message='', resolved_at=now(), updated_at=now() WHERE id=$1`,
		id, remoteFileID, pickcode, sha1, size, models.STRMResolveResolved)
	return err
}

func (s *Store) UpdateSTRMLinkPlayPath(ctx context.Context, id, playPath string) error {
	_, err := s.db.Exec(ctx, `UPDATE strm_links SET play_path=$2, updated_at=now() WHERE id=$1`, id, playPath)
	return err
}

func (s *Store) CreateP115SyncRun(ctx context.Context, run models.P115SyncRun) error {
	if run.Status == "" {
		run.Status = models.P115SyncStatusRunning
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	_, err := s.db.Exec(ctx, `INSERT INTO p115_sync_runs
		(id, trigger, status, mode, tree_version, exported, generated, restored, updated_count, deleted_count, skipped, failed, error_message, started_at, duration_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		run.ID, run.Trigger, run.Status, run.Mode, run.TreeVersion, run.Exported, run.Generated, run.Restored, run.Updated, run.Deleted, run.Skipped, run.Failed, run.ErrorMessage, run.StartedAt, run.DurationMS)
	return err
}

func (s *Store) FinishP115SyncRun(ctx context.Context, run models.P115SyncRun) error {
	_, err := s.db.Exec(ctx, `UPDATE p115_sync_runs SET
		status=$2, mode=$3, tree_version=$4, exported=$5, generated=$6, restored=$7, updated_count=$8,
		deleted_count=$9, skipped=$10, failed=$11, error_message=$12, ended_at=$13, duration_ms=$14
		WHERE id=$1`,
		run.ID, run.Status, run.Mode, run.TreeVersion, run.Exported, run.Generated, run.Restored, run.Updated,
		run.Deleted, run.Skipped, run.Failed, run.ErrorMessage, run.EndedAt, run.DurationMS)
	return err
}

func (s *Store) P115SyncRuns(ctx context.Context, limit int) ([]models.P115SyncRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(ctx, `SELECT id, trigger, status, mode, tree_version, exported, generated, restored, updated_count,
		deleted_count, skipped, failed, error_message, started_at, ended_at, duration_ms
		FROM p115_sync_runs ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]models.P115SyncRun, 0)
	for rows.Next() {
		run, err := scanP115SyncRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) LatestP115SyncRun(ctx context.Context, triggers []string) (models.P115SyncRun, bool, error) {
	if len(triggers) == 0 {
		return models.P115SyncRun{}, false, nil
	}
	rows, err := s.db.Query(ctx, `SELECT id, trigger, status, mode, tree_version, exported, generated, restored, updated_count,
		deleted_count, skipped, failed, error_message, started_at, ended_at, duration_ms
		FROM p115_sync_runs WHERE trigger=ANY($1) ORDER BY started_at DESC LIMIT 1`, triggers)
	if err != nil {
		return models.P115SyncRun{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return models.P115SyncRun{}, false, rows.Err()
	}
	run, err := scanP115SyncRun(rows)
	return run, err == nil, err
}

func (s *Store) ReplaceP115Snapshot(ctx context.Context, libraryCID, treeVersion string, items []models.P115TreeSnapshotItem) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM p115_tree_snapshots WHERE library_cid=$1`, libraryCID); err != nil {
		return err
	}
	for _, item := range items {
		contentKey := item.LibraryCID + ":" + item.RelativePath
		if _, err := tx.Exec(ctx, `INSERT INTO p115_tree_snapshots
			(library_cid, content_key, tree_version, relative_path, name, remote_file_id, parent_file_id, pickcode, sha1, size, extension, depth, is_directory, is_media, source_tree_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			libraryCID, contentKey, treeVersion, item.RelativePath, item.Name, item.RemoteFileID, item.ParentFileID, item.PickCode, item.SHA1, item.Size, item.Extension, item.Depth, item.IsDirectory, item.IsMedia, item.SourceTreeHash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) P115Snapshot(ctx context.Context, libraryCID string) ([]models.P115TreeSnapshotItem, string, error) {
	rows, err := s.db.Query(ctx, `SELECT library_cid, tree_version, relative_path, name, remote_file_id, parent_file_id, pickcode, sha1, size, extension, depth, is_directory, is_media, source_tree_hash
		FROM p115_tree_snapshots WHERE library_cid=$1 ORDER BY relative_path`, libraryCID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]models.P115TreeSnapshotItem, 0)
	version := ""
	for rows.Next() {
		var item models.P115TreeSnapshotItem
		if err := rows.Scan(&item.LibraryCID, &item.TreeVersion, &item.RelativePath, &item.Name, &item.RemoteFileID, &item.ParentFileID, &item.PickCode, &item.SHA1, &item.Size, &item.Extension, &item.Depth, &item.IsDirectory, &item.IsMedia, &item.SourceTreeHash); err != nil {
			return nil, "", err
		}
		if version == "" {
			version = item.TreeVersion
		}
		items = append(items, item)
	}
	return items, version, rows.Err()
}

func (s *Store) ReplaceP115Nodes(ctx context.Context, libraryCID, treeVersion string, nodes []models.P115Node) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM p115_nodes WHERE library_cid=$1`, libraryCID); err != nil {
		return err
	}
	for _, node := range nodes {
		if node.RemoteFileID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO p115_nodes
			(library_cid, remote_file_id, parent_file_id, tree_version, relative_path, name, pickcode, sha1, size, is_directory, is_media, is_alive, source_tree_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			libraryCID, node.RemoteFileID, node.ParentFileID, treeVersion, node.RelativePath, node.Name, node.PickCode, node.SHA1, node.Size, node.IsDirectory, node.IsMedia, node.IsAlive, node.SourceTreeHash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ReplaceP115NodesAndCursor(ctx context.Context, libraryCID, treeVersion string, nodes []models.P115Node, cursor models.P115EventCursor) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM p115_nodes WHERE library_cid=$1`, libraryCID); err != nil {
		return err
	}
	for _, node := range nodes {
		if node.RemoteFileID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO p115_nodes
			(library_cid, remote_file_id, parent_file_id, tree_version, relative_path, name, pickcode, sha1, size, is_directory, is_media, is_alive, source_tree_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			libraryCID, node.RemoteFileID, node.ParentFileID, treeVersion, node.RelativePath, node.Name, node.PickCode, node.SHA1, node.Size, node.IsDirectory, node.IsMedia, node.IsAlive, node.SourceTreeHash); err != nil {
			return err
		}
	}
	if cursor.LibraryCID == "" {
		cursor.LibraryCID = libraryCID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO p115_event_cursors
		(library_cid, last_event_id, last_event_time, last_sync_status, last_sync_error)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (library_cid) DO UPDATE SET
			last_event_id=$2, last_event_time=$3, last_sync_status=$4, last_sync_error=$5, updated_at=now()`,
		cursor.LibraryCID, cursor.LastEventID, cursor.LastEventTime, cursor.LastSyncStatus, cursor.LastSyncError); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) P115Nodes(ctx context.Context, libraryCID string, aliveOnly bool) ([]models.P115Node, string, error) {
	query := `SELECT library_cid, remote_file_id, parent_file_id, tree_version, relative_path, name, pickcode, sha1, size, is_directory, is_media, is_alive, source_tree_hash, updated_at
		FROM p115_nodes WHERE library_cid=$1`
	if aliveOnly {
		query += ` AND is_alive`
	}
	query += ` ORDER BY relative_path`
	rows, err := s.db.Query(ctx, query, libraryCID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	nodes := make([]models.P115Node, 0)
	version := ""
	for rows.Next() {
		var node models.P115Node
		if err := rows.Scan(&node.LibraryCID, &node.RemoteFileID, &node.ParentFileID, &node.TreeVersion, &node.RelativePath, &node.Name, &node.PickCode, &node.SHA1, &node.Size, &node.IsDirectory, &node.IsMedia, &node.IsAlive, &node.SourceTreeHash, &node.UpdatedAt); err != nil {
			return nil, "", err
		}
		if version == "" {
			version = node.TreeVersion
		}
		nodes = append(nodes, node)
	}
	return nodes, version, rows.Err()
}

func (s *Store) P115NodeCount(ctx context.Context, libraryCID string) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM p115_nodes WHERE library_cid=$1 AND is_alive`, libraryCID).Scan(&count)
	return count, err
}

func (s *Store) P115EventCursor(ctx context.Context, libraryCID string) (models.P115EventCursor, error) {
	var cursor models.P115EventCursor
	err := s.db.QueryRow(ctx, `SELECT library_cid, last_event_id, last_event_time, last_sync_status, last_sync_error, updated_at
		FROM p115_event_cursors WHERE library_cid=$1`, libraryCID).
		Scan(&cursor.LibraryCID, &cursor.LastEventID, &cursor.LastEventTime, &cursor.LastSyncStatus, &cursor.LastSyncError, &cursor.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		cursor.LibraryCID = libraryCID
		return cursor, nil
	}
	return cursor, err
}

func (s *Store) SaveP115EventCursor(ctx context.Context, cursor models.P115EventCursor) error {
	_, err := s.db.Exec(ctx, `INSERT INTO p115_event_cursors
		(library_cid, last_event_id, last_event_time, last_sync_status, last_sync_error)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (library_cid) DO UPDATE SET
			last_event_id=$2, last_event_time=$3, last_sync_status=$4, last_sync_error=$5, updated_at=now()`,
		cursor.LibraryCID, cursor.LastEventID, cursor.LastEventTime, cursor.LastSyncStatus, cursor.LastSyncError)
	return err
}

func (s *Store) UpsertEmbySTRMItem(ctx context.Context, item models.EmbySTRMItem) error {
	_, err := s.db.Exec(ctx, `INSERT INTO emby_strm_items (id, emby_server_id, emby_item_id, strm_link_id, strm_path, status)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (emby_server_id, emby_item_id) DO UPDATE SET
			strm_link_id=$4, strm_path=$5, status=$6, last_seen_at=now(), updated_at=now()`,
		item.ID, item.EmbyServerID, item.EmbyItemID, item.STRMLinkID, item.STRMPath, item.Status)
	return err
}

func (s *Store) Directories(ctx context.Context) (models.DirectoryConfig, error) {
	var dirs models.DirectoryConfig
	err := s.db.QueryRow(ctx, `SELECT incoming_path, staging_path, failed_path, incomplete_collections_path FROM system_directories WHERE id=1`).
		Scan(&dirs.IncomingPath, &dirs.StagingPath, &dirs.FailedPath, &dirs.IncompleteCollectionsPath)
	return dirs, err
}

func (s *Store) SaveDirectories(ctx context.Context, dirs models.DirectoryConfig) error {
	_, err := s.db.Exec(ctx, `INSERT INTO system_directories (id, incoming_path, staging_path, failed_path, incomplete_collections_path)
		VALUES (1, $1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET incoming_path=$1, staging_path=$2, failed_path=$3, incomplete_collections_path=$4, updated_at=now()`,
		dirs.IncomingPath, dirs.StagingPath, dirs.FailedPath, dirs.IncompleteCollectionsPath)
	return err
}

func (s *Store) Templates(ctx context.Context) ([]models.NamingTemplate, error) {
	rows, err := s.db.Query(ctx, `SELECT template_type, name, template, enabled, updated_at FROM naming_templates ORDER BY template_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates := make([]models.NamingTemplate, 0)
	for rows.Next() {
		var template models.NamingTemplate
		if err := rows.Scan(&template.TemplateType, &template.Name, &template.Template, &template.Enabled, &template.UpdatedAt); err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, rows.Err()
}

func (s *Store) Template(ctx context.Context, templateType string) (models.NamingTemplate, error) {
	var template models.NamingTemplate
	err := s.db.QueryRow(ctx, `SELECT template_type, name, template, enabled, updated_at FROM naming_templates WHERE template_type=$1 AND enabled=true`, templateType).
		Scan(&template.TemplateType, &template.Name, &template.Template, &template.Enabled, &template.UpdatedAt)
	return template, err
}

func (s *Store) SaveTemplate(ctx context.Context, template models.NamingTemplate) error {
	_, err := s.db.Exec(ctx, `INSERT INTO naming_templates (template_type, name, template, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (template_type) DO UPDATE SET name=$2, template=$3, enabled=$4, updated_at=now()`,
		template.TemplateType, template.Name, template.Template, template.Enabled)
	return err
}

func (s *Store) CreateBatch(ctx context.Context, id, source string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO batches (id, source, status) VALUES ($1, $2, $3)`, id, source, models.BatchStatusQueued)
	return err
}

func (s *Store) SetBatchStatus(ctx context.Context, id, status string) error {
	var allowed []string
	switch status {
	case models.BatchStatusRunning:
		allowed = []string{models.BatchStatusQueued}
	case models.BatchStatusCancelling:
		allowed = []string{models.BatchStatusQueued, models.BatchStatusRunning, models.BatchStatusCancelling}
	default:
		return fmt.Errorf("非法批次状态 %s", status)
	}
	tag, err := s.db.Exec(ctx, `UPDATE batches SET status=$2 WHERE id=$1 AND ended_at IS NULL AND status=ANY($3)`, id, status, allowed)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("批次 %s 无法流转到 %s", id, status)
	}
	return nil
}

func (s *Store) SetBatchTotal(ctx context.Context, id string, total int) error {
	_, err := s.db.Exec(ctx, `UPDATE batches SET total=$2 WHERE id=$1`, id, total)
	return err
}

func (s *Store) IncrementBatch(ctx context.Context, id, bucket string) error {
	column := map[string]string{
		models.StatusDone:                 "done",
		models.StatusFailed:               "failed",
		models.StatusIncompleteCollection: "incomplete_collection",
	}[bucket]
	if column == "" {
		return fmt.Errorf("未知批次计数字段 %s", bucket)
	}
	_, err := s.db.Exec(ctx, fmt.Sprintf(`UPDATE batches SET %s=%s+1 WHERE id=$1`, column, column), id)
	return err
}

func (s *Store) FinishBatch(ctx context.Context, id, status string) error {
	if !isTerminalBatchStatus(status) {
		return fmt.Errorf("非法批次结束状态 %s", status)
	}
	_, err := s.db.Exec(ctx, `UPDATE batches SET
		status=CASE WHEN status=$3 AND $2=$4 THEN $5 ELSE $2 END,
		ended_at=now()
		WHERE id=$1 AND ended_at IS NULL`,
		id, status, models.BatchStatusCancelling, models.BatchStatusComplete, models.BatchStatusCancelled)
	return err
}

func (s *Store) InterruptActiveBatches(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `UPDATE batches SET status=$1, ended_at=now()
		WHERE status IN ($2,$3,$4) AND ended_at IS NULL`,
		models.BatchStatusInterrupted, models.BatchStatusQueued, models.BatchStatusRunning, models.BatchStatusCancelling)
	return err
}

func (s *Store) ActiveBatch(ctx context.Context) (models.Batch, bool, error) {
	var batch models.Batch
	var ended sql.NullTime
	err := s.db.QueryRow(ctx, `SELECT id, source, status, total, done, failed, incomplete_collection, started_at, ended_at
		FROM batches WHERE status IN ($1,$2,$3) AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`,
		models.BatchStatusQueued, models.BatchStatusRunning, models.BatchStatusCancelling).
		Scan(&batch.ID, &batch.Source, &batch.Status, &batch.Total, &batch.Done, &batch.Failed, &batch.IncompleteCollection, &batch.StartedAt, &ended)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Batch{}, false, nil
	}
	if err != nil {
		return models.Batch{}, false, err
	}
	if ended.Valid {
		batch.EndedAt = &ended.Time
	}
	return batch, true, nil
}

func (s *Store) Batch(ctx context.Context, id string) (models.Batch, error) {
	var batch models.Batch
	var ended sql.NullTime
	err := s.db.QueryRow(ctx, `SELECT id, source, status, total, done, failed, incomplete_collection, started_at, ended_at FROM batches WHERE id=$1`, id).
		Scan(&batch.ID, &batch.Source, &batch.Status, &batch.Total, &batch.Done, &batch.Failed, &batch.IncompleteCollection, &batch.StartedAt, &ended)
	if ended.Valid {
		batch.EndedAt = &ended.Time
	}
	return batch, err
}

func (s *Store) Batches(ctx context.Context, limit int) ([]models.Batch, error) {
	rows, err := s.db.Query(ctx, `SELECT id, source, status, total, done, failed, incomplete_collection, started_at, ended_at FROM batches ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batches := make([]models.Batch, 0)
	for rows.Next() {
		batch, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func (s *Store) CreateMediaFile(ctx context.Context, file models.MediaFile) error {
	_, err := s.db.Exec(ctx, `INSERT INTO media_files
		(id, batch_id, original_path, current_path, original_file_name, extension, file_size, file_hash, hash_type, process_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		file.ID, file.BatchID, file.OriginalPath, file.CurrentPath, file.OriginalName, file.Extension, file.FileSize, file.FileHash, file.HashType, file.ProcessStatus)
	return err
}

func (s *Store) UpdateMediaStatus(ctx context.Context, id, status string) error {
	return s.updateMediaStatus(ctx, id, status, previousMediaStatuses(status)...)
}

func (s *Store) UpdateMediaParsed(ctx context.Context, id, parseTitle string, year, season, episode int, source string, technical models.MediaTechnicalInfo) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET parse_title=$2, parse_year=$3, season=$4, episode=$5,
		resolution=$6, source=$7, video_codec=$8, audio_codec=$9, audio_channels=$10, hdr_format=$11, process_status=$12, updated_at=now() WHERE id=$1 AND process_status=ANY($13)`,
		id, parseTitle, nullableInt(year), nullableInt(season), nullableInt(episode), technical.Resolution, source, technical.VideoCodec,
		technical.AudioCodec, technical.AudioChannels, technical.HDRFormat, models.StatusParsed,
		previousMediaStatuses(models.StatusParsed))
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+models.StatusParsed)
}

func (s *Store) UpdateMediaMatched(ctx context.Context, id, mediaType string) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET media_type=$2, match_status='matched', process_status=$3, updated_at=now()
		WHERE id=$1 AND process_status=ANY($4)`,
		id, mediaType, models.StatusMatched, previousMediaStatuses(models.StatusMatched))
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+models.StatusMatched)
}

func (s *Store) UpdateMediaPlanned(ctx context.Context, id, targetPath string) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET planned_target=$2, process_status=$3, updated_at=now()
		WHERE id=$1 AND process_status=ANY($4)`,
		id, targetPath, models.StatusPlanned, previousMediaStatuses(models.StatusPlanned))
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+models.StatusPlanned)
}

func (s *Store) IncrementMoveAttempt(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `UPDATE media_files SET move_attempts=move_attempts+1, updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *Store) UpdateMediaFinal(ctx context.Context, id, currentPath, finalPath, finalName, status string) error {
	if status != models.StatusDone && status != models.StatusIncompleteCollection {
		return fmt.Errorf("非法媒体完成状态 %s", status)
	}
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET current_path=$2, final_path=$3, final_file_name=$4, process_status=$5, last_verified_path=$3,
		error_code='', error_message='', updated_at=now() WHERE id=$1 AND process_status=ANY($6)`,
		id, currentPath, finalPath, finalName, status, previousMediaStatuses(status))
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+status)
}

func (s *Store) UpdateMediaFailure(ctx context.Context, id, currentPath, failedPath, code, message string) error {
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET current_path=$2, final_path=$3, process_status=$4, error_code=$5,
		error_message=$6, updated_at=now()
		WHERE id=$1 AND process_status NOT IN ($7,$8,$9)`,
		id, currentPath, failedPath, models.StatusFailed, code, message,
		models.StatusDone, models.StatusFailed, models.StatusIncompleteCollection)
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+models.StatusFailed)
}

func (s *Store) MediaFile(ctx context.Context, id string) (models.MediaFile, error) {
	rows, err := s.db.Query(ctx, `SELECT id, batch_id, original_path, current_path, final_path, original_file_name, final_file_name,
		extension, file_size, file_hash, hash_type, media_type, process_status, match_status, parse_title,
		COALESCE(parse_year,0), COALESCE(season,0), COALESCE(episode,0), resolution, source, video_codec, audio_codec, audio_channels, hdr_format,
		planned_target, move_attempts, last_verified_path, error_code, error_message, created_at, updated_at FROM media_files WHERE id=$1`, id)
	if err != nil {
		return models.MediaFile{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return models.MediaFile{}, err
		}
		return models.MediaFile{}, pgx.ErrNoRows
	}
	return scanMediaFile(rows)
}

func (s *Store) DeleteMediaFile(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM media_files WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 不存在")
}

func (s *Store) DeleteMediaFiles(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, errors.New("媒体文件 ID 不能为空")
	}
	rows, err := s.db.Query(ctx, `SELECT DISTINCT batch_id FROM media_files WHERE id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	batchIDs := make([]string, 0)
	for rows.Next() {
		var batchID string
		if err := rows.Scan(&batchID); err != nil {
			rows.Close()
			return nil, err
		}
		batchIDs = append(batchIDs, batchID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	tag, err := s.db.Exec(ctx, `DELETE FROM media_files WHERE id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	return batchIDs, requireRows(tag.RowsAffected(), "没有可删除的媒体记录")
}

func (s *Store) UpdateMediaTechnical(ctx context.Context, id string, technical models.MediaTechnicalInfo) error {
	_, err := s.db.Exec(ctx, `UPDATE media_files SET resolution=$2, video_codec=$3, audio_codec=$4,
		audio_channels=$5, hdr_format=$6, updated_at=now() WHERE id=$1`,
		id, technical.Resolution, technical.VideoCodec, technical.AudioCodec, technical.AudioChannels, technical.HDRFormat)
	return err
}

func (s *Store) UpdateMediaRearchiveFinal(ctx context.Context, id, parseTitle string, year, season, episode int, source string, technical models.MediaTechnicalInfo, mediaType, targetPath, movedPath, finalName, status string) error {
	if status != models.StatusDone && status != models.StatusIncompleteCollection {
		return fmt.Errorf("非法媒体完成状态 %s", status)
	}
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET parse_title=$2, parse_year=$3, season=$4, episode=$5, source=$6,
		resolution=$7, video_codec=$8, audio_codec=$9, audio_channels=$10, hdr_format=$11, media_type=$12, match_status='matched',
		process_status=$13, planned_target=$14, current_path=$15, final_path=$16, final_file_name=$17, last_verified_path=$16,
		error_code='', error_message='', updated_at=now() WHERE id=$1`,
		id, parseTitle, nullableInt(year), nullableInt(season), nullableInt(episode), source,
		technical.Resolution, technical.VideoCodec, technical.AudioCodec, technical.AudioChannels, technical.HDRFormat,
		mediaType, status, targetPath, movedPath, movedPath, finalName)
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 不存在")
}

func (s *Store) RecountBatch(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `UPDATE batches b SET
		total=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id), 0),
		done=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status=$2), 0),
		failed=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status=$3), 0),
		incomplete_collection=COALESCE((SELECT COUNT(*)::INT FROM media_files mf WHERE mf.batch_id=b.id AND mf.process_status=$4), 0)
		WHERE b.id=$1`,
		id, models.StatusDone, models.StatusFailed, models.StatusIncompleteCollection)
	return err
}

func (s *Store) RefreshCollectionLocalCounts(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `WITH counts AS (
			SELECT c.tmdb_id,
				(COUNT(DISTINCT mm.movie_tmdb_id) FILTER (
					WHERE cm.released=true AND mf.process_status IN ($1,$2)
				))::INT AS local_count
			FROM collections c
			LEFT JOIN collection_movies cm ON cm.collection_id=c.tmdb_id
			LEFT JOIN media_matches mm ON mm.movie_tmdb_id=cm.movie_tmdb_id
			LEFT JOIN media_files mf ON mf.id=mm.file_id
			GROUP BY c.tmdb_id
		)
		UPDATE collections c SET local_count=counts.local_count,
			status=CASE WHEN c.movie_count > 0 AND counts.local_count >= c.movie_count THEN 'complete' ELSE 'incomplete' END,
			updated_at=now()
		FROM counts WHERE counts.tmdb_id=c.tmdb_id`,
		models.StatusDone, models.StatusIncompleteCollection)
	return err
}

func (s *Store) ListMediaFiles(ctx context.Context, status, search string, limit, offset int) (models.MediaFilePage, error) {
	query := `SELECT id, batch_id, original_path, current_path, final_path, original_file_name, final_file_name,
		extension, file_size, file_hash, hash_type, media_type, process_status, match_status, parse_title,
		COALESCE(parse_year,0), COALESCE(season,0), COALESCE(episode,0), resolution, source, video_codec, audio_codec, audio_channels, hdr_format,
		planned_target, move_attempts, last_verified_path, error_code, error_message, created_at, updated_at FROM media_files`
	args := []any{}
	conditions := []string{}
	statuses := splitCSV(status)
	if len(statuses) > 0 {
		args = append(args, statuses)
		conditions = append(conditions, fmt.Sprintf("process_status=ANY($%d)", len(args)))
	}
	if search != "" {
		args = append(args, "%"+search+"%")
		p := fmt.Sprintf("$%d", len(args))
		conditions = append(conditions, fmt.Sprintf(`(id ILIKE %[1]s OR batch_id ILIKE %[1]s OR original_path ILIKE %[1]s OR current_path ILIKE %[1]s
			OR final_path ILIKE %[1]s OR original_file_name ILIKE %[1]s OR final_file_name ILIKE %[1]s OR extension ILIKE %[1]s
			OR file_hash ILIKE %[1]s OR hash_type ILIKE %[1]s OR media_type ILIKE %[1]s OR process_status ILIKE %[1]s OR match_status ILIKE %[1]s
			OR parse_title ILIKE %[1]s OR COALESCE(parse_year,0)::TEXT ILIKE %[1]s OR COALESCE(season,0)::TEXT ILIKE %[1]s
			OR COALESCE(episode,0)::TEXT ILIKE %[1]s OR resolution ILIKE %[1]s OR source ILIKE %[1]s OR video_codec ILIKE %[1]s
			OR audio_codec ILIKE %[1]s OR audio_channels ILIKE %[1]s OR hdr_format ILIKE %[1]s OR planned_target ILIKE %[1]s
			OR error_code ILIKE %[1]s OR error_message ILIKE %[1]s)`, p))
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, " AND ")
	}
	var total int
	countQuery := `SELECT COUNT(*)::INT FROM media_files`
	if len(conditions) > 0 {
		countQuery += ` WHERE ` + strings.Join(conditions, " AND ")
	}
	if err := s.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return models.MediaFilePage{}, err
	}
	query += fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.Query(ctx, query, pageArgs...)
	if err != nil {
		return models.MediaFilePage{}, err
	}
	defer rows.Close()
	files := make([]models.MediaFile, 0)
	for rows.Next() {
		file, err := scanMediaFile(rows)
		if err != nil {
			return models.MediaFilePage{}, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return models.MediaFilePage{}, err
	}
	return models.MediaFilePage{Items: files, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Store) MediaStats(ctx context.Context) (models.MediaStats, error) {
	var stats models.MediaStats
	err := s.db.QueryRow(ctx, `WITH media AS (
			SELECT
				COUNT(*)::INT AS total,
				(COUNT(*) FILTER (WHERE process_status=$1))::INT AS done,
				(COUNT(*) FILTER (WHERE process_status=$2))::INT AS failed,
				(COUNT(*) FILTER (WHERE process_status=$3))::INT AS incomplete_collection
			FROM media_files
		),
		active_shows AS (
			SELECT DISTINCT mm.show_tmdb_id
			FROM media_matches mm
			JOIN media_files mf ON mf.id=mm.file_id
			WHERE mm.show_tmdb_id > 0 AND mf.process_status IN ($1,$3)
		),
		episode_status AS (
			SELECT te.show_tmdb_id, te.season,
				EXISTS (
					SELECT 1
					FROM media_matches mm
					JOIN media_files mf ON mf.id=mm.file_id
					WHERE mm.episode_id=te.id AND mf.process_status IN ($1,$3)
				) AS local
			FROM tv_episodes te
			JOIN active_shows active ON active.show_tmdb_id=te.show_tmdb_id
			WHERE te.released=true
		),
		season_status AS (
			SELECT show_tmdb_id, season,
				(COUNT(*) FILTER (WHERE NOT local))::INT AS missing_count
			FROM episode_status
			GROUP BY show_tmdb_id, season
		),
		tv AS (
			SELECT
				COALESCE((SELECT COUNT(*)::INT FROM season_status WHERE missing_count > 0), 0) AS missing_season_count,
				COALESCE((SELECT COUNT(*)::INT FROM episode_status WHERE NOT local), 0) AS missing_episode_count
		)
		SELECT media.total, media.done, media.failed, media.incomplete_collection, tv.missing_season_count, tv.missing_episode_count
		FROM media, tv`,
		models.StatusDone, models.StatusFailed, models.StatusIncompleteCollection).
		Scan(&stats.Total, &stats.Done, &stats.Failed, &stats.IncompleteCollection, &stats.MissingTVSeasonCount, &stats.MissingTVEpisodeCount)
	return stats, err
}

func (s *Store) UpsertMovie(ctx context.Context, movie models.MovieMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO movies (tmdb_id, imdb_id, title, original_title, year, release_date, overview, runtime, genres, genre_ids, original_language, production_countries, keywords, rating, poster_path, backdrop_path, collection_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (tmdb_id) DO UPDATE SET imdb_id=$2, title=$3, original_title=$4, year=$5, release_date=$6, overview=$7,
		runtime=$8, genres=$9, genre_ids=$10, original_language=$11, production_countries=$12, keywords=$13, rating=$14, poster_path=$15, backdrop_path=$16, collection_id=$17, updated_at=now()`,
		movie.TMDBID, movie.IMDBID, movie.Title, movie.Original, movie.Year, movie.ReleaseDate, movie.Overview, movie.Runtime,
		movie.Genres, movie.GenreIDs, movie.OriginalLanguage, movie.ProductionCountries, movie.Keywords, movie.Rating, movie.PosterPath, movie.BackdropPath, movie.CollectionID)
	return err
}

func (s *Store) UpsertTVShow(ctx context.Context, show models.TVShowMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO tv_shows (tmdb_id, tvdb_id, name, original_name, year, first_air_date, overview, season_count, episode_count, genres, genre_ids, original_language, origin_country, keywords, poster_path, backdrop_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (tmdb_id) DO UPDATE SET tvdb_id=$2, name=$3, original_name=$4, year=$5, first_air_date=$6,
		overview=$7, season_count=$8, episode_count=$9, genres=$10, genre_ids=$11, original_language=$12, origin_country=$13, keywords=$14, poster_path=$15, backdrop_path=$16, updated_at=now()`,
		show.TMDBID, show.TVDBID, show.Name, show.Original, show.Year, show.FirstAirDate, show.Overview, show.SeasonCount, show.EpisodeCount,
		show.Genres, show.GenreIDs, show.OriginalLanguage, show.OriginCountry, show.Keywords, show.PosterPath, show.BackdropPath)
	return err
}

func (s *Store) UpsertTVEpisode(ctx context.Context, episode models.TVEpisodeMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO tv_episodes (id, show_tmdb_id, tmdb_id, season, episode, title, air_date, released, overview, runtime, still_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (show_tmdb_id, season, episode) DO UPDATE SET tmdb_id=$3, title=$6, air_date=$7, released=$8, overview=$9, runtime=$10, still_path=$11, updated_at=now()`,
		episode.ID, episode.ShowTMDBID, episode.TMDBID, episode.Season, episode.Episode, episode.Title, episode.AirDate, episode.Released, episode.Overview, episode.Runtime, episode.StillPath)
	return err
}

func (s *Store) DeleteTVEpisodesNotIn(ctx context.Context, showID int, episodeIDs []string) error {
	if len(episodeIDs) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, `DELETE FROM tv_episodes WHERE show_tmdb_id=$1 AND NOT (id = ANY($2))`, showID, episodeIDs)
	return err
}

func (s *Store) TVShows(ctx context.Context, search string, limit, offset int) (models.TVShowPage, error) {
	args := []any{models.StatusDone, models.StatusIncompleteCollection}
	where := `WHERE EXISTS (
		SELECT 1
		FROM media_matches mm
		JOIN media_files mf ON mf.id=mm.file_id
		WHERE mm.show_tmdb_id=s.tmdb_id AND mf.process_status IN ($1,$2)
	)`
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+search+"%")
		where += fmt.Sprintf(` AND (s.name ILIKE %[1]s OR s.original_name ILIKE %[1]s OR s.tmdb_id::TEXT ILIKE %[1]s OR s.overview ILIKE %[1]s)`, fmt.Sprintf("$%d", len(args)))
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::INT FROM tv_shows s `+where, args...).Scan(&total); err != nil {
		return models.TVShowPage{}, err
	}
	args = append(args, limit, offset)
	order := fmt.Sprintf(`ORDER BY s.updated_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := s.db.Query(ctx, tvShowStatusSQL(where, order), args...)
	if err != nil {
		return models.TVShowPage{}, err
	}
	defer rows.Close()
	items, err := scanTVShowStatuses(rows)
	if err != nil {
		return models.TVShowPage{}, err
	}
	return models.TVShowPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Store) TVShow(ctx context.Context, id int) (models.TVShowStatus, error) {
	rows, err := s.db.Query(ctx, tvShowStatusSQL(`WHERE s.tmdb_id=$3`, ``), models.StatusDone, models.StatusIncompleteCollection, id)
	if err != nil {
		return models.TVShowStatus{}, err
	}
	defer rows.Close()
	shows, err := scanTVShowStatuses(rows)
	if err != nil {
		return models.TVShowStatus{}, err
	}
	if len(shows) == 0 {
		return models.TVShowStatus{}, pgx.ErrNoRows
	}
	seasons, err := s.TVSeasons(ctx, id)
	if err != nil {
		return models.TVShowStatus{}, err
	}
	shows[0].Seasons = seasons
	return shows[0], nil
}

func (s *Store) TVSeasons(ctx context.Context, showID int) ([]models.TVSeasonStatus, error) {
	rows, err := s.db.Query(ctx, `SELECT te.id, te.show_tmdb_id, te.tmdb_id, te.season, te.episode, te.title, te.air_date, te.released,
		te.overview, te.runtime, te.still_path, COALESCE(local.file_id, ''), COALESCE(local.file_path, ''), COALESCE(local.process_status, '')
		FROM tv_episodes te
		LEFT JOIN LATERAL (
			SELECT mf.id AS file_id, COALESCE(NULLIF(mf.final_path, ''), mf.current_path) AS file_path, mf.process_status
			FROM media_matches mm
			JOIN media_files mf ON mf.id=mm.file_id
			WHERE mm.episode_id=te.id AND mf.process_status IN ($2,$3)
			ORDER BY mf.updated_at DESC
			LIMIT 1
		) local ON true
		WHERE te.show_tmdb_id=$1
		ORDER BY te.season, te.episode`, showID, models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seasonOrder := make([]int, 0)
	bySeason := map[int]*models.TVSeasonStatus{}
	for rows.Next() {
		var episode models.TVEpisodeMetadata
		if err := rows.Scan(&episode.ID, &episode.ShowTMDBID, &episode.TMDBID, &episode.Season, &episode.Episode, &episode.Title, &episode.AirDate, &episode.Released, &episode.Overview, &episode.Runtime, &episode.StillPath, &episode.FileID, &episode.FilePath, &episode.ProcessStatus); err != nil {
			return nil, err
		}
		episode.Local = episode.FileID != ""
		season, ok := bySeason[episode.Season]
		if !ok {
			season = &models.TVSeasonStatus{Season: episode.Season, Episodes: make([]models.TVEpisodeMetadata, 0)}
			bySeason[episode.Season] = season
			seasonOrder = append(seasonOrder, episode.Season)
		}
		season.EpisodeCount++
		if episode.Released {
			season.ReleasedEpisodeCount++
			if episode.Local {
				season.LocalEpisodeCount++
			} else {
				season.MissingEpisodeCount++
			}
		} else {
			season.UnreleasedEpisodeCount++
		}
		season.Episodes = append(season.Episodes, episode)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seasons := make([]models.TVSeasonStatus, 0, len(seasonOrder))
	for _, number := range seasonOrder {
		season := bySeason[number]
		season.Status = tvAvailabilityStatus(season.ReleasedEpisodeCount, season.MissingEpisodeCount)
		seasons = append(seasons, *season)
	}
	return seasons, nil
}

func tvShowStatusSQL(where, order string) string {
	return `WITH episode_status AS (
		SELECT te.show_tmdb_id, te.season, te.released,
			EXISTS (
				SELECT 1
				FROM media_matches mm
				JOIN media_files mf ON mf.id=mm.file_id
				WHERE mm.episode_id=te.id AND mf.process_status IN ($1,$2)
			) AS local
		FROM tv_episodes te
	),
	show_stats AS (
		SELECT show_tmdb_id,
			(COUNT(*) FILTER (WHERE released))::INT AS released_count,
			(COUNT(*) FILTER (WHERE NOT released))::INT AS unreleased_count,
			(COUNT(*) FILTER (WHERE released AND local))::INT AS local_count,
			(COUNT(*) FILTER (WHERE released AND NOT local))::INT AS missing_count
		FROM episode_status
		GROUP BY show_tmdb_id
	),
	season_stats AS (
		SELECT show_tmdb_id, season,
			(COUNT(*) FILTER (WHERE released AND NOT local))::INT AS missing_count
		FROM episode_status
		GROUP BY show_tmdb_id, season
	),
	missing_seasons AS (
		SELECT show_tmdb_id, (COUNT(*) FILTER (WHERE missing_count > 0))::INT AS missing_season_count
		FROM season_stats
		GROUP BY show_tmdb_id
	)
	SELECT s.tmdb_id, s.name, s.original_name, s.year, s.first_air_date, s.overview, s.season_count, s.episode_count,
		COALESCE(ss.released_count, 0), COALESCE(ss.unreleased_count, 0), COALESCE(ss.local_count, 0), COALESCE(ss.missing_count, 0),
		COALESCE(ms.missing_season_count, 0),
		CASE
			WHEN COALESCE(ss.released_count, 0)=0 THEN 'unknown'
			WHEN COALESCE(ss.missing_count, 0)=0 THEN 'complete'
			ELSE 'incomplete'
		END,
		s.poster_path, s.backdrop_path
	FROM tv_shows s
	LEFT JOIN show_stats ss ON ss.show_tmdb_id=s.tmdb_id
	LEFT JOIN missing_seasons ms ON ms.show_tmdb_id=s.tmdb_id ` + where + ` ` + order
}

func scanTVShowStatuses(rows pgx.Rows) ([]models.TVShowStatus, error) {
	shows := make([]models.TVShowStatus, 0)
	for rows.Next() {
		var show models.TVShowStatus
		if err := rows.Scan(&show.TMDBID, &show.Name, &show.Original, &show.Year, &show.FirstAirDate, &show.Overview, &show.SeasonCount, &show.EpisodeCount, &show.ReleasedEpisodeCount, &show.UnreleasedEpisodeCount, &show.LocalEpisodeCount, &show.MissingEpisodeCount, &show.MissingSeasonCount, &show.Status, &show.PosterPath, &show.BackdropPath); err != nil {
			return nil, err
		}
		shows = append(shows, show)
	}
	return shows, rows.Err()
}

func tvAvailabilityStatus(releasedCount, missingCount int) string {
	if releasedCount == 0 {
		return "unknown"
	}
	if missingCount == 0 {
		return "complete"
	}
	return "incomplete"
}

func (s *Store) UpsertCollection(ctx context.Context, collection models.CollectionMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO collections (tmdb_id, name, overview, movie_count, unreleased_count, local_count, status, poster_path, backdrop_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tmdb_id) DO UPDATE SET name=$2, overview=$3, movie_count=$4, unreleased_count=$5, local_count=$6, status=$7, poster_path=$8, backdrop_path=$9, updated_at=now()`,
		collection.TMDBID, collection.Name, collection.Overview, collection.MovieCount, collection.UnreleasedCount, collection.LocalCount, collection.Status, collection.PosterPath, collection.BackdropPath)
	return err
}

func (s *Store) UpsertCollectionMovie(ctx context.Context, movie models.CollectionMovieMetadata) error {
	_, err := s.db.Exec(ctx, `INSERT INTO collection_movies (collection_id, movie_tmdb_id, title, release_date, released, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (collection_id, movie_tmdb_id) DO UPDATE SET title=$3, release_date=$4, released=$5, sort_order=$6`,
		movie.CollectionID, movie.MovieTMDBID, movie.Title, movie.ReleaseDate, movie.Released, movie.SortOrder)
	return err
}

func (s *Store) UpsertMediaMatch(ctx context.Context, fileID, targetType string, movieID, showID int, episodeID string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO media_matches (file_id, target_type, movie_tmdb_id, show_tmdb_id, episode_id, confidence)
		VALUES ($1,$2,$3,$4,$5,1)
		ON CONFLICT (file_id) DO UPDATE SET target_type=$2, movie_tmdb_id=$3, show_tmdb_id=$4, episode_id=$5`,
		fileID, targetType, movieID, showID, episodeID)
	return err
}

func (s *Store) CollectionPartIDs(ctx context.Context, collectionID int) ([]int, error) {
	rows, err := s.db.Query(ctx, `SELECT movie_tmdb_id FROM collection_movies WHERE collection_id=$1 AND released=true`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInts(rows)
}

func (s *Store) LocalCollectionMovieIDs(ctx context.Context, collectionID int) ([]int, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT mm.movie_tmdb_id
		FROM media_matches mm
		JOIN media_files mf ON mf.id=mm.file_id
		JOIN collection_movies cm ON cm.movie_tmdb_id=mm.movie_tmdb_id
		WHERE cm.collection_id=$1 AND cm.released=true AND mf.process_status IN ($2,$3)`,
		collectionID, models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInts(rows)
}

func (s *Store) UpdateCollectionStatus(ctx context.Context, collectionID, localCount int, status string) error {
	_, err := s.db.Exec(ctx, `UPDATE collections SET local_count=$2, status=$3, updated_at=now() WHERE tmdb_id=$1`, collectionID, localCount, status)
	return err
}

func (s *Store) Collections(ctx context.Context, search string, limit, offset int) (models.CollectionPage, error) {
	args := []any{}
	where := ""
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+search+"%")
		where = fmt.Sprintf(` WHERE (name ILIKE %[1]s OR tmdb_id::TEXT ILIKE %[1]s OR overview ILIKE %[1]s OR status ILIKE %[1]s)`, fmt.Sprintf("$%d", len(args)))
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::INT FROM collections`+where, args...).Scan(&total); err != nil {
		return models.CollectionPage{}, err
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT tmdb_id, name, overview, movie_count, unreleased_count, local_count, status, poster_path, backdrop_path
		FROM collections%s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return models.CollectionPage{}, err
	}
	defer rows.Close()
	collections := make([]models.CollectionMetadata, 0)
	for rows.Next() {
		var c models.CollectionMetadata
		if err := rows.Scan(&c.TMDBID, &c.Name, &c.Overview, &c.MovieCount, &c.UnreleasedCount, &c.LocalCount, &c.Status, &c.PosterPath, &c.BackdropPath); err != nil {
			return models.CollectionPage{}, err
		}
		collections = append(collections, c)
	}
	if err := rows.Err(); err != nil {
		return models.CollectionPage{}, err
	}
	return models.CollectionPage{Items: collections, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Store) Collection(ctx context.Context, id int) (models.CollectionMetadata, error) {
	var c models.CollectionMetadata
	err := s.db.QueryRow(ctx, `SELECT tmdb_id, name, overview, movie_count, unreleased_count, local_count, status, poster_path, backdrop_path FROM collections WHERE tmdb_id=$1`, id).
		Scan(&c.TMDBID, &c.Name, &c.Overview, &c.MovieCount, &c.UnreleasedCount, &c.LocalCount, &c.Status, &c.PosterPath, &c.BackdropPath)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	parts, err := s.CollectionMovies(ctx, id)
	if err != nil {
		return models.CollectionMetadata{}, err
	}
	c.Parts = parts
	return c, nil
}

func (s *Store) CollectionMovies(ctx context.Context, collectionID int) ([]models.CollectionMovieMetadata, error) {
	rows, err := s.db.Query(ctx, `SELECT cm.collection_id, cm.movie_tmdb_id, cm.title, cm.release_date, cm.released, cm.sort_order,
		COALESCE(local.file_id, ''), COALESCE(local.file_path, ''), COALESCE(local.process_status, '')
		FROM collection_movies cm
		LEFT JOIN LATERAL (
			SELECT mf.id AS file_id, COALESCE(NULLIF(mf.final_path, ''), mf.current_path) AS file_path, mf.process_status
			FROM media_matches mm
			JOIN media_files mf ON mf.id=mm.file_id
			WHERE mm.movie_tmdb_id=cm.movie_tmdb_id AND mf.process_status IN ($2,$3)
			ORDER BY mf.updated_at DESC
			LIMIT 1
		) local ON true
		WHERE cm.collection_id=$1
		ORDER BY cm.sort_order, cm.release_date, cm.movie_tmdb_id`,
		collectionID, models.StatusDone, models.StatusIncompleteCollection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := make([]models.CollectionMovieMetadata, 0)
	for rows.Next() {
		var item models.CollectionMovieMetadata
		if err := rows.Scan(&item.CollectionID, &item.MovieTMDBID, &item.Title, &item.ReleaseDate, &item.Released, &item.SortOrder, &item.FileID, &item.FilePath, &item.ProcessStatus); err != nil {
			return nil, err
		}
		item.Local = item.FileID != ""
		parts = append(parts, item)
	}
	return parts, rows.Err()
}

func (s *Store) CreateTask(ctx context.Context, id, fileID, batchID, templateID, sourcePath, targetPath, status string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO organize_tasks (id, file_id, batch_id, template_id, source_path, target_path, task_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, fileID, batchID, templateID, sourcePath, targetPath, status)
	return err
}

func (s *Store) FinishTask(ctx context.Context, id, status, code, message string) error {
	_, err := s.db.Exec(ctx, `UPDATE organize_tasks SET task_status=$2, error_code=$3, error_message=$4, executed_at=now() WHERE id=$1`, id, status, code, message)
	return err
}

func (s *Store) AddHistory(ctx context.Context, id, taskID, fileID, batchID, sourcePath, targetPath, name, status, code, message string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO operation_histories (id, task_id, file_id, batch_id, source_path, target_path, operation_name, operation_status, error_code, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, taskID, fileID, batchID, sourcePath, targetPath, name, status, code, message)
	return err
}

func (s *Store) AddCollectionCompletionHistory(ctx context.Context, id string, collectionID int, name, sourcePath, targetPath string, movieCount int) error {
	_, err := s.db.Exec(ctx, `INSERT INTO collection_completion_histories (id, collection_id, collection_tmdb_id, collection_name, source_path, target_path, movie_count)
		VALUES ($1,$2,$2,$3,$4,$5,$6)`, id, collectionID, name, sourcePath, targetPath, movieCount)
	return err
}

func (s *Store) UpdateCollectionPathPrefix(ctx context.Context, collectionID int, sourcePrefix, targetPrefix string) error {
	_, err := s.db.Exec(ctx, `UPDATE media_files mf SET
			current_path=replace(current_path, $2, $3),
			final_path=replace(final_path, $2, $3),
			process_status=$4,
			updated_at=now()
		FROM media_matches mm
		JOIN collection_movies cm ON cm.movie_tmdb_id=mm.movie_tmdb_id
		WHERE mf.id=mm.file_id AND cm.collection_id=$1 AND mf.process_status=$5`,
		collectionID, sourcePrefix, targetPrefix, models.StatusDone, models.StatusIncompleteCollection)
	return err
}

func scanBatch(rows pgx.Rows) (models.Batch, error) {
	var batch models.Batch
	var ended sql.NullTime
	err := rows.Scan(&batch.ID, &batch.Source, &batch.Status, &batch.Total, &batch.Done, &batch.Failed, &batch.IncompleteCollection, &batch.StartedAt, &ended)
	if ended.Valid {
		batch.EndedAt = &ended.Time
	}
	return batch, err
}

func scanMediaFile(rows pgx.Rows) (models.MediaFile, error) {
	var file models.MediaFile
	err := rows.Scan(&file.ID, &file.BatchID, &file.OriginalPath, &file.CurrentPath, &file.FinalPath, &file.OriginalName, &file.FinalName,
		&file.Extension, &file.FileSize, &file.FileHash, &file.HashType, &file.MediaType, &file.ProcessStatus, &file.MatchStatus, &file.ParseTitle,
		&file.ParseYear, &file.Season, &file.Episode, &file.Resolution, &file.Source, &file.VideoCodec, &file.AudioCodec, &file.AudioChannels, &file.HDRFormat, &file.PlannedTarget, &file.MoveAttempts,
		&file.LastVerified, &file.ErrorCode, &file.ErrorMessage, &file.CreatedAt, &file.UpdatedAt)
	return file, err
}

func scanSTRMLink(rows pgx.Rows) (models.STRMLink, error) {
	var link models.STRMLink
	var resolved sql.NullTime
	err := rows.Scan(&link.ID, &link.Provider, &link.LibraryCID, &link.LibraryName, &link.LibraryType, &link.RelativePath, &link.RemotePath,
		&link.RemoteFileID, &link.PickCode, &link.SHA1, &link.Size, &link.STRMPath, &link.PlayPath, &link.SourceTreeHash, &link.TreeVersion,
		&link.ResolveStatus, &link.Status, &link.ErrorCode, &link.ErrorMessage, &link.GeneratedAt, &resolved, &link.UpdatedAt)
	if resolved.Valid {
		link.ResolvedAt = &resolved.Time
	}
	return link, err
}

func scanP115SyncRun(rows pgx.Rows) (models.P115SyncRun, error) {
	var run models.P115SyncRun
	var ended sql.NullTime
	err := rows.Scan(&run.ID, &run.Trigger, &run.Status, &run.Mode, &run.TreeVersion, &run.Exported, &run.Generated, &run.Restored,
		&run.Updated, &run.Deleted, &run.Skipped, &run.Failed, &run.ErrorMessage, &run.StartedAt, &ended, &run.DurationMS)
	if ended.Valid {
		run.EndedAt = &ended.Time
	}
	return run, err
}

func scanInts(rows pgx.Rows) ([]int, error) {
	values := make([]int, 0)
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func (s *Store) updateMediaStatus(ctx context.Context, id, status string, previous ...string) error {
	if len(previous) == 0 {
		return fmt.Errorf("非法媒体状态 %s", status)
	}
	tag, err := s.db.Exec(ctx, `UPDATE media_files SET process_status=$2, updated_at=now()
		WHERE id=$1 AND process_status=ANY($3)`, id, status, previous)
	if err != nil {
		return err
	}
	return requireRows(tag.RowsAffected(), "媒体文件 "+id+" 无法流转到 "+status)
}

func previousMediaStatuses(status string) []string {
	switch status {
	case models.StatusScanned:
		return []string{models.StatusIncoming}
	case models.StatusParsed:
		return []string{models.StatusScanned}
	case models.StatusScraped:
		return []string{models.StatusParsed}
	case models.StatusMatched:
		return []string{models.StatusScraped}
	case models.StatusCollectionChecked:
		return []string{models.StatusMatched}
	case models.StatusPlanned:
		return []string{models.StatusCollectionChecked}
	case models.StatusDone, models.StatusIncompleteCollection:
		return []string{models.StatusPlanned}
	default:
		return nil
	}
}

func isTerminalBatchStatus(status string) bool {
	return status == models.BatchStatusCancelled ||
		status == models.BatchStatusComplete ||
		status == models.BatchStatusFailed ||
		status == models.BatchStatusInterrupted
}

func requireRows(rows int64, message string) error {
	if rows == 0 {
		return errors.New(message)
	}
	return nil
}
