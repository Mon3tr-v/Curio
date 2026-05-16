export type DirectoryConfig = {
  incoming_path: string;
  staging_path: string;
  failed_path: string;
  incomplete_collections_path: string;
};

export type SystemSettings = {
  tmdb_api_key: string;
  network_proxy: string;
  classification_yaml: string;
  updated_at: string;
};

export type CloudDriveSettings = {
  address: string;
  username: string;
  password: string;
  token: string;
  root_path: string;
  staging_path: string;
  failed_path: string;
  incomplete_collections_path: string;
  updated_at: string;
};

export type CloudDriveFile = {
  id: string;
  name: string;
  path: string;
  uri: string;
  size: number;
  extension: string;
  is_directory: boolean;
  is_cloud_file: boolean;
  is_local: boolean;
  hash: string;
  hash_type: string;
};

export type CloudDriveStatus = {
  ready: boolean;
  logged_in: boolean;
  user_name: string;
  message: string;
  address: string;
  root_path: string;
  can_browse: boolean;
};

export type P115Settings = {
  enabled: boolean;
  app_id: string;
  app_secret: string;
  cookies: string;
  cookie_login_app: string;
  strm_output_path: string;
  public_base_url: string;
  libraries_yaml: string;
  delete_missing_strm: boolean;
  stale_before_delete: boolean;
  refresh_emby_after_sync: boolean;
  sync_cron_enabled: boolean;
  sync_interval_minutes: number;
  emby_upstream_url: string;
  emby_public_url: string;
  emby_proxy_port: number;
  emby_proxy_base_path: string;
  emby_api_key: string;
  updated_at: string;
};

export type P115Status = {
  ready: boolean;
  message: string;
  user_name: string;
  can_export: boolean;
  can_play: boolean;
};

export type P115QRCodeSession = {
  uid: string;
  qrcode_url: string;
  expires_at: string;
};

export type P115QRCodeStatus = {
  uid: string;
  status: string;
  message: string;
};

export type P115OAuthStart = {
  authorize_url: string;
  redirect_uri: string;
  state: string;
};

export type P115AuthResult = {
  status: string;
  message: string;
};

export type STRMSyncResult = {
  tree_version: string;
  mode?: string;
  exported: number;
  generated: number;
  restored: number;
  updated: number;
  deleted: number;
  skipped: number;
  failed: number;
};

export type P115SyncRun = {
  id: string;
  trigger: string;
  status: string;
  mode: string;
  tree_version: string;
  exported: number;
  generated: number;
  restored: number;
  updated: number;
  deleted: number;
  skipped: number;
  failed: number;
  error_message: string;
  started_at: string;
  ended_at?: string;
  duration_ms: number;
};

export type ClassificationConfig = {
  classification_yaml: string;
};

export type NamingTemplate = {
  template_type: string;
  name: string;
  template: string;
  enabled: boolean;
  updated_at: string;
};

export type Batch = {
  batch_id: string;
  source: string;
  status: string;
  total: number;
  done: number;
  failed: number;
  incomplete_collection: number;
  started_at: string;
  ended_at?: string;
};

export type MediaStats = {
  total: number;
  done: number;
  failed: number;
  incomplete_collection: number;
  missing_tv_season_count: number;
  missing_tv_episode_count: number;
};

export type MediaFile = {
  file_id: string;
  batch_id: string;
  original_path: string;
  current_path: string;
  final_path: string;
  original_file_name: string;
  final_file_name: string;
  extension: string;
  file_size: number;
  file_hash: string;
  hash_type: string;
  media_type: string;
  process_status: string;
  match_status: string;
  parse_title: string;
  parse_year: number;
  season: number;
  episode: number;
  resolution: string;
  source: string;
  video_codec: string;
  audio_codec: string;
  audio_channels: string;
  hdr_format: string;
  planned_target: string;
  move_attempts: number;
  last_verified_path: string;
  error_code: string;
  error_message: string;
  created_at: string;
  updated_at: string;
};

export type MediaFilePage = {
  items: MediaFile[];
  total: number;
  limit: number;
  offset: number;
};

export type RearchivePayload = {
  tmdb_id?: number;
  media_type?: string;
  season?: number;
  episode?: number;
  season_offset?: number;
  episode_offset?: number;
};

export type RearchiveBatchResult = {
  items: MediaFile[];
  count: number;
  failed: number;
  errors: { file_id: string; message: string }[];
};

export type Collection = {
  tmdb_id: number;
  name: string;
  overview: string;
  movie_count: number;
  unreleased_count: number;
  local_count: number;
  status: string;
  poster_path: string;
  backdrop_path: string;
  parts?: CollectionMovie[];
};

export type CollectionPage = {
  items: Collection[];
  total: number;
  limit: number;
  offset: number;
};

export type CollectionMovie = {
  collection_id: number;
  movie_tmdb_id: number;
  title: string;
  release_date: string;
  released: boolean;
  sort_order: number;
  local: boolean;
  file_id: string;
  file_path: string;
  process_status: string;
};

export type TVShow = {
  tmdb_id: number;
  name: string;
  original_name: string;
  year: number;
  first_air_date: string;
  overview: string;
  season_count: number;
  episode_count: number;
  released_episode_count: number;
  unreleased_episode_count: number;
  local_episode_count: number;
  missing_episode_count: number;
  missing_season_count: number;
  status: string;
  poster_path: string;
  backdrop_path: string;
  seasons?: TVSeason[];
};

export type TVShowPage = {
  items: TVShow[];
  total: number;
  limit: number;
  offset: number;
};

export type TVSeason = {
  season: number;
  episode_count: number;
  released_episode_count: number;
  unreleased_episode_count: number;
  local_episode_count: number;
  missing_episode_count: number;
  status: string;
  episodes?: TVEpisode[];
};

export type TVEpisode = {
  id: string;
  show_tmdb_id: number;
  tmdb_id: number;
  season: number;
  episode: number;
  title: string;
  air_date: string;
  released: boolean;
  overview: string;
  runtime: number;
  still_path: string;
  local: boolean;
  file_id: string;
  file_path: string;
  process_status: string;
};

export type Health = {
  status: string;
  database: string;
  redis: string;
  queues: Record<string, number>;
  active_task?: Batch | null;
};

export type AuthStatus = {
  enabled: boolean;
};

export type AuthLoginResult = {
  status: string;
  enabled: boolean;
};
