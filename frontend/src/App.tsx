import { AnimatePresence, motion } from 'framer-motion';
import {
  Activity,
  AlertTriangle,
  Archive,
  ArchiveRestore,
  BadgeCheck,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Cloud,
  CloudCheck,
  CloudCog,
  Copy,
  DatabaseZap,
  FileCode2,
  FileSymlink,
  FolderCheck,
  FolderInput,
  FolderOpen,
  HardDrive,
  HardDriveDownload,
  History,
  Eye,
  EyeOff,
  Import,
  Info,
  KeyRound,
  LayoutDashboard,
  Library,
  LogIn,
  Play,
  PlugZap,
  RefreshCw,
  Router,
  Save,
  ScanSearch,
  ScanQrCode,
  Search,
  Server,
  ServerCog,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Tags,
  Trash2,
  Tv,
  X,
  XCircle,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { KeyboardEvent, ReactNode } from 'react';
import { API_URL, endpoints, getAuthToken, isAuthError, setAuthToken } from './api';
import type {
  Batch,
  CloudDriveFile,
  CloudDriveSettings,
  Collection,
  CollectionPage,
  DirectoryConfig,
  Health,
  MediaFile,
  MediaFilePage,
  MediaStats,
  NamingTemplate,
  P115QRCodeSession,
  P115Settings,
  P115SyncRun,
  RearchivePayload,
  SystemSettings,
  TVShow,
  TVShowPage,
} from './types';

type Page =
  | 'dashboard'
  | 'scan'
  | 'processing'
  | 'staging'
  | 'failed'
  | 'tv'
  | 'collections'
  | 'classification'
  | 'templates'
  | 'settings';

type CloudDriveTextKey = Exclude<keyof CloudDriveSettings, 'updated_at'>;
type P115TextKey = Exclude<
  keyof P115Settings,
  | 'enabled'
  | 'delete_missing_strm'
  | 'emby_proxy_port'
  | 'cookie_login_app'
  | 'stale_before_delete'
  | 'refresh_emby_after_sync'
  | 'sync_cron_enabled'
  | 'sync_interval_minutes'
  | 'updated_at'
>;
type ToastState = { id: number; message: string; tone: 'success' | 'error' | 'info' };

const nav = [
  { id: 'dashboard', label: '总览', title: '总览', icon: LayoutDashboard },
  { id: 'scan', label: '扫描', title: '扫描', icon: Search },
  { id: 'processing', label: '处理', title: '处理', icon: Activity },
  { id: 'staging', label: '完成', title: '完成', icon: FolderCheck },
  { id: 'failed', label: '失败', title: '失败', icon: AlertTriangle },
  { id: 'tv', label: '剧集', title: '剧集', icon: Tv },
  { id: 'collections', label: '合集', title: '合集', icon: Library },
  { id: 'classification', label: '分类', title: '分类', icon: Tags },
  { id: 'templates', label: '命名', title: '命名', icon: FileCode2 },
  { id: 'settings', label: '设置', title: '设置', icon: Settings },
] satisfies { id: Page; label: string; title: string; icon: typeof LayoutDashboard }[];

const pageStorageKey = 'curio.page';
const isPage = (value: string | null): value is Page => Boolean(value && nav.some((item) => item.id === value));

const templateFieldDocs = [
  { field: '{title}', name: '电影标题', description: '电影名称，优先使用简体中文，缺失时回退英文。' },
  { field: '{year}', name: '电影年份', description: '电影上映年份，用于区分同名影片。' },
  { field: '{category}', name: '分类目录', description: '分类 YAML 匹配到的目录名，例如 欧美电影、国产剧集。' },
  { field: '{resolution}', name: '分辨率', description: 'ffprobe 读取到的真实视频分辨率，例如 2160p、1080p。' },
  { field: '{source}', name: '片源类型', description: '从文件名识别出的 BluRay、WEB-DL、UHD、Remux 等来源。' },
  { field: '{video_codec}', name: '视频编码', description: 'ffprobe 读取到的真实视频编码，例如 HEVC、AVC、AV1。' },
  { field: '{audio_codec}', name: '音频编码', description: 'ffprobe 读取到的主音轨编码，例如 TrueHD、DTS-HD MA、DDP。' },
  { field: '{audio_channels}', name: '声道', description: 'ffprobe 读取到的主音轨声道，例如 7.1、5.1、2.0。' },
  { field: '{hdr_format}', name: 'HDR 格式', description: 'ffprobe 读取到的真实 HDR 信息，例如 DV、HDR10+、HDR10、HLG、SDR。' },
  { field: '{extension}', name: '文件扩展名', description: '原始媒体文件扩展名，模板必须包含该字段。' },
  { field: '{show_title}', name: '剧集标题', description: '剧集名称，优先使用简体中文，缺失时回退英文。' },
  { field: '{show_year}', name: '首播年份', description: '剧集首播年份。' },
  { field: '{season}', name: '季号', description: '不补零的季号，例如 1。' },
  { field: '{season_2}', name: '两位季号', description: '补零后的季号，例如 01。' },
  { field: '{episode}', name: '集号', description: '不补零的集号，例如 3。' },
  { field: '{episode_2}', name: '两位集号', description: '补零后的集号，例如 03。' },
  { field: '{episode_title}', name: '分集标题', description: 'TMDB 返回的单集标题。' },
  { field: '{collection_name}', name: '合集名称', description: 'TMDB 合集名称。' },
  { field: '{collection_id}', name: '合集 ID', description: 'TMDB 合集 ID。' },
];
const templateFields = templateFieldDocs.map((item) => item.field);

const templateLabels: Record<string, string> = {
  movie: '电影',
  tv_episode: '剧集',
  collection_movie: '完整合集',
  incomplete_collection_movie: '缺失合集',
};

const emptyDirs: DirectoryConfig = {
  incoming_path: '',
  staging_path: '',
  failed_path: '',
  incomplete_collections_path: '',
};

const emptySettings: SystemSettings = {
  tmdb_api_key: '',
  network_proxy: '',
  classification_yaml: '',
  updated_at: '',
};

const emptyCloudDrive: CloudDriveSettings = {
  address: 'http://localhost:19798',
  username: '',
  password: '',
  token: '',
  root_path: '/',
  staging_path: '/Curio/staging',
  failed_path: '/Curio/failed',
  incomplete_collections_path: '/Curio/incomplete_collections',
  updated_at: '',
};

const emptyP115: P115Settings = {
  enabled: true,
  app_id: '',
  app_secret: '',
  cookies: '',
  cookie_login_app: 'wechatmini',
  strm_output_path: '/data/Curio/strm',
  public_base_url: '',
  libraries_yaml: '',
  delete_missing_strm: true,
  stale_before_delete: false,
  refresh_emby_after_sync: false,
  sync_cron_enabled: false,
  sync_interval_minutes: 60,
  emby_upstream_url: '',
  emby_public_url: '',
  emby_proxy_port: 8097,
  emby_proxy_base_path: '/emby',
  emby_api_key: '',
  updated_at: '',
};

const p115CookieLoginApps = [
  ['wechatmini', '微信小程序'],
  ['alipaymini', '支付宝小程序'],
  ['android', '安卓端'],
  ['ios', '苹果端'],
  ['web', '网页端'],
  ['qandroid', '管理安卓端'],
  ['qios', '管理苹果端'],
] as const;

const emptyStats: MediaStats = {
  total: 0,
  done: 0,
  failed: 0,
  incomplete_collection: 0,
  missing_tv_season_count: 0,
  missing_tv_episode_count: 0,
};

const mediaPageLimit = 50;
const emptyMediaPage: MediaFilePage = { items: [], total: 0, limit: mediaPageLimit, offset: 0 };
const emptyTVShowPage: TVShowPage = { items: [], total: 0, limit: mediaPageLimit, offset: 0 };
const emptyCollectionPage: CollectionPage = { items: [], total: 0, limit: mediaPageLimit, offset: 0 };

function arrayOrEmpty<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function normalizeMediaPage(value: MediaFilePage | null | undefined): MediaFilePage {
  return {
    items: arrayOrEmpty(value?.items),
    total: Number.isFinite(value?.total) ? Number(value?.total) : 0,
    limit: Number.isFinite(value?.limit) && Number(value?.limit) > 0 ? Number(value?.limit) : mediaPageLimit,
    offset: Number.isFinite(value?.offset) && Number(value?.offset) >= 0 ? Number(value?.offset) : 0,
  };
}

function normalizeTVShowPage(value: TVShowPage | null | undefined): TVShowPage {
  return {
    items: arrayOrEmpty(value?.items),
    total: Number.isFinite(value?.total) ? Number(value?.total) : 0,
    limit: Number.isFinite(value?.limit) && Number(value?.limit) > 0 ? Number(value?.limit) : mediaPageLimit,
    offset: Number.isFinite(value?.offset) && Number(value?.offset) >= 0 ? Number(value?.offset) : 0,
  };
}

function normalizeCollectionPage(value: CollectionPage | null | undefined): CollectionPage {
  return {
    items: arrayOrEmpty(value?.items),
    total: Number.isFinite(value?.total) ? Number(value?.total) : 0,
    limit: Number.isFinite(value?.limit) && Number(value?.limit) > 0 ? Number(value?.limit) : mediaPageLimit,
    offset: Number.isFinite(value?.offset) && Number(value?.offset) >= 0 ? Number(value?.offset) : 0,
  };
}

type MediaMode = 'processing' | 'staging' | 'failed';
const processingStatuses = ['incoming', 'scanned', 'parsed', 'scraped', 'matched', 'collection_checked', 'planned'].join(',');
type RearchiveDraft = {
  tmdbID: string;
  mediaType: 'movie' | 'tv_episode';
  season: string;
  episode: string;
  seasonOffset: string;
  episodeOffset: string;
};
type P115TokenDraft = { accessToken: string; refreshToken: string };
type SettingsTab = 'base' | 'cloud' | 'p115' | 'emby';

function p115CIDFromConfig(value: string) {
  const text = value.trim();
  if (!text) return '';
  if (!text.includes('\n') && !text.includes(':')) return text;
  const match = text.match(/^\s*cid\s*:\s*["']?([^"'\s]+)["']?/m);
  return match?.[1] ?? '';
}

export default function App() {
  const [page, setPageState] = useState<Page>(() => {
    if (typeof window === 'undefined') return 'dashboard';
    const saved = window.localStorage.getItem(pageStorageKey);
    return isPage(saved) ? saved : 'dashboard';
  });
  const [health, setHealth] = useState<Health | null>(null);
  const [stats, setStats] = useState<MediaStats>(emptyStats);
  const [activeTask, setActiveTask] = useState<Batch | null>(null);
  const [batches, setBatches] = useState<Batch[]>([]);
  const [directories, setDirectories] = useState<DirectoryConfig>(emptyDirs);
  const [systemSettings, setSystemSettings] = useState<SystemSettings>(emptySettings);
  const [cloudDriveSettings, setCloudDriveSettings] = useState<CloudDriveSettings>(emptyCloudDrive);
  const [p115Settings, setP115Settings] = useState<P115Settings>(emptyP115);
  const [p115SyncRuns, setP115SyncRuns] = useState<P115SyncRun[]>([]);
  const [templates, setTemplates] = useState<NamingTemplate[]>([]);
  const [mediaPage, setMediaPage] = useState<MediaFilePage>(emptyMediaPage);
  const [stagingPage, setStagingPage] = useState<MediaFilePage>(emptyMediaPage);
  const [failedPage, setFailedPage] = useState<MediaFilePage>(emptyMediaPage);
  const [tvShowPage, setTVShowPage] = useState<TVShowPage>(emptyTVShowPage);
  const [collectionPage, setCollectionPage] = useState<CollectionPage>(emptyCollectionPage);
  const [mediaQuery, setMediaQueryState] = useState('');
  const [stagingQuery, setStagingQueryState] = useState('');
  const [failedQuery, setFailedQueryState] = useState('');
  const [tvQuery, setTVQueryState] = useState('');
  const [collectionQuery, setCollectionQueryState] = useState('');
  const [mediaOffset, setMediaOffsetState] = useState(0);
  const [stagingOffset, setStagingOffsetState] = useState(0);
  const [failedOffset, setFailedOffsetState] = useState(0);
  const [tvOffset, setTVOffsetState] = useState(0);
  const [collectionOffset, setCollectionOffsetState] = useState(0);
  const [selectedMedia, setSelectedMedia] = useState<string[]>([]);
  const [selectedStaging, setSelectedStaging] = useState<string[]>([]);
  const [selectedFailed, setSelectedFailed] = useState<string[]>([]);
  const [draftDirs, setDraftDirs] = useState<DirectoryConfig>(emptyDirs);
  const [draftSettings, setDraftSettings] = useState<SystemSettings>(emptySettings);
  const [draftCloudDrive, setDraftCloudDrive] = useState<CloudDriveSettings>(emptyCloudDrive);
  const [draftP115, setDraftP115] = useState<P115Settings>(emptyP115);
  const [p115QRSession, setP115QRSession] = useState<P115QRCodeSession | null>(null);
  const [p115QRStatus, setP115QRStatus] = useState('');
  const [p115OAuthRedirect, setP115OAuthRedirect] = useState('');
  const [p115TokenDraft, setP115TokenDraft] = useState<P115TokenDraft>({ accessToken: '', refreshToken: '' });
  const [draftTemplates, setDraftTemplates] = useState<NamingTemplate[]>([]);
  const [draftClassification, setDraftClassification] = useState('');
  const [cloudDriveFiles, setCloudDriveFiles] = useState<CloudDriveFile[]>([]);
  const [cloudDrivePath, setCloudDrivePath] = useState('/');
  const [preview, setPreview] = useState('');
  const [toast, setToast] = useState<ToastState | null>(null);
  const [rearchiveTargets, setRearchiveTargets] = useState<MediaFile[]>([]);
  const [rearchiveDraft, setRearchiveDraft] = useState<RearchiveDraft>({
    tmdbID: '',
    mediaType: 'movie',
    season: '',
    episode: '',
    seasonOffset: '',
    episodeOffset: '',
  });
  const [busy, setBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [authChecked, setAuthChecked] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [authTokenDraft, setAuthTokenDraft] = useState('');
  const draftsReady = useRef(false);
  const toastSeq = useRef(0);
  const mediaQueryRef = useRef('');
  const stagingQueryRef = useRef('');
  const failedQueryRef = useRef('');
  const tvQueryRef = useRef('');
  const collectionQueryRef = useRef('');
  const mediaOffsetRef = useRef(0);
  const stagingOffsetRef = useRef(0);
  const failedOffsetRef = useRef(0);
  const tvOffsetRef = useRef(0);
  const collectionOffsetRef = useRef(0);
  const pageRef = useRef<Page>(page);
  const refreshTimerRef = useRef<number | null>(null);
  const mediaFiles = mediaPage.items;
  const staging = stagingPage.items;
  const failed = failedPage.items;

  const showToast = useCallback((message: string, tone: ToastState['tone'] = 'info') => {
    toastSeq.current += 1;
    setToast({ id: toastSeq.current, message, tone });
  }, []);

  const handleLoadError = useCallback(
    (error: unknown, fallback = '数据加载失败') => {
      if (isAuthError(error)) {
        setAuthRequired(true);
        return;
      }
      showToast(error instanceof Error ? error.message : fallback, 'error');
    },
    [showToast],
  );

  const setPage = useCallback((next: Page) => {
    pageRef.current = next;
    setPageState(next);
    window.localStorage.setItem(pageStorageKey, next);
  }, []);

  const setMediaQuery = useCallback((value: string) => {
    mediaQueryRef.current = value;
    mediaOffsetRef.current = 0;
    setMediaQueryState(value);
    setMediaOffsetState(0);
    setSelectedMedia([]);
  }, []);

  const setStagingQuery = useCallback((value: string) => {
    stagingQueryRef.current = value;
    stagingOffsetRef.current = 0;
    setStagingQueryState(value);
    setStagingOffsetState(0);
    setSelectedStaging([]);
  }, []);

  const setFailedQuery = useCallback((value: string) => {
    failedQueryRef.current = value;
    failedOffsetRef.current = 0;
    setFailedQueryState(value);
    setFailedOffsetState(0);
    setSelectedFailed([]);
  }, []);

  const setTVQuery = useCallback((value: string) => {
    tvQueryRef.current = value;
    tvOffsetRef.current = 0;
    setTVQueryState(value);
    setTVOffsetState(0);
  }, []);

  const setCollectionQuery = useCallback((value: string) => {
    collectionQueryRef.current = value;
    collectionOffsetRef.current = 0;
    setCollectionQueryState(value);
    setCollectionOffsetState(0);
  }, []);

  const setMediaOffset = useCallback((value: number) => {
    const next = Math.max(0, value);
    mediaOffsetRef.current = next;
    setMediaOffsetState(next);
    setSelectedMedia([]);
  }, []);

  const setStagingOffset = useCallback((value: number) => {
    const next = Math.max(0, value);
    stagingOffsetRef.current = next;
    setStagingOffsetState(next);
    setSelectedStaging([]);
  }, []);

  const setFailedOffset = useCallback((value: number) => {
    const next = Math.max(0, value);
    failedOffsetRef.current = next;
    setFailedOffsetState(next);
    setSelectedFailed([]);
  }, []);

  const setTVOffset = useCallback((value: number) => {
    const next = Math.max(0, value);
    tvOffsetRef.current = next;
    setTVOffsetState(next);
  }, []);

  const setCollectionOffset = useCallback((value: number) => {
    const next = Math.max(0, value);
    collectionOffsetRef.current = next;
    setCollectionOffsetState(next);
  }, []);

  const load = useCallback(async (includeSettings = !draftsReady.current) => {
    const currentPage = pageRef.current;
    const loadMediaPage = currentPage === 'dashboard' || currentPage === 'scan' || currentPage === 'processing';
    const loadStagingPage = currentPage === 'staging';
    const loadFailedPage = currentPage === 'failed';
    const loadTVPage = currentPage === 'tv';
    const loadCollectionPage = currentPage === 'collections';
    const loadSettingsPage = currentPage === 'settings';
    const [
      healthData,
      statsData,
      activeData,
      batchData,
      mediaData,
      stagingData,
      failedData,
      tvShowData,
      collectionData,
      p115RunsData,
    ] = await Promise.all([
      endpoints.health(),
      endpoints.stats(),
      endpoints.activeTask(),
      endpoints.batches(),
      loadMediaPage
        ? endpoints.mediaFiles({
            query: mediaQueryRef.current,
            offset: mediaOffsetRef.current,
            limit: mediaPageLimit,
            status: currentPage === 'processing' ? processingStatuses : undefined,
          })
        : Promise.resolve(null),
      loadStagingPage
        ? endpoints.staging({ query: stagingQueryRef.current, offset: stagingOffsetRef.current, limit: mediaPageLimit })
        : Promise.resolve(null),
      loadFailedPage
        ? endpoints.failed({ query: failedQueryRef.current, offset: failedOffsetRef.current, limit: mediaPageLimit })
        : Promise.resolve(null),
      loadTVPage ? endpoints.tvShows({ query: tvQueryRef.current, offset: tvOffsetRef.current, limit: mediaPageLimit }) : Promise.resolve(null),
      loadCollectionPage
        ? endpoints.collections({ query: collectionQueryRef.current, offset: collectionOffsetRef.current, limit: mediaPageLimit })
        : Promise.resolve(null),
      loadSettingsPage ? endpoints.p115SyncRuns() : Promise.resolve(null),
    ]);
    setHealth(healthData);
    setStats(statsData);
    setActiveTask(activeData ?? healthData.active_task ?? null);
    setBatches(arrayOrEmpty(batchData));
    if (loadMediaPage) setMediaPage(normalizeMediaPage(mediaData));
    if (loadStagingPage) setStagingPage(normalizeMediaPage(stagingData));
    if (loadFailedPage) setFailedPage(normalizeMediaPage(failedData));
    if (loadTVPage) setTVShowPage(normalizeTVShowPage(tvShowData));
    if (loadCollectionPage) setCollectionPage(normalizeCollectionPage(collectionData));
    if (loadSettingsPage) setP115SyncRuns(arrayOrEmpty(p115RunsData));

    if (includeSettings) {
      const [dirData, settingsData, cloudDriveData, p115Data, templateData, classificationData] = await Promise.all([
        endpoints.directories(),
        endpoints.systemSettings(),
        endpoints.cloudDriveSettings(),
        endpoints.p115Settings(),
        endpoints.templates(),
        endpoints.classification(),
      ]);
      setDirectories(dirData);
      setSystemSettings(settingsData);
      setCloudDriveSettings(cloudDriveData);
      setP115Settings(p115Data);
      setTemplates(arrayOrEmpty(templateData));
      if (!draftsReady.current) {
        setDraftDirs(dirData);
        setDraftSettings(settingsData);
        setDraftCloudDrive(cloudDriveData);
        setDraftP115(p115Data);
        setDraftTemplates(arrayOrEmpty(templateData));
        setDraftClassification(classificationData?.classification_yaml ?? '');
        setCloudDrivePath(cloudDriveData.root_path || '/');
        draftsReady.current = true;
      }
    }
  }, []);

  const scheduleLoad = useCallback(
    (includeSettings = false, delay = 180) => {
      if (refreshTimerRef.current) window.clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = window.setTimeout(() => {
        refreshTimerRef.current = null;
        load(includeSettings).catch(() => undefined);
      }, delay);
    },
    [load],
  );

  useEffect(() => {
    let closed = false;
    endpoints
      .authStatus()
      .then((status) => {
        if (closed) return;
        const needsToken = status.enabled && !getAuthToken();
        setAuthRequired(needsToken);
        setAuthChecked(true);
      })
      .catch((error) => {
        if (closed) return;
        setAuthChecked(true);
        handleLoadError(error, '鉴权状态读取失败');
      });
    return () => {
      closed = true;
    };
  }, [handleLoadError]);

  useEffect(() => {
    if (!authChecked || authRequired) return undefined;
    load().catch(handleLoadError);
    if (typeof EventSource === 'undefined') {
      const timer = window.setInterval(() => scheduleLoad(false, 0), 8000);
      return () => {
        window.clearInterval(timer);
        if (refreshTimerRef.current) window.clearTimeout(refreshTimerRef.current);
      };
    }
    const eventURL = new URL(`${API_URL}/api/events`, window.location.origin);
    const token = getAuthToken();
    if (token) eventURL.searchParams.set('token', token);
    const events = new EventSource(eventURL.toString());
    events.onmessage = () => scheduleLoad(false);
    events.onerror = () => undefined;
    return () => {
      events.close();
      if (refreshTimerRef.current) window.clearTimeout(refreshTimerRef.current);
    };
  }, [authChecked, authRequired, handleLoadError, load, scheduleLoad]);

  useEffect(() => {
    if (!authChecked || authRequired) return;
    pageRef.current = page;
    load(false).catch(handleLoadError);
  }, [authChecked, authRequired, page, load, handleLoadError]);

  useEffect(() => {
    if (!authChecked || authRequired) return undefined;
    const timer = window.setTimeout(() => load(false).catch(() => undefined), 260);
    return () => window.clearTimeout(timer);
  }, [authChecked, authRequired, mediaQuery, stagingQuery, failedQuery, tvQuery, collectionQuery, mediaOffset, stagingOffset, failedOffset, tvOffset, collectionOffset, load]);

  useEffect(() => {
    if (!toast) return undefined;
    const timer = window.setTimeout(() => setToast((current) => (current?.id === toast.id ? null : current)), 3200);
    return () => window.clearTimeout(timer);
  }, [toast]);

  const latestBatch = activeTask ?? batches[0];
  const taskProgress = useMemo(() => {
    const total = activeTask?.total ?? 0;
    const done = (activeTask?.done ?? 0) + (activeTask?.failed ?? 0) + (activeTask?.incomplete_collection ?? 0);
    return { total, done, percent: total ? Math.round((done / total) * 100) : 0 };
  }, [activeTask]);

  async function startScan() {
    setBusy(true);
    try {
      const result = await endpoints.startScan();
      showToast(result.status === 'started' ? '本地整理已开始' : result.status, 'success');
      setPage('scan');
      await load();
    } catch (error) {
      showToast(error instanceof Error ? error.message : '启动失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function startCloudDriveScan() {
    setBusy(true);
    try {
      const result = await endpoints.startCloudDriveScan();
      showToast(result.status === 'started' ? '云端整理已开始' : result.status, 'success');
      setPage('scan');
      await load();
    } catch (error) {
      showToast(error instanceof Error ? error.message : '云端整理启动失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function stopActiveTask() {
    if (!activeTask) return;
    setBusy(true);
    try {
      await endpoints.stopTask(activeTask.batch_id);
      showToast('已请求停止任务', 'info');
      await load();
    } catch (error) {
      showToast(error instanceof Error ? error.message : '停止失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function deleteMediaRecords(files: MediaFile[]) {
    const targets = files.filter(Boolean);
    if (targets.length === 0) return;
    const label = targets.length === 1 ? targets[0].original_file_name : `${targets.length} 条记录`;
    const ok = window.confirm(`仅删除数据库记录，不会删除真实文件：${label}`);
    if (!ok) return;
    setBusy(true);
    try {
      if (targets.length === 1) {
        await endpoints.deleteMediaFile(targets[0].file_id);
      } else {
        await endpoints.deleteMediaFiles(targets.map((file) => file.file_id));
      }
      clearSelected();
      showToast(`已删除 ${targets.length} 条记录，源文件未改动`, 'success');
      await load(false);
    } catch (error) {
      showToast(error instanceof Error ? error.message : '记录删除失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  function openRearchive(files: MediaFile[] | MediaFile) {
    const targets = Array.isArray(files) ? files : [files];
    if (targets.length === 0) return;
    setRearchiveTargets(targets);
    setRearchiveDraft({
      tmdbID: '',
      mediaType: targets.some(isTVLike) ? 'tv_episode' : 'movie',
      season: targets.length === 1 && targets[0].season > 0 ? String(targets[0].season) : '',
      episode: targets.length === 1 && targets[0].episode > 0 ? String(targets[0].episode) : '',
      seasonOffset: '',
      episodeOffset: '',
    });
  }

  async function submitRearchive() {
    if (rearchiveTargets.length === 0) return;
    const payload = rearchivePayload(rearchiveDraft);
    if (!payload) {
      showToast('请输入有效的季集或偏移', 'error');
      return;
    }
    setBusy(true);
    try {
      let successCount = 1;
      let failedCount = 0;
      if (rearchiveTargets.length === 1) {
        await endpoints.rearchiveMediaFile(rearchiveTargets[0].file_id, payload);
      } else {
        const result = await endpoints.rearchiveMediaFiles(
          rearchiveTargets.map((file) => file.file_id),
          payload,
        );
        successCount = result.count;
        failedCount = result.failed ?? 0;
      }
      showToast(failedCount > 0 ? `重新归档完成：成功 ${successCount}，失败 ${failedCount}` : `已重新归档 ${successCount} 条记录`, failedCount > 0 ? 'error' : 'success');
      setRearchiveTargets([]);
      clearSelected();
      await load(false);
    } catch (error) {
      showToast(error instanceof Error ? error.message : '重新归档失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  function clearSelected() {
    setSelectedMedia([]);
    setSelectedStaging([]);
    setSelectedFailed([]);
  }

  async function saveDirectories() {
    setBusy(true);
    try {
      const saved = await endpoints.saveDirectories(draftDirs);
      setDirectories(saved);
      setDraftDirs(saved);
      showToast('目录已保存', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '目录保存失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function saveSystemSettings() {
    setBusy(true);
    try {
      const saved = await endpoints.saveSystemSettings(draftSettings);
      setSystemSettings(saved);
      setDraftSettings(saved);
      showToast('TMDB 配置已保存', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'TMDB 配置保存失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function saveClassification() {
    setBusy(true);
    try {
      const saved = await endpoints.saveClassification({ classification_yaml: draftClassification });
      setDraftClassification(saved.classification_yaml);
      showToast('分类规则已保存', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '分类规则保存失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function saveTemplate(template: NamingTemplate) {
    setBusy(true);
    try {
      const saved = await endpoints.saveTemplate(template.template_type, template);
      setDraftTemplates((items) => items.map((item) => (item.template_type === saved.template_type ? saved : item)));
      setTemplates((items) => items.map((item) => (item.template_type === saved.template_type ? saved : item)));
      showToast(`${templateLabels[template.template_type] ?? '命名'}模板已保存`, 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '模板保存失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function showPreview(template: NamingTemplate) {
    try {
      const result = await endpoints.previewTemplate({ template_type: template.template_type, template: template.template });
      setPreview(result.preview);
      showToast('预览已生成', 'success');
    } catch (error) {
      setPreview(error instanceof Error ? error.message : '预览失败');
      showToast(error instanceof Error ? error.message : '预览失败', 'error');
    }
  }

  async function saveCloudDrive() {
    setBusy(true);
    try {
      const saved = await endpoints.saveCloudDriveSettings(draftCloudDrive);
      setCloudDriveSettings(saved);
      setDraftCloudDrive(saved);
      showToast('CloudDrive2 配置已保存', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'CloudDrive2 配置保存失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function testCloudDrive() {
    setBusy(true);
    try {
      const status = await endpoints.testCloudDrive();
      showToast(status.can_browse ? 'CloudDrive2 连接正常' : 'CloudDrive2 已响应', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'CloudDrive2 测试失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function saveP115() {
    setBusy(true);
    try {
      const saved = await endpoints.saveP115Settings(draftP115);
      setP115Settings(saved);
      setDraftP115(saved);
      showToast('115 播放配置已保存', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '115 播放配置保存失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function startP115QRCode() {
    setBusy(true);
    try {
      const saved = await endpoints.saveP115Settings(draftP115);
      setP115Settings(saved);
      setDraftP115(saved);
      const session = await endpoints.startP115QRCode();
      setP115QRSession(session);
      setP115QRStatus('等待扫码');
      showToast('二维码已生成，请使用 115 App 扫码获取 Cookies', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '二维码生成失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function refreshP115QRCodeStatus() {
    if (!p115QRSession) return;
    setBusy(true);
    try {
      const status = await endpoints.p115QRCodeStatus(p115QRSession.uid);
      setP115QRStatus(status.message || status.status || '等待扫码');
      showToast(status.message || '扫码状态已刷新', 'info');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '扫码状态读取失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function completeP115QRCode() {
    if (!p115QRSession) {
      showToast('请先生成二维码', 'error');
      return;
    }
    setBusy(true);
    try {
      const result = await endpoints.completeP115QRCode(p115QRSession.uid);
      const saved = await endpoints.p115Settings();
      setP115Settings(saved);
      setDraftP115(saved);
      setP115QRSession(null);
      setP115QRStatus('');
      showToast(result.message || '115 登录成功', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'Cookies 获取失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function startP115OAuth() {
    setBusy(true);
    try {
      const saved = await endpoints.saveP115Settings(draftP115);
      setP115Settings(saved);
      setDraftP115(saved);
      const result = await endpoints.startP115OAuth();
      setP115OAuthRedirect(result.redirect_uri);
      window.open(result.authorize_url, '_blank', 'noopener,noreferrer');
      showToast('OAuth 授权页已打开', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'OAuth 登录失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function importP115Token(accessToken: string, refreshToken: string) {
    if (!accessToken.trim()) {
      showToast('请填写 Access Token', 'error');
      return;
    }
    if (!refreshToken.trim()) {
      showToast('请填写 Refresh Token', 'error');
      return;
    }
    setBusy(true);
    try {
      const result = await endpoints.importP115Token({
        access_token: accessToken.trim(),
        refresh_token: refreshToken.trim(),
      });
      const saved = await endpoints.p115Settings();
      setP115Settings(saved);
      setDraftP115(saved);
      setP115TokenDraft({ accessToken: '', refreshToken: '' });
      showToast(result.message || 'OpenList Token 已导入', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'OpenList Token 导入失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function refreshP115Token() {
    setBusy(true);
    try {
      const result = await endpoints.refreshP115Token();
      const saved = await endpoints.p115Settings();
      setP115Settings(saved);
      setDraftP115(saved);
      showToast(result.message || '115 令牌已刷新', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '令牌刷新失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function testP115() {
    setBusy(true);
    try {
      const status = await endpoints.testP115();
      showToast(status.ready ? status.message || '115 连接正常' : status.message || '115 未就绪', status.ready ? 'success' : 'error');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '115 测试失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function exportP115Tree() {
    setBusy(true);
    try {
      const result = await endpoints.exportP115Tree();
      showToast(`目录快照已刷新：${result.exported} 项，媒体 ${result.skipped} 个，失败 ${result.failed} 个`, result.failed > 0 ? 'error' : 'success');
      void endpoints.p115SyncRuns().then((runs) => setP115SyncRuns(arrayOrEmpty(runs))).catch(() => undefined);
    } catch (error) {
      showToast(error instanceof Error ? error.message : '目录快照刷新失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function syncP115STRM() {
    setBusy(true);
    try {
      const result = await endpoints.syncP115STRM();
      const source = result.mode === 'snapshot' ? '快照' : '远端';
      showToast(
        `STRM 已同步：来源 ${source}，新增 ${result.generated}，恢复 ${result.restored ?? 0}，更新 ${result.updated}，删除 ${result.deleted}，跳过 ${result.skipped}，失败 ${result.failed}`,
        result.failed > 0 ? 'error' : 'success',
      );
      void endpoints.p115SyncRuns().then((runs) => setP115SyncRuns(arrayOrEmpty(runs))).catch(() => undefined);
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'STRM 同步失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function cleanupP115STRM() {
    setBusy(true);
    try {
      const result = await endpoints.cleanupP115STRM();
      showToast(`孤儿 STRM 已清理：删除 ${result.deleted} 个，失败 ${result.failed} 个`, result.failed > 0 ? 'error' : 'success');
      void endpoints.p115SyncRuns().then((runs) => setP115SyncRuns(arrayOrEmpty(runs))).catch(() => undefined);
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'STRM 清理失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function openCloudDrivePath(path: string) {
    setBusy(true);
    try {
      const files = await endpoints.cloudDriveFiles(path);
      setCloudDriveFiles(arrayOrEmpty(files));
      setCloudDrivePath(path);
      showToast('云端目录已打开', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '云端目录读取失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function loginWithToken() {
    const token = authTokenDraft.trim();
    if (!token) {
      showToast('请输入管理令牌', 'error');
      return;
    }
    setBusy(true);
    try {
      const result = await endpoints.authLogin(token);
      if (result.enabled) setAuthToken(token);
      setAuthRequired(false);
      setAuthTokenDraft('');
      draftsReady.current = false;
      await load(true);
      showToast('登录成功', 'success');
    } catch (error) {
      showToast(error instanceof Error ? error.message : '登录失败', 'error');
    } finally {
      setBusy(false);
    }
  }

  async function refreshData() {
    setRefreshing(true);
    try {
      await load(true);
      showToast('数据已刷新', 'success');
    } catch (error) {
      handleLoadError(error, '刷新失败');
    } finally {
      setRefreshing(false);
    }
  }

  if (!authChecked) {
    return (
      <>
        <AuthScreen loading token={authTokenDraft} setToken={setAuthTokenDraft} onSubmit={loginWithToken} busy={busy} />
        <ToastHost toast={toast} />
      </>
    );
  }

  if (authRequired) {
    return (
      <>
        <AuthScreen token={authTokenDraft} setToken={setAuthTokenDraft} onSubmit={loginWithToken} busy={busy} />
        <ToastHost toast={toast} />
      </>
    );
  }

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <img className="brandIcon" src="/curio-icon.svg" alt="" aria-hidden="true" />
          <span>Curio</span>
        </div>
        <nav>
          {nav.map((item) => {
            const Icon = item.icon;
            return (
              <button
                className={page === item.id ? 'nav active' : 'nav'}
                key={item.id}
                onClick={() => setPage(item.id)}
                title={item.title}
              >
                <Icon size={18} />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>
      </aside>

      <div className="workspace">
        <header className="topbar">
          <div className="taskPulse">
            {activeTask ? (
              <>
                <Activity size={18} />
                <span>
                  {sourceLabel(activeTask.source)} {statusLabel(activeTask.status)} / {taskProgress.done}/{taskProgress.total}
                </span>
              </>
            ) : null}
          </div>
          <div className="actions">
            <button className={refreshing ? 'iconButton refreshing' : 'iconButton'} onClick={refreshData} disabled={refreshing} title="刷新">
              <RefreshCw size={18} />
            </button>
            {activeTask ? (
              <button className="dangerAction" onClick={stopActiveTask} disabled={busy} title="停止任务">
                <XCircle size={17} />
                <span>停止</span>
              </button>
            ) : (
              <>
                <button className="secondaryAction" onClick={startCloudDriveScan} disabled={busy} title="整理云端">
                  <Cloud size={17} />
                  <span>云端</span>
                </button>
                <button className="primary" onClick={startScan} disabled={busy} title="开始整理">
                  <Play size={17} />
                  <span>开始</span>
                </button>
              </>
            )}
          </div>
        </header>

        <AnimatePresence mode="wait">
          <motion.main
            className={`page page-${page}`}
            key={page}
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: 0.22, ease: [0.22, 1, 0.36, 1] }}
          >
            {page === 'dashboard' && (
              <Dashboard stats={stats} batches={batches} health={health} mediaFiles={mediaFiles} activeTask={activeTask} />
            )}
            {page === 'scan' && (
              <Scan
                directories={directories}
                batch={latestBatch}
                activeTask={activeTask}
                page={mediaPage}
                query={mediaQuery}
                setQuery={setMediaQuery}
                offset={mediaOffset}
                setOffset={setMediaOffset}
                selected={selectedMedia}
                setSelected={setSelectedMedia}
                onDelete={deleteMediaRecords}
                onRearchive={openRearchive}
                onStart={startScan}
                onStop={stopActiveTask}
                busy={busy}
              />
            )}
            {page === 'processing' && (
              <Processing
                page={mediaPage}
                query={mediaQuery}
                setQuery={setMediaQuery}
                offset={mediaOffset}
                setOffset={setMediaOffset}
                selected={selectedMedia}
                setSelected={setSelectedMedia}
                onDelete={deleteMediaRecords}
                onRearchive={openRearchive}
                busy={busy}
              />
            )}
            {page === 'staging' && (
              <Staging
                page={stagingPage}
                query={stagingQuery}
                setQuery={setStagingQuery}
                offset={stagingOffset}
                setOffset={setStagingOffset}
                selected={selectedStaging}
                setSelected={setSelectedStaging}
                onDelete={deleteMediaRecords}
                onRearchive={openRearchive}
                busy={busy}
              />
            )}
            {page === 'failed' && (
              <Failed
                page={failedPage}
                query={failedQuery}
                setQuery={setFailedQuery}
                offset={failedOffset}
                setOffset={setFailedOffset}
                selected={selectedFailed}
                setSelected={setSelectedFailed}
                onDelete={deleteMediaRecords}
                onRearchive={openRearchive}
                busy={busy}
              />
            )}
            {page === 'tv' && <TVShows page={tvShowPage} query={tvQuery} setQuery={setTVQuery} offset={tvOffset} setOffset={setTVOffset} />}
            {page === 'collections' && (
              <Collections
                page={collectionPage}
                query={collectionQuery}
                setQuery={setCollectionQuery}
                offset={collectionOffset}
                setOffset={setCollectionOffset}
              />
            )}
            {page === 'classification' && (
              <ClassificationPage value={draftClassification} setValue={setDraftClassification} onSave={saveClassification} busy={busy} />
            )}
            {page === 'templates' && (
              <TemplatesPage
                templates={draftTemplates}
                preview={preview}
                busy={busy}
                setTemplates={setDraftTemplates}
                saveTemplate={saveTemplate}
                showPreview={showPreview}
                showToast={showToast}
              />
            )}
            {page === 'settings' && (
              <SettingsPage
                directories={draftDirs}
                systemSettings={draftSettings}
                cloudDriveSettings={draftCloudDrive}
                p115Settings={draftP115}
                p115QRSession={p115QRSession}
                p115QRStatus={p115QRStatus}
                p115OAuthRedirect={p115OAuthRedirect}
                p115TokenDraft={p115TokenDraft}
                p115SyncRuns={p115SyncRuns}
                cloudDriveFiles={cloudDriveFiles}
                cloudDrivePath={cloudDrivePath}
                busy={busy}
                setDirectories={setDraftDirs}
                setSystemSettings={setDraftSettings}
                setCloudDriveSettings={setDraftCloudDrive}
                setP115Settings={setDraftP115}
                setP115TokenDraft={setP115TokenDraft}
                setCloudDrivePath={setCloudDrivePath}
                saveDirectories={saveDirectories}
                saveSystemSettings={saveSystemSettings}
                saveCloudDrive={saveCloudDrive}
                testCloudDrive={testCloudDrive}
                startCloudDriveScan={startCloudDriveScan}
                openCloudDrivePath={openCloudDrivePath}
                saveP115={saveP115}
                startP115QRCode={startP115QRCode}
                refreshP115QRCodeStatus={refreshP115QRCodeStatus}
                completeP115QRCode={completeP115QRCode}
                startP115OAuth={startP115OAuth}
                importP115Token={importP115Token}
                refreshP115Token={refreshP115Token}
                testP115={testP115}
                exportP115Tree={exportP115Tree}
                syncP115STRM={syncP115STRM}
                cleanupP115STRM={cleanupP115STRM}
              />
            )}
          </motion.main>
        </AnimatePresence>
      </div>
      <RearchiveModal
        files={rearchiveTargets}
        draft={rearchiveDraft}
        busy={busy}
        setDraft={setRearchiveDraft}
        onClose={() => setRearchiveTargets([])}
        onSubmit={submitRearchive}
      />
      <ToastHost toast={toast} />
    </div>
  );
}

function AuthScreen({
  token,
  setToken,
  onSubmit,
  busy,
  loading = false,
}: {
  token: string;
  setToken: (value: string) => void;
  onSubmit: () => void;
  busy: boolean;
  loading?: boolean;
}) {
  return (
    <main className="authShell">
      <motion.section
        className="authCard"
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.22, ease: [0.2, 0, 0, 1] }}
      >
        <div className="brand authBrand">
          <img className="brandIcon" src="/curio-icon.svg" alt="" aria-hidden="true" />
          <span>Curio</span>
        </div>
        {loading ? (
          <div className="authLoading">
            <RefreshCw size={18} className="spinIcon" />
            <span>正在连接</span>
          </div>
        ) : (
          <>
            <label className="field">
              <span>管理令牌</span>
              <input
                value={token}
                type="password"
                autoComplete="current-password"
                spellCheck={false}
                onChange={(event) => setToken(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') onSubmit();
                }}
              />
            </label>
            <button className="primary authSubmit" onClick={onSubmit} disabled={busy} type="button">
              <LogIn size={17} />
              <span>登录</span>
            </button>
          </>
        )}
      </motion.section>
    </main>
  );
}

function ToastHost({ toast }: { toast: ToastState | null }) {
  const Icon = toast?.tone === 'error' ? XCircle : toast?.tone === 'success' ? CheckCircle2 : Activity;
  return (
    <div className="toastStack" aria-live="polite" aria-atomic="true">
      <AnimatePresence>
        {toast && (
          <motion.div
            className={`toast ${toast.tone}`}
            key={toast.id}
            initial={{ opacity: 0, y: -12, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -10, scale: 0.98 }}
            transition={{ duration: 0.2, ease: [0.2, 0, 0, 1] }}
          >
            <Icon size={18} />
            <span>{toast.message}</span>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function RearchiveModal({
  files,
  draft,
  busy,
  setDraft,
  onClose,
  onSubmit,
}: {
  files: MediaFile[];
  draft: RearchiveDraft;
  busy: boolean;
  setDraft: (value: RearchiveDraft) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const open = files.length > 0;
  const title = files.length === 1 ? files[0].original_file_name : `${files.length} 条记录`;
  const update = (patch: Partial<RearchiveDraft>) => setDraft({ ...draft, ...patch });
  return (
    <AnimatePresence>
      {open && (
        <motion.div className="collectionModalOverlay" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <motion.section
            className="collectionModal rearchiveModal"
            initial={{ opacity: 0, y: 18, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 12, scale: 0.98 }}
            transition={{ duration: 0.22, ease: 'easeOut' }}
          >
            <header className="collectionModalHeader">
              <div>
                <span>重新归档</span>
                <h2>{title}</h2>
              </div>
              <button className="iconButton" onClick={onClose} disabled={busy} title="关闭" type="button">
                <X size={18} />
              </button>
            </header>
            <div className="rearchiveBody">
              <div className="segmentedControl">
                <button
                  className={draft.mediaType === 'movie' ? 'active' : ''}
                  onClick={() => update({ mediaType: 'movie' })}
                  type="button"
                >
                  电影
                </button>
                <button
                  className={draft.mediaType === 'tv_episode' ? 'active' : ''}
                  onClick={() => update({ mediaType: 'tv_episode' })}
                  type="button"
                >
                  剧集
                </button>
              </div>
              <label className="field">
                <span>TMDB ID（可空）</span>
                <input
                  value={draft.tmdbID}
                  inputMode="numeric"
                  autoFocus
                  placeholder="留空则按当前文件名重新匹配"
                  onChange={(event) => update({ tmdbID: event.target.value })}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') onSubmit();
                  }}
                />
              </label>
              {draft.mediaType === 'tv_episode' && (
                <div className="rearchiveGrid">
                  <label className="field">
                    <span>季号</span>
                    <input
                      value={draft.season}
                      inputMode="numeric"
                      placeholder={files.length === 1 ? '留空使用识别值' : '批量通常留空'}
                      onChange={(event) => update({ season: event.target.value })}
                    />
                  </label>
                  <label className="field">
                    <span>集号</span>
                    <input
                      value={draft.episode}
                      inputMode="numeric"
                      placeholder={files.length === 1 ? '留空使用识别值' : '批量通常留空'}
                      onChange={(event) => update({ episode: event.target.value })}
                    />
                  </label>
                  <label className="field">
                    <span>季偏移</span>
                    <input
                      value={draft.seasonOffset}
                      inputMode="numeric"
                      placeholder="例如 -1、1"
                      onChange={(event) => update({ seasonOffset: event.target.value })}
                    />
                  </label>
                  <label className="field">
                    <span>集偏移</span>
                    <input
                      value={draft.episodeOffset}
                      inputMode="numeric"
                      placeholder="例如 -1、1"
                      onChange={(event) => update({ episodeOffset: event.target.value })}
                    />
                  </label>
                </div>
              )}
              <p>TMDB ID 留空时按当前文件名重新匹配。删除记录只删除数据库数据，不删除真实源文件。</p>
            </div>
            <div className="settingsActions rearchiveActions">
              <button className="secondaryAction" onClick={onClose} disabled={busy} type="button">
                <X size={17} />
                <span>取消</span>
              </button>
              <button className="primary" onClick={onSubmit} disabled={busy} type="button">
                <ArchiveRestore size={17} />
                <span>归档</span>
              </button>
            </div>
          </motion.section>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function Dashboard({
  stats,
  batches,
  health,
  mediaFiles,
  activeTask,
}: {
  stats: MediaStats;
  batches: Batch[];
  health: Health | null;
  mediaFiles: MediaFile[];
  activeTask: Batch | null;
}) {
  return (
    <>
      <section className="metrics overviewStrip">
        <Metric label="总数" value={stats.total} />
        <Metric label="完成" value={stats.done} />
        <Metric label="失败" value={stats.failed} />
        <Metric label="缺合集" value={stats.incomplete_collection} />
        <Metric label="缺季" value={stats.missing_tv_season_count} />
        <Metric label="缺集" value={stats.missing_tv_episode_count} />
      </section>
      <section className="split dashboardGrid">
        <Block title="最近批次">
          <BatchTable rows={batches.slice(0, 6)} />
        </Block>
        <Block title="运行状态">
          <div className="queueList">
            <div className="queueRow">
              <span>当前任务</span>
              <b>{activeTask ? `${sourceLabel(activeTask.source)} ${statusLabel(activeTask.status)}` : '空闲'}</b>
            </div>
            {Object.entries(health?.queues ?? {}).map(([name, value]) => (
              <div className="queueRow" key={name}>
                <span>{queueLabel(name)}</span>
                <b>{value}</b>
              </div>
            ))}
          </div>
        </Block>
      </section>
      <Block title="最近活动">
        <MediaTable rows={mediaFiles.slice(0, 8)} mode="processing" />
      </Block>
    </>
  );
}

function Scan({
  directories,
  batch,
  activeTask,
  page,
  query,
  setQuery,
  offset,
  setOffset,
  selected,
  setSelected,
  onDelete,
  onRearchive,
  onStart,
  onStop,
  busy,
}: {
  directories: DirectoryConfig;
  batch?: Batch;
  activeTask: Batch | null;
  page: MediaFilePage;
  query: string;
  setQuery: (value: string) => void;
  offset: number;
  setOffset: (value: number) => void;
  selected: string[];
  setSelected: (value: string[]) => void;
  onDelete: (files: MediaFile[]) => void;
  onRearchive: (files: MediaFile[] | MediaFile) => void;
  onStart: () => void;
  onStop: () => void;
  busy: boolean;
}) {
  const total = batch?.total ?? 0;
  const done = (batch?.done ?? 0) + (batch?.failed ?? 0) + (batch?.incomplete_collection ?? 0);
  return (
    <>
      <section className="scanHead scanConsole">
        <div>
          <label>{activeTask?.source === 'cloud' ? '云端根目录' : '入库目录'}</label>
          <strong>{activeTask?.source === 'cloud' ? 'CloudDrive2' : directories.incoming_path}</strong>
        </div>
        {activeTask ? (
          <button className="dangerAction" onClick={onStop} disabled={busy} title="停止任务">
            <XCircle size={17} />
            <span>停止</span>
          </button>
        ) : (
          <button className="primary" onClick={onStart} disabled={busy} title="开始整理">
            <Play size={17} />
            <span>开始</span>
          </button>
        )}
      </section>
      <Block title="任务进度">
        <div className="progressLine">
          <span>{statusLabel(batch?.status ?? 'idle')}</span>
          <b>
            {done}/{total}
          </b>
        </div>
        <div className="progressTrack">
          <motion.div
            className="progressBar"
            animate={{ width: `${total ? Math.round((done / total) * 100) : 0}%` }}
            transition={{ duration: 0.45, ease: 'easeOut' }}
          />
        </div>
      </Block>
      <Block
        title="扫描结果"
        action={<TableSearch value={query} onChange={setQuery} />}
      >
        <MediaList
          page={page}
          mode="processing"
          offset={offset}
          setOffset={setOffset}
          selected={selected}
          setSelected={setSelected}
          onDelete={onDelete}
          onRearchive={onRearchive}
          busy={busy}
        />
      </Block>
    </>
  );
}

type MediaPageProps = {
  page: MediaFilePage;
  query: string;
  setQuery: (value: string) => void;
  offset: number;
  setOffset: (value: number) => void;
  selected: string[];
  setSelected: (value: string[]) => void;
  onDelete: (files: MediaFile[]) => void;
  onRearchive: (files: MediaFile[] | MediaFile) => void;
  busy: boolean;
};

function Processing(props: MediaPageProps) {
  return (
    <Block title="处理文件" action={<TableSearch value={props.query} onChange={props.setQuery} />}>
      <MediaList {...props} mode="processing" />
    </Block>
  );
}

function Staging(props: MediaPageProps) {
  return (
    <>
      <section className="inlineNotice">
        <Archive size={18} />
        <span>已完成重命名的文件可以进入目标媒体库。</span>
      </section>
      <Block title="整理结果" action={<TableSearch value={props.query} onChange={props.setQuery} />}>
        <MediaList {...props} mode="staging" />
      </Block>
    </>
  );
}

function Failed(props: MediaPageProps) {
  return (
    <Block title="失败文件" action={<TableSearch value={props.query} onChange={props.setQuery} />}>
      <MediaList {...props} mode="failed" />
    </Block>
  );
}

function TableSearch({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <label className="tableSearch">
      <Search size={16} />
      <input value={value} placeholder="模糊搜索" onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function TVShows({
  page,
  query,
  setQuery,
  offset,
  setOffset,
}: {
  page: TVShowPage;
  query: string;
  setQuery: (value: string) => void;
  offset: number;
  setOffset: (value: number) => void;
}) {
  const rows = page.items ?? [];
  const [selected, setSelected] = useState<TVShow | null>(null);
  const [detail, setDetail] = useState<TVShow | null>(null);
  const [loading, setLoading] = useState(false);
  const detailRequestRef = useRef(0);

  const openShow = async (item: TVShow) => {
    const requestID = detailRequestRef.current + 1;
    detailRequestRef.current = requestID;
    setSelected(item);
    setDetail(null);
    setLoading(true);
    try {
      const next = await endpoints.tvShow(item.tmdb_id);
      if (detailRequestRef.current === requestID) setDetail(next);
    } catch {
      if (detailRequestRef.current === requestID) setDetail({ ...item, seasons: [] });
    } finally {
      if (detailRequestRef.current === requestID) setLoading(false);
    }
  };

  const active = detail ?? selected;

  return (
    <>
      <Block title="剧集状态" action={<TableSearch value={query} onChange={setQuery} />}>
        <table className="showsTable">
          <thead>
            <tr>
              <th>剧集</th>
              <th>TMDB ID</th>
              <th>已上映</th>
              <th>本地</th>
              <th>缺季</th>
              <th>缺集</th>
              <th>未播</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((item) => (
              <tr
                className="collectionRow"
                key={item.tmdb_id}
                tabIndex={0}
                onClick={() => openShow(item)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') openShow(item);
                }}
              >
                <td>{item.name}</td>
                <td>{item.tmdb_id}</td>
                <td>{item.released_episode_count}</td>
                <td>{item.local_episode_count}</td>
                <td>{item.missing_season_count}</td>
                <td>{item.missing_episode_count}</td>
                <td>{item.unreleased_episode_count}</td>
                <td>
                  <Status value={item.status} />
                </td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td className="emptyCell" colSpan={8}>
                  暂无数据
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <TablePager page={page} offset={offset} setOffset={setOffset} />
      </Block>

      <AnimatePresence>
        {active && (
          <TVShowDetailModal
            show={active}
            loading={loading}
            onClose={() => {
              detailRequestRef.current += 1;
              setSelected(null);
              setDetail(null);
              setLoading(false);
            }}
          />
        )}
      </AnimatePresence>
    </>
  );
}

function TVShowDetailModal({ show, loading, onClose }: { show: TVShow; loading: boolean; onClose: () => void }) {
  const seasons = show.seasons ?? [];
  const hasDetail = seasons.length > 0;

  return (
    <motion.div className="collectionModalOverlay" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
      <motion.section
        className="collectionModal"
        initial={{ opacity: 0, y: 18, scale: 0.98 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 12, scale: 0.98 }}
        transition={{ duration: 0.22, ease: 'easeOut' }}
      >
        <header className="collectionModalHeader">
          <div>
            <span>TMDB {show.tmdb_id}</span>
            <h2>{show.name}</h2>
          </div>
          <button className="iconButton" onClick={onClose} title="关闭" type="button">
            <X size={18} />
          </button>
        </header>

        <div className="collectionSummary tvSummary">
          <div>
            <span>已上映</span>
            <strong>{show.released_episode_count}</strong>
          </div>
          <div>
            <span>本地已有</span>
            <strong>{show.local_episode_count}</strong>
          </div>
          <div>
            <span>缺失季</span>
            <strong>{show.missing_season_count}</strong>
          </div>
          <div>
            <span>缺失集</span>
            <strong>{show.missing_episode_count}</strong>
          </div>
          <div>
            <span>未播</span>
            <strong>{show.unreleased_episode_count}</strong>
          </div>
        </div>

        {loading && <div className="modalLoading">正在读取剧集季集</div>}
        {!loading && !hasDetail && <div className="modalLoading">暂无季集明细</div>}
        {!loading && hasDetail && (
          <div className="collectionPartGroups">
            {seasons.map((season) => (
              <TVSeasonGroup key={season.season} season={season} />
            ))}
          </div>
        )}
      </motion.section>
    </motion.div>
  );
}

function TVSeasonGroup({ season }: { season: NonNullable<TVShow['seasons']>[number] }) {
  const episodes = season.episodes ?? [];
  return (
    <section className="collectionPartGroup">
      <div className="collectionPartGroupTitle">
        <span>第 {season.season} 季</span>
        <b>
          已有 {season.local_episode_count} / 缺失 {season.missing_episode_count} / 未播 {season.unreleased_episode_count}
        </b>
      </div>
      <div className="collectionPartList">
        {episodes.map((episode) => {
          const tone = episode.local ? 'ok' : episode.released ? 'bad' : 'idle';
          return (
            <article className="collectionPartItem" key={episode.id || `${episode.season}-${episode.episode}`}>
              <div>
                <b>
                  S{twoDigit(episode.season)}E{twoDigit(episode.episode)} {episode.title || `第 ${episode.episode} 集`}
                </b>
                <small>
                  TMDB {episode.tmdb_id || '-'}
                  {episode.air_date ? ` · ${episode.air_date}` : ''}
                </small>
                {episode.file_path && <em>{episode.file_path}</em>}
              </div>
              <span className={tone === 'ok' ? 'partState ok' : tone === 'bad' ? 'partState bad' : 'partState'}>
                {tone === 'ok' ? '已有' : tone === 'bad' ? '缺失' : '未播'}
              </span>
            </article>
          );
        })}
        {episodes.length === 0 && <div className="collectionPartEmpty">无</div>}
      </div>
    </section>
  );
}

function Collections({
  page,
  query,
  setQuery,
  offset,
  setOffset,
}: {
  page: CollectionPage;
  query: string;
  setQuery: (value: string) => void;
  offset: number;
  setOffset: (value: number) => void;
}) {
  const rows = page.items ?? [];
  const [selected, setSelected] = useState<Collection | null>(null);
  const [detail, setDetail] = useState<Collection | null>(null);
  const [loading, setLoading] = useState(false);
  const detailRequestRef = useRef(0);

  const openCollection = async (item: Collection) => {
    const requestID = detailRequestRef.current + 1;
    detailRequestRef.current = requestID;
    setSelected(item);
    setDetail(null);
    setLoading(true);
    try {
      const next = await endpoints.collection(item.tmdb_id);
      if (detailRequestRef.current === requestID) setDetail(next);
    } catch {
      if (detailRequestRef.current === requestID) setDetail({ ...item, parts: [] });
    } finally {
      if (detailRequestRef.current === requestID) setLoading(false);
    }
  };

  const active = detail ?? selected;

  return (
    <>
      <Block title="合集状态" action={<TableSearch value={query} onChange={setQuery} />}>
        <table className="collectionsTable">
          <thead>
            <tr>
              <th>合集</th>
              <th>TMDB ID</th>
              <th>已上映</th>
              <th>本地</th>
              <th>缺失</th>
              <th>未上映</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((item) => {
              const missing = Math.max(item.movie_count - item.local_count, 0);
              return (
                <tr
                  className="collectionRow"
                  key={item.tmdb_id}
                  tabIndex={0}
                  onClick={() => openCollection(item)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') openCollection(item);
                  }}
                >
                  <td>{item.name}</td>
                  <td>{item.tmdb_id}</td>
                  <td>{item.movie_count}</td>
                  <td>{item.local_count}</td>
                  <td>{missing}</td>
                  <td>{item.unreleased_count}</td>
                  <td>
                    <Status value={item.status} />
                  </td>
                </tr>
              );
            })}
            {rows.length === 0 && (
              <tr>
                <td className="emptyCell" colSpan={7}>
                  暂无数据
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <TablePager page={page} offset={offset} setOffset={setOffset} />
      </Block>

      <AnimatePresence>
        {active && (
          <CollectionDetailModal
            collection={active}
            loading={loading}
            onClose={() => {
              detailRequestRef.current += 1;
              setSelected(null);
              setDetail(null);
              setLoading(false);
            }}
          />
        )}
      </AnimatePresence>
    </>
  );
}

function CollectionDetailModal({
  collection,
  loading,
  onClose,
}: {
  collection: Collection;
  loading: boolean;
  onClose: () => void;
}) {
  const parts = collection.parts ?? [];
  const local = parts.filter((item) => item.local);
  const missing = parts.filter((item) => item.released && !item.local);
  const unreleased = parts.filter((item) => !item.released);
  const hasDetail = parts.length > 0;

  return (
    <motion.div className="collectionModalOverlay" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
      <motion.section
        className="collectionModal"
        initial={{ opacity: 0, y: 18, scale: 0.98 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 12, scale: 0.98 }}
        transition={{ duration: 0.22, ease: 'easeOut' }}
      >
        <header className="collectionModalHeader">
          <div>
            <span>TMDB {collection.tmdb_id}</span>
            <h2>{collection.name}</h2>
          </div>
          <button className="iconButton" onClick={onClose} title="关闭" type="button">
            <X size={18} />
          </button>
        </header>

        <div className="collectionSummary">
          <div>
            <span>已上映</span>
            <strong>{collection.movie_count}</strong>
          </div>
          <div>
            <span>本地已有</span>
            <strong>{hasDetail ? local.length : collection.local_count}</strong>
          </div>
          <div>
            <span>缺失</span>
            <strong>{hasDetail ? missing.length : Math.max(collection.movie_count - collection.local_count, 0)}</strong>
          </div>
          <div>
            <span>未上映</span>
            <strong>{hasDetail ? unreleased.length : collection.unreleased_count}</strong>
          </div>
        </div>

        {loading && <div className="modalLoading">正在读取合集电影</div>}
        {!loading && !hasDetail && <div className="modalLoading">暂无电影明细</div>}
        {!loading && hasDetail && (
          <div className="collectionPartGroups">
            <CollectionPartGroup title="本地已有" tone="ok" items={local} />
            <CollectionPartGroup title="缺失电影" tone="bad" items={missing} />
            <CollectionPartGroup title="未上映" tone="idle" items={unreleased} />
          </div>
        )}
      </motion.section>
    </motion.div>
  );
}

function CollectionPartGroup({
  title,
  tone,
  items,
}: {
  title: string;
  tone: 'ok' | 'bad' | 'idle';
  items: NonNullable<Collection['parts']>;
}) {
  return (
    <section className="collectionPartGroup">
      <div className="collectionPartGroupTitle">
        <span>{title}</span>
        <b>{items.length}</b>
      </div>
      <div className="collectionPartList">
        {items.map((item) => (
          <article className="collectionPartItem" key={item.movie_tmdb_id}>
            <div>
              <b>{item.title}</b>
              <small>
                TMDB {item.movie_tmdb_id}
                {item.release_date ? ` · ${item.release_date}` : ''}
              </small>
              {item.file_path && <em>{item.file_path}</em>}
            </div>
            <span className={tone === 'ok' ? 'partState ok' : tone === 'bad' ? 'partState bad' : 'partState'}>
              {tone === 'ok' ? '已有' : tone === 'bad' ? '缺失' : '未上映'}
            </span>
          </article>
        ))}
        {items.length === 0 && <div className="collectionPartEmpty">无</div>}
      </div>
    </section>
  );
}

function ClassificationPage({
  value,
  setValue,
  onSave,
  busy,
}: {
  value: string;
  setValue: (value: string) => void;
  onSave: () => void;
  busy: boolean;
}) {
  return (
    <Block title="分类 YAML">
      <div className="editorShell">
        <textarea className="yamlEditor" value={value} spellCheck={false} onChange={(event) => setValue(event.target.value)} />
      </div>
      <button className="primary" onClick={onSave} disabled={busy} title="保存规则">
        <Save size={17} />
        <span>保存规则</span>
      </button>
    </Block>
  );
}

function TemplatesPage({
  templates,
  preview,
  busy,
  setTemplates,
  saveTemplate,
  showPreview,
  showToast,
}: {
  templates: NamingTemplate[];
  preview: string;
  busy: boolean;
  setTemplates: (value: NamingTemplate[]) => void;
  saveTemplate: (template: NamingTemplate) => void;
  showPreview: (template: NamingTemplate) => void;
  showToast: (message: string, tone?: ToastState['tone']) => void;
}) {
  const textareaRefs = useRef<Record<string, HTMLTextAreaElement | null>>({});
  const [activeTemplateType, setActiveTemplateType] = useState(templates[0]?.template_type ?? '');
  const [fieldGuideOpen, setFieldGuideOpen] = useState(false);

  useEffect(() => {
    if (!activeTemplateType && templates[0]) {
      setActiveTemplateType(templates[0].template_type);
    }
  }, [activeTemplateType, templates]);

  const updateTemplate = (templateType: string, value: string) => {
    setTemplates(templates.map((item) => (item.template_type === templateType ? { ...item, template: value } : item)));
  };

  const focusTemplate = (templateType: string, caret: number) => {
    window.requestAnimationFrame(() => {
      const textarea = textareaRefs.current[templateType];
      if (!textarea) return;
      textarea.focus();
      textarea.setSelectionRange(caret, caret);
    });
  };

  const insertField = (field: string) => {
    const templateType = activeTemplateType || templates[0]?.template_type;
    const template = templates.find((item) => item.template_type === templateType);
    if (!template) return;
    const textarea = textareaRefs.current[templateType];
    const start = textarea?.selectionStart ?? template.template.length;
    const end = textarea?.selectionEnd ?? start;
    const value = `${template.template.slice(0, start)}${field}${template.template.slice(end)}`;
    updateTemplate(templateType, value);
    focusTemplate(templateType, start + field.length);
  };

  const deleteWholeField = (event: KeyboardEvent<HTMLTextAreaElement>, template: NamingTemplate) => {
    if (event.key !== 'Backspace' && event.key !== 'Delete') return;
    const textarea = event.currentTarget;
    if (textarea.selectionStart !== textarea.selectionEnd) return;
    const range = fieldDeleteRange(textarea.value, textarea.selectionStart, event.key === 'Backspace' ? 'backward' : 'forward');
    if (!range) return;
    event.preventDefault();
    const value = `${textarea.value.slice(0, range.start)}${textarea.value.slice(range.end)}`;
    updateTemplate(template.template_type, value);
    focusTemplate(template.template_type, range.start);
  };

  const copyField = async (field: string) => {
    try {
      await copyText(field);
      showToast(`${field} 已复制`, 'success');
    } catch {
      showToast('字段复制失败', 'error');
    }
  };

  return (
    <section className="templatePage">
      <Block title="命名模板">
        <div className="templateStack">
          {templates.map((template) => (
            <div className="templateRow" key={template.template_type}>
              <label className="field">
                <span>{templateLabels[template.template_type] ?? template.template_type}</span>
                <textarea
                  ref={(node) => {
                    textareaRefs.current[template.template_type] = node;
                  }}
                  value={template.template}
                  onFocus={() => setActiveTemplateType(template.template_type)}
                  onKeyDown={(event) => deleteWholeField(event, template)}
                  onChange={(event) => updateTemplate(template.template_type, event.target.value)}
                />
              </label>
              <div className="rowActions">
                <button className="iconButton" onClick={() => showPreview(template)} title="生成预览">
                  <SlidersHorizontal size={18} />
                </button>
                <button className="iconButton" onClick={() => saveTemplate(template)} disabled={busy} title="保存模板">
                  <Save size={18} />
                </button>
              </div>
            </div>
          ))}
        </div>
      </Block>
      <Block
        title="可用字段"
        action={
          <button className="iconButton" onClick={() => setFieldGuideOpen(true)} title="字段说明" type="button">
            <Info size={18} />
          </button>
        }
      >
        <div className="tokenList">
          {templateFields.map((field) => (
            <button className="tokenChip" key={field} onClick={() => insertField(field)} title={`插入 ${field}`} type="button">
              {field}
            </button>
          ))}
        </div>
      </Block>
      <Block title="预览">
        <pre>{preview}</pre>
      </Block>
      <FieldGuide open={fieldGuideOpen} onClose={() => setFieldGuideOpen(false)} onCopy={copyField} />
    </section>
  );
}

function FieldGuide({ open, onClose, onCopy }: { open: boolean; onClose: () => void; onCopy: (field: string) => void }) {
  return (
    <AnimatePresence>
      {open && (
        <motion.div
          className="fieldGuideOverlay"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.18, ease: [0.2, 0, 0, 1] }}
          onMouseDown={onClose}
        >
          <motion.section
            className="fieldGuidePanel"
            initial={{ opacity: 0, y: 14, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 10, scale: 0.98 }}
            transition={{ duration: 0.2, ease: [0.2, 0, 0, 1] }}
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="fieldGuideHeader">
              <h2>字段说明</h2>
              <button className="iconButton" onClick={onClose} title="关闭" type="button">
                <X size={18} />
              </button>
            </div>
            <div className="fieldGuideList">
              {templateFieldDocs.map((item) => (
                <button className="fieldGuideItem" key={item.field} onClick={() => onCopy(item.field)} type="button">
                  <span className="fieldGuideToken">{item.field}</span>
                  <span className="fieldGuideText">
                    <b>{item.name}</b>
                    <small>{item.description}</small>
                  </span>
                  <Copy size={17} />
                </button>
              ))}
            </div>
          </motion.section>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

async function copyText(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const textarea = document.createElement('textarea');
  textarea.value = value;
  textarea.setAttribute('readonly', 'true');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  document.body.appendChild(textarea);
  textarea.select();
  const ok = document.execCommand('copy');
  document.body.removeChild(textarea);
  if (!ok) throw new Error('copy failed');
}

function fieldDeleteRange(value: string, caret: number, direction: 'backward' | 'forward') {
  for (const field of templateFields) {
    let start = value.indexOf(field);
    while (start !== -1) {
      const end = start + field.length;
      const shouldDelete = direction === 'backward' ? caret > start && caret <= end : caret >= start && caret < end;
      if (shouldDelete) return { start, end };
      start = value.indexOf(field, end);
    }
  }
  return null;
}

function SecretInput({
  value,
  placeholder,
  visible,
  onChange,
  onToggle,
}: {
  value: string;
  placeholder?: string;
  visible: boolean;
  onChange: (value: string) => void;
  onToggle: () => void;
}) {
  const Icon = visible ? EyeOff : Eye;
  return (
    <div className="secretInput">
      <input
        type={visible ? 'text' : 'password'}
        value={value}
        placeholder={placeholder}
        autoComplete="off"
        spellCheck={false}
        onChange={(event) => onChange(event.target.value)}
      />
      <button className="secretToggle" onClick={onToggle} title={visible ? '隐藏' : '显示'} aria-label={visible ? '隐藏' : '显示'} type="button">
        <Icon size={17} />
      </button>
    </div>
  );
}

function SettingsPage({
  directories,
  systemSettings,
  cloudDriveSettings,
  p115Settings,
  p115QRSession,
  p115QRStatus,
  p115OAuthRedirect,
  p115TokenDraft,
  p115SyncRuns,
  cloudDriveFiles,
  cloudDrivePath,
  busy,
  setDirectories,
  setSystemSettings,
  setCloudDriveSettings,
  setP115Settings,
  setP115TokenDraft,
  setCloudDrivePath,
  saveDirectories,
  saveSystemSettings,
  saveCloudDrive,
  testCloudDrive,
  startCloudDriveScan,
  openCloudDrivePath,
  saveP115,
  startP115QRCode,
  refreshP115QRCodeStatus,
  completeP115QRCode,
  startP115OAuth,
  importP115Token,
  refreshP115Token,
  testP115,
  exportP115Tree,
  syncP115STRM,
  cleanupP115STRM,
}: {
  directories: DirectoryConfig;
  systemSettings: SystemSettings;
  cloudDriveSettings: CloudDriveSettings;
  p115Settings: P115Settings;
  p115QRSession: P115QRCodeSession | null;
  p115QRStatus: string;
  p115OAuthRedirect: string;
  p115TokenDraft: P115TokenDraft;
  p115SyncRuns: P115SyncRun[];
  cloudDriveFiles: CloudDriveFile[];
  cloudDrivePath: string;
  busy: boolean;
  setDirectories: (value: DirectoryConfig) => void;
  setSystemSettings: (value: SystemSettings) => void;
  setCloudDriveSettings: (value: CloudDriveSettings) => void;
  setP115Settings: (value: P115Settings) => void;
  setP115TokenDraft: (value: P115TokenDraft) => void;
  setCloudDrivePath: (value: string) => void;
  saveDirectories: () => void;
  saveSystemSettings: () => void;
  saveCloudDrive: () => void;
  testCloudDrive: () => void;
  startCloudDriveScan: () => void;
  openCloudDrivePath: (path: string) => void;
  saveP115: () => void;
  startP115QRCode: () => void;
  refreshP115QRCodeStatus: () => void;
  completeP115QRCode: () => void;
  startP115OAuth: () => void;
  importP115Token: (accessToken: string, refreshToken: string) => void;
  refreshP115Token: () => void;
  testP115: () => void;
  exportP115Tree: () => void;
  syncP115STRM: () => void;
  cleanupP115STRM: () => void;
}) {
  const [visibleSecrets, setVisibleSecrets] = useState<Record<string, boolean>>({});
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('base');
  const secretVisible = (key: string) => Boolean(visibleSecrets[key]);
  const toggleSecret = (key: string) => setVisibleSecrets((current) => ({ ...current, [key]: !current[key] }));
  const dirFields: [keyof DirectoryConfig, string][] = [
    ['incoming_path', '入库目录'],
    ['staging_path', '整理目录'],
    ['failed_path', '失败目录'],
    ['incomplete_collections_path', '缺失合集目录'],
  ];
  const cloudFields: [CloudDriveTextKey, string, string][] = [
    ['address', '服务地址', 'http://host.docker.internal:19798'],
    ['username', '用户名', ''],
    ['password', '密码', ''],
    ['token', '访问令牌', ''],
    ['root_path', '扫描根目录', '/'],
    ['staging_path', '整理目录', '/Curio/staging'],
    ['failed_path', '失败目录', '/Curio/failed'],
    ['incomplete_collections_path', '缺失合集目录', '/Curio/incomplete_collections'],
  ];
  const p115SecretFields = new Set<P115TextKey>(['app_secret', 'cookies', 'emby_api_key']);
  const p115Fields: [P115TextKey, string, string][] = [
    ['app_id', 'App ID', ''],
    ['app_secret', 'App Secret', ''],
    ['cookies', 'Cookies', 'UID=...; CID=...; SEID=...'],
    ['strm_output_path', 'STRM 输出目录', '/data/Curio/strm'],
    ['public_base_url', 'Curio 外部地址', 'http://curio.example.com'],
  ];
  const embyFields: [P115TextKey, string, string][] = [
    ['emby_upstream_url', 'Emby 原始地址', 'http://emby:8096'],
    ['emby_public_url', 'Emby 对外地址', 'http://curio.example.com:8097'],
    ['emby_api_key', 'API Key', ''],
  ];
  const p115CID = p115CIDFromConfig(p115Settings.libraries_yaml);
  const settingsTabs = [
    { id: 'base', label: '基础', summary: '目录、TMDB、网络', icon: HardDrive },
    { id: 'cloud', label: '云端', summary: 'CloudDrive2 与浏览', icon: CloudCog },
    { id: 'p115', label: '115', summary: 'STRM、授权、CID', icon: ShieldCheck },
    { id: 'emby', label: 'Emby', summary: '反代与媒体服务器', icon: ServerCog },
  ] satisfies { id: SettingsTab; label: string; summary: string; icon: typeof Settings }[];

  return (
    <section className="settingsPage">
      <div className="settingsTabBar" role="tablist" aria-label="设置分类">
        {settingsTabs.map((item) => {
          const Icon = item.icon;
          return (
            <button
              className={`settingsTabButton ${settingsTab === item.id ? 'active' : ''}`}
              key={item.id}
              onClick={() => setSettingsTab(item.id)}
              role="tab"
              aria-selected={settingsTab === item.id}
              type="button"
            >
              <Icon size={17} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </div>

      <div className="settingsContent">
        <div className="settingsStack">

      {settingsTab === 'base' && (
        <motion.div className="settingsTabPanel" key="base" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Block title="本地目录">
        <div className="formGrid settingsFormGrid">
          {dirFields.map(([key, label]) => (
            <label className="field" key={key}>
              <span>{label}</span>
              <input value={directories[key]} onChange={(event) => setDirectories({ ...directories, [key]: event.target.value })} />
            </label>
          ))}
        </div>
        <div className="settingsActions">
          <button className="primary" onClick={saveDirectories} disabled={busy} title="保存目录">
            <FolderInput size={17} />
            <span>保存目录</span>
          </button>
        </div>
      </Block>

      <Block title="TMDB 与网络">
        <div className="formGrid settingsFormGrid">
          <label className="field">
            <span>TMDB API 密钥</span>
            <SecretInput
              value={systemSettings.tmdb_api_key}
              visible={secretVisible('tmdb_api_key')}
              onToggle={() => toggleSecret('tmdb_api_key')}
              onChange={(value) => setSystemSettings({ ...systemSettings, tmdb_api_key: value })}
            />
          </label>
          <label className="field">
            <span>网络代理</span>
            <input
              value={systemSettings.network_proxy}
              placeholder="http://127.0.0.1:7890"
              onChange={(event) => setSystemSettings({ ...systemSettings, network_proxy: event.target.value })}
            />
          </label>
        </div>
        <div className="settingsActions">
          <button className="primary" onClick={saveSystemSettings} disabled={busy} title="保存 TMDB 配置">
            <DatabaseZap size={17} />
            <span>保存 TMDB</span>
          </button>
        </div>
      </Block>
        </motion.div>
      )}

      {settingsTab === 'cloud' && (
        <motion.div className="settingsTabPanel" key="cloud" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Block title="CloudDrive2">
        <div className="formGrid settingsFormGrid">
          {cloudFields.map(([key, label, placeholder]) => (
            <label className="field" key={key}>
              <span>{label}</span>
              {key === 'password' || key === 'token' ? (
                <SecretInput
                  value={String(cloudDriveSettings[key] ?? '')}
                  placeholder={placeholder}
                  visible={secretVisible(`cloud_${key}`)}
                  onToggle={() => toggleSecret(`cloud_${key}`)}
                  onChange={(value) => setCloudDriveSettings({ ...cloudDriveSettings, [key]: value })}
                />
              ) : (
                <input
                  value={String(cloudDriveSettings[key] ?? '')}
                  placeholder={placeholder}
                  onChange={(event) => setCloudDriveSettings({ ...cloudDriveSettings, [key]: event.target.value })}
                />
              )}
            </label>
          ))}
        </div>
        <div className="settingsActions">
          <button className="primary" onClick={saveCloudDrive} disabled={busy} title="保存 CloudDrive2">
            <CloudCheck size={17} />
            <span>保存云端</span>
          </button>
          <button className="secondaryAction" onClick={testCloudDrive} disabled={busy} title="测试 CloudDrive2">
            <PlugZap size={17} />
            <span>测试</span>
          </button>
          <button className="secondaryAction" onClick={startCloudDriveScan} disabled={busy} title="整理云端">
            <HardDriveDownload size={17} />
            <span>整理云端</span>
          </button>
        </div>
      </Block>
        </motion.div>
      )}

      {settingsTab === 'p115' && (
        <motion.div className="settingsTabPanel" key="p115" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Block title="115 基础">
        <div className="formGrid settingsFormGrid">
          {p115Fields.map(([key, label, placeholder]) => (
            <label className={key === 'cookies' ? 'field wideField' : 'field'} key={key}>
              <span>{label}</span>
              {p115SecretFields.has(key) ? (
                <SecretInput
                  value={String(p115Settings[key] ?? '')}
                  placeholder={placeholder}
                  visible={secretVisible(`p115_${key}`)}
                  onToggle={() => toggleSecret(`p115_${key}`)}
                  onChange={(value) => setP115Settings({ ...p115Settings, [key]: value })}
                />
              ) : (
                <input
                  value={String(p115Settings[key] ?? '')}
                  placeholder={placeholder}
                  onChange={(event) => setP115Settings({ ...p115Settings, [key]: event.target.value })}
                />
              )}
            </label>
          ))}
          <label className="field">
            <span>媒体库 CID</span>
            <input
              value={p115CID}
              placeholder="3428557242282467406"
              onChange={(event) => setP115Settings({ ...p115Settings, libraries_yaml: event.target.value })}
            />
          </label>
          <label className="field">
            <span>扫码设备</span>
            <select
              value={p115Settings.cookie_login_app || 'wechatmini'}
              onChange={(event) => setP115Settings({ ...p115Settings, cookie_login_app: event.target.value })}
            >
              {p115CookieLoginApps.map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          <div className="settingsCheckGroup">
            <label className="checkLine">
              <input
                type="checkbox"
                checked={p115Settings.delete_missing_strm}
                onChange={(event) => setP115Settings({ ...p115Settings, delete_missing_strm: event.target.checked })}
              />
              <span>删除缺失 STRM</span>
            </label>
            <label className="checkLine">
              <input
                type="checkbox"
                checked={p115Settings.stale_before_delete}
                onChange={(event) => setP115Settings({ ...p115Settings, stale_before_delete: event.target.checked })}
              />
              <span>先标记失效</span>
            </label>
            <label className="checkLine">
              <input
                type="checkbox"
                checked={p115Settings.refresh_emby_after_sync}
                onChange={(event) => setP115Settings({ ...p115Settings, refresh_emby_after_sync: event.target.checked })}
              />
              <span>同步后刷新 Emby</span>
            </label>
          </div>
        </div>
        <div className="settingsActions">
          <button className="primary" onClick={saveP115} disabled={busy} title="保存 115 播放">
            <KeyRound size={17} />
            <span>保存 115</span>
          </button>
          <button className="secondaryAction" onClick={testP115} disabled={busy} title="测试 115">
            <Router size={17} />
            <span>测试连接</span>
          </button>
        </div>
      </Block>

      <Block title="登录授权">
        <div className="authPanel">
          <div className="authActions">
            <button className="secondaryAction" onClick={startP115QRCode} disabled={busy} title="扫码获取 115 Cookies">
              <ScanQrCode size={17} />
              <span>获取 Cookies</span>
            </button>
            <button className="secondaryAction" onClick={startP115OAuth} disabled={busy} title="打开 OAuth 授权页">
              <LogIn size={17} />
              <span>OAuth 登录</span>
            </button>
            <button className="secondaryAction" onClick={refreshP115Token} disabled={busy} title="刷新 115 Open Token">
              <BadgeCheck size={17} />
              <span>刷新令牌</span>
            </button>
          </div>
          <div className="tokenImportGrid">
            <label className="field">
              <span>OpenList Access Token</span>
              <SecretInput
                value={p115TokenDraft.accessToken}
                placeholder="access_token"
                visible={secretVisible('p115_openlist_access')}
                onToggle={() => toggleSecret('p115_openlist_access')}
                onChange={(value) => setP115TokenDraft({ ...p115TokenDraft, accessToken: value })}
              />
            </label>
            <label className="field">
              <span>OpenList Refresh Token</span>
              <SecretInput
                value={p115TokenDraft.refreshToken}
                placeholder="refresh_token"
                visible={secretVisible('p115_openlist_refresh')}
                onToggle={() => toggleSecret('p115_openlist_refresh')}
                onChange={(value) => setP115TokenDraft({ ...p115TokenDraft, refreshToken: value })}
              />
            </label>
            <button
              className="primary"
              onClick={() => importP115Token(p115TokenDraft.accessToken, p115TokenDraft.refreshToken)}
              disabled={busy}
              title="导入 OpenList Token"
            >
              <Import size={17} />
              <span>导入 Token</span>
            </button>
          </div>
          {p115QRSession && (
            <div className="qrPanel">
              <img src={p115QRSession.qrcode_url} alt="115 Cookies 登录二维码" />
              <div>
                <strong>{p115QRStatus || '等待扫码'}</strong>
                <span>{new Date(p115QRSession.expires_at).toLocaleString()}</span>
                <div className="authActions compactActions">
                  <button className="secondaryAction" onClick={refreshP115QRCodeStatus} disabled={busy} title="刷新扫码状态">
                    <RefreshCw size={17} />
                    <span>刷新状态</span>
                  </button>
                  <button className="primary" onClick={completeP115QRCode} disabled={busy} title="保存扫码返回的 Cookies">
                    <CheckCircle2 size={17} />
                    <span>保存 Cookies</span>
                  </button>
                </div>
              </div>
            </div>
          )}
          {p115OAuthRedirect && <div className="inlineHint">OAuth 回调地址：{p115OAuthRedirect}</div>}
        </div>
      </Block>

      <Block title="STRM 操作">
        <div className="formGrid settingsFormGrid scheduleGrid">
          <label className="checkLine">
            <input
              type="checkbox"
              checked={p115Settings.sync_cron_enabled}
              onChange={(event) => setP115Settings({ ...p115Settings, sync_cron_enabled: event.target.checked })}
            />
            <span>定时增量同步</span>
          </label>
          <label className="field">
            <span>同步间隔（分钟）</span>
            <input
              type="number"
              min="5"
              max="10080"
              value={p115Settings.sync_interval_minutes || 60}
              onChange={(event) =>
                setP115Settings({ ...p115Settings, sync_interval_minutes: Number.parseInt(event.target.value, 10) || 60 })
              }
            />
          </label>
        </div>
        <div className="settingsActions">
          <button className="primary" onClick={saveP115} disabled={busy} title="保存 STRM 设置">
            <Clock3 size={17} />
            <span>保存定时</span>
          </button>
          <button className="secondaryAction" onClick={exportP115Tree} disabled={busy} title="刷新 115 目录快照">
            <FolderOpen size={17} />
            <span>刷新快照</span>
          </button>
          <button className="secondaryAction" onClick={syncP115STRM} disabled={busy} title="同步 STRM">
            <FileSymlink size={17} />
            <span>同步 STRM</span>
          </button>
          <button className="secondaryAction" onClick={cleanupP115STRM} disabled={busy} title="清理孤儿 STRM">
            <Trash2 size={17} />
            <span>清理孤儿</span>
          </button>
        </div>
        <div className="syncHistoryHeader">
          <History size={17} />
          <span>同步记录</span>
        </div>
        <P115SyncRunTable rows={p115SyncRuns} />
      </Block>
        </motion.div>
      )}

      {settingsTab === 'emby' && (
        <motion.div className="settingsTabPanel" key="emby" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Block title="Emby 反代">
        <div className="formGrid settingsFormGrid">
          {embyFields.map(([key, label, placeholder]) => (
            <label className="field" key={key}>
              <span>{label}</span>
              {p115SecretFields.has(key) ? (
                <SecretInput
                  value={String(p115Settings[key] ?? '')}
                  placeholder={placeholder}
                  visible={secretVisible(`p115_${key}`)}
                  onToggle={() => toggleSecret(`p115_${key}`)}
                  onChange={(value) => setP115Settings({ ...p115Settings, [key]: value })}
                />
              ) : (
                <input
                  value={String(p115Settings[key] ?? '')}
                  placeholder={placeholder}
                  onChange={(event) => setP115Settings({ ...p115Settings, [key]: event.target.value })}
                />
              )}
            </label>
          ))}
          <label className="field">
            <span>反代端口</span>
            <input
              type="number"
              min="1"
              max="65535"
              value={p115Settings.emby_proxy_port || 8097}
              placeholder="8097"
              onChange={(event) =>
                setP115Settings({ ...p115Settings, emby_proxy_port: Number.parseInt(event.target.value, 10) || 0 })
              }
            />
          </label>
        </div>
        <div className="settingsActions">
          <button className="primary" onClick={saveP115} disabled={busy} title="保存 Emby 反代">
            <Server size={17} />
            <span>保存反代</span>
          </button>
        </div>
      </Block>
        </motion.div>
      )}

      {settingsTab === 'cloud' && (
        <motion.div className="settingsTabPanel" key="cloud-browser" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Block title="云端浏览">
        <div className="browserBar">
          <input value={cloudDrivePath} onChange={(event) => setCloudDrivePath(event.target.value)} />
          <button className="primary" onClick={() => openCloudDrivePath(cloudDrivePath)} disabled={busy} title="打开目录">
            <ScanSearch size={17} />
            <span>打开</span>
          </button>
        </div>
        <div className="tableFrame">
          <CloudDriveTable rows={cloudDriveFiles} onOpen={openCloudDrivePath} />
        </div>
      </Block>
        </motion.div>
      )}
        </div>
      </div>
    </section>
  );
}

function CloudDriveTable({ rows, onOpen }: { rows: CloudDriveFile[]; onOpen: (path: string) => void }) {
  return (
    <table>
      <thead>
        <tr>
          <th>名称</th>
          <th>类型</th>
          <th>大小</th>
          <th>路径</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((file) => (
          <tr className={file.is_directory ? 'clickableRow' : ''} key={file.path} onClick={() => file.is_directory && onOpen(file.path)}>
            <td>{file.name}</td>
            <td>{file.is_directory ? '目录' : '文件'}</td>
            <td>{file.is_directory ? '' : formatSize(file.size)}</td>
            <td title={file.path}>{shortPath(file.path)}</td>
          </tr>
        ))}
        {rows.length === 0 && (
          <tr>
            <td className="emptyCell" colSpan={4}>
              暂无数据
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}

function P115SyncRunTable({ rows }: { rows: P115SyncRun[] }) {
  return (
    <div className="tableFrame syncRunTable">
      <table>
        <thead>
          <tr>
            <th>时间</th>
            <th>来源</th>
            <th>状态</th>
            <th>模式</th>
            <th>结果</th>
            <th>耗时</th>
            <th>错误</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((run) => (
            <tr key={run.id}>
              <td>{formatFullDate(run.started_at)}</td>
              <td>{p115TriggerLabel(run.trigger)}</td>
              <td>
                <span className={`syncStatus ${run.status}`}>{p115SyncStatusLabel(run.status)}</span>
              </td>
              <td>{p115ModeLabel(run.mode)}</td>
              <td>{`新增 ${run.generated} / 更新 ${run.updated} / 删除 ${run.deleted} / 失败 ${run.failed}`}</td>
              <td>{formatDuration(run.duration_ms)}</td>
              <td title={run.error_message}>{run.error_message ? shortText(run.error_message, 28) : '-'}</td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td className="emptyCell" colSpan={7}>
                暂无同步记录
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

function Block({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="block">
      <div className="blockTitle">
        <h2>{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function BatchTable({ rows }: { rows: Batch[] }) {
  return (
    <table>
      <thead>
        <tr>
          <th>批次</th>
          <th>来源</th>
          <th>状态</th>
          <th>总数</th>
          <th>完成</th>
          <th>失败</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.batch_id}>
            <td>{row.batch_id.slice(0, 8)}</td>
            <td>{sourceLabel(row.source)}</td>
            <td>
              <Status value={row.status} />
            </td>
            <td>{row.total}</td>
            <td>{row.done}</td>
            <td>{row.failed}</td>
          </tr>
        ))}
        {rows.length === 0 && (
          <tr>
            <td className="emptyCell" colSpan={6}>
              暂无数据
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}

function MediaList({
  page,
  mode,
  offset,
  setOffset,
  selected,
  setSelected,
  onDelete,
  onRearchive,
  busy,
}: {
  page: MediaFilePage;
  mode: MediaMode;
  offset: number;
  setOffset: (value: number) => void;
  selected: string[];
  setSelected: (value: string[]) => void;
  onDelete: (files: MediaFile[]) => void;
  onRearchive: (files: MediaFile[] | MediaFile) => void;
  busy: boolean;
}) {
  const [detail, setDetail] = useState<MediaFile | null>(null);
  const rows = page.items;
  const ids = rows.map((file) => file.file_id);
  const selectedSet = new Set(selected);
  const selectedFiles = rows.filter((file) => selectedSet.has(file.file_id));
  const allSelected = rows.length > 0 && ids.every((id) => selectedSet.has(id));
  const toggleAll = () => {
    if (allSelected) {
      setSelected(selected.filter((id) => !ids.includes(id)));
      return;
    }
    setSelected(Array.from(new Set([...selected, ...ids])));
  };
  const toggleOne = (id: string) => {
    setSelected(selectedSet.has(id) ? selected.filter((item) => item !== id) : [...selected, id]);
  };
  return (
    <>
      <div className="mediaBulkBar">
        <span>
          已选 {selectedFiles.length} / 当前页 {rows.length} / 共 {page.total}
        </span>
        <div className="rowActions">
          <button className="secondaryAction" onClick={() => onRearchive(selectedFiles)} disabled={busy || selectedFiles.length === 0} type="button">
            <ArchiveRestore size={17} />
            <span>批量归档</span>
          </button>
          <button className="dangerAction" onClick={() => onDelete(selectedFiles)} disabled={busy || selectedFiles.length === 0} type="button">
            <Trash2 size={17} />
            <span>批量删除</span>
          </button>
        </div>
      </div>
      <MediaTable
        rows={rows}
        mode={mode}
        selected={selected}
        allSelected={allSelected}
        onToggle={toggleOne}
        onToggleAll={toggleAll}
        onOpen={setDetail}
        onDelete={(file) => onDelete([file])}
        onRearchive={onRearchive}
        busy={busy}
      />
      <TablePager page={page} offset={offset} setOffset={setOffset} />
      <MediaDetailModal
        file={detail}
        onClose={() => setDetail(null)}
        onDelete={(file) => {
          setDetail(null);
          onDelete([file]);
        }}
        onRearchive={(file) => {
          setDetail(null);
          onRearchive(file);
        }}
        busy={busy}
      />
    </>
  );
}

function MediaTable({
  rows,
  mode,
  selected = [],
  allSelected = false,
  onToggle,
  onToggleAll,
  onOpen,
  onDelete,
  onRearchive,
  busy = false,
}: {
  rows: MediaFile[];
  mode: MediaMode;
  selected?: string[];
  allSelected?: boolean;
  onToggle?: (id: string) => void;
  onToggleAll?: () => void;
  onOpen?: (file: MediaFile) => void;
  onDelete?: (file: MediaFile) => void;
  onRearchive?: (file: MediaFile) => void;
  busy?: boolean;
}) {
  const baseColumns: { key: string; label: string; width: string }[] =
    mode === 'staging'
      ? [
          { key: 'media', label: '媒体', width: '30%' },
          { key: 'tech', label: '参数', width: '20%' },
          { key: 'path', label: '路径', width: '34%' },
          { key: 'time', label: '时间', width: '124px' },
        ]
      : mode === 'failed'
        ? [
            { key: 'media', label: '媒体', width: '28%' },
            { key: 'error', label: '错误', width: '23%' },
            { key: 'path', label: '路径', width: '35%' },
            { key: 'time', label: '时间', width: '124px' },
          ]
        : [
            { key: 'media', label: '媒体', width: '27%' },
            { key: 'status', label: '状态', width: '150px' },
            { key: 'tech', label: '参数', width: '20%' },
            { key: 'path', label: '路径', width: '30%' },
            { key: 'time', label: '时间', width: '124px' },
          ];
  const hasActions = Boolean(onDelete || onRearchive);
  const columns = [
    ...(hasActions ? [{ key: 'select', label: '选择', width: '54px' }] : []),
    ...baseColumns,
    ...(hasActions ? [{ key: 'actions', label: '操作', width: '116px' }] : []),
  ];
  return (
    <table className={`mediaTable mediaTable-${mode}`}>
      <colgroup>
        {columns.map((column) => (
          <col key={column.key} style={{ width: column.width }} />
        ))}
      </colgroup>
      <thead>
        <tr>
          {columns.map((column) =>
            column.key === 'select' ? (
              <th key={column.key} className="selectCol">
                <input
                  type="checkbox"
                  checked={allSelected}
                  onClick={(event) => event.stopPropagation()}
                  onChange={onToggleAll}
                  aria-label="选择当前页"
                />
              </th>
            ) : (
              <th key={column.key}>{column.label}</th>
            ),
          )}
        </tr>
      </thead>
      <tbody>
        {rows.map((file) => (
          <tr className={onOpen ? 'clickableRow' : ''} key={file.file_id} onClick={() => onOpen?.(file)} title="点击查看详情">
            {hasActions && (
              <td className="selectCol">
                <input
                  type="checkbox"
                  checked={selected.includes(file.file_id)}
                  onClick={(event) => event.stopPropagation()}
                  onChange={() => onToggle?.(file.file_id)}
                  aria-label={`选择 ${file.original_file_name}`}
                />
              </td>
            )}
            {mode === 'staging' ? (
              <>
                <MediaNameCell file={file} name={file.final_file_name || file.original_file_name} />
                <td>{techSummary(file) || '未知'}</td>
                <PathCell value={file.final_path} />
                <td>{formatDate(file.updated_at)}</td>
                {hasActions && (
                  <td>
                    <MediaRowActions file={file} busy={busy} onDelete={onDelete} onRearchive={onRearchive} />
                  </td>
                )}
              </>
            ) : mode === 'failed' ? (
              <>
                <MediaNameCell file={file} name={file.original_file_name} />
                <td>
                  <div className="mediaCell">
                    <b>{errorCodeLabel(file.error_code) || '未知错误'}</b>
                    <small title={file.error_message}>{file.error_message || '-'}</small>
                  </div>
                </td>
                <PathCell value={file.final_path || file.current_path} />
                <td>{formatDate(file.updated_at)}</td>
                {hasActions && (
                  <td>
                    <MediaRowActions file={file} busy={busy} onDelete={onDelete} onRearchive={onRearchive} />
                  </td>
                )}
              </>
            ) : (
              <>
                <MediaNameCell file={file} name={file.original_file_name} />
                <td>
                  <div className="statusStack">
                    <Status value={file.process_status} />
                    <small>{statusLabel(file.match_status)}</small>
                  </div>
                </td>
                <td>{techSummary(file) || '未知'}</td>
                <PathCell value={file.final_path || file.planned_target || file.current_path} />
                <td>{formatDate(file.updated_at)}</td>
                {hasActions && (
                  <td>
                    <MediaRowActions file={file} busy={busy} onDelete={onDelete} onRearchive={onRearchive} />
                  </td>
                )}
              </>
            )}
          </tr>
        ))}
        {rows.length === 0 && (
          <tr>
            <td className="emptyCell" colSpan={columns.length}>
              暂无数据
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}

function MediaNameCell({ file, name }: { file: MediaFile; name: string }) {
  const meta = [
    mediaTypeLabel(file.media_type),
    file.parse_title || '',
    file.season > 0 || file.episode > 0 ? `S${twoDigit(file.season)}E${twoDigit(file.episode)}` : '',
  ].filter(Boolean);
  return (
    <td>
      <div className="mediaCell">
        <b title={name}>{name || file.original_file_name}</b>
        <small>{meta.join(' · ') || file.extension}</small>
      </div>
    </td>
  );
}

function PathCell({ value }: { value: string }) {
  return (
    <td title={value}>
      <span className="pathPreview">{shortPath(value) || '-'}</span>
    </td>
  );
}

function MediaDetailModal({
  file,
  busy,
  onClose,
  onDelete,
  onRearchive,
}: {
  file: MediaFile | null;
  busy: boolean;
  onClose: () => void;
  onDelete: (file: MediaFile) => void;
  onRearchive: (file: MediaFile) => void;
}) {
  const canRearchive = file ? ['failed', 'done', 'incomplete_collection'].includes(file.process_status) : false;
  return (
    <AnimatePresence>
      {file && (
        <motion.div className="collectionModalOverlay" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <motion.section
            className="collectionModal mediaDetailModal"
            initial={{ opacity: 0, y: 18, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 12, scale: 0.98 }}
            transition={{ duration: 0.22, ease: 'easeOut' }}
          >
            <header className="collectionModalHeader">
              <div>
                <span>{mediaTypeLabel(file.media_type)} · {statusLabel(file.process_status)}</span>
                <h2>{file.final_file_name || file.original_file_name}</h2>
              </div>
              <button className="iconButton" onClick={onClose} disabled={busy} title="关闭" type="button">
                <X size={18} />
              </button>
            </header>

            <div className="mediaDetailSummary">
              <div>
                <span>状态</span>
                <strong>{statusLabel(file.process_status)}</strong>
              </div>
              <div>
                <span>匹配</span>
                <strong>{statusLabel(file.match_status)}</strong>
              </div>
              <div>
                <span>大小</span>
                <strong>{formatSize(file.file_size)}</strong>
              </div>
              <div>
                <span>更新时间</span>
                <strong>{formatDate(file.updated_at)}</strong>
              </div>
            </div>

            <div className="mediaDetailBody">
              <DetailSection title="识别">
                <DetailField label="媒体类型" value={mediaTypeLabel(file.media_type)} />
                <DetailField label="解析标题" value={file.parse_title} />
                <DetailField label="年份" value={file.parse_year ? String(file.parse_year) : ''} />
                <DetailField label="季集" value={file.season || file.episode ? `S${twoDigit(file.season)}E${twoDigit(file.episode)}` : ''} />
                <DetailField label="片源" value={file.source} />
                <DetailField label="扩展名" value={file.extension} />
              </DetailSection>

              <DetailSection title="技术参数">
                <DetailField label="分辨率" value={file.resolution} />
                <DetailField label="视频编码" value={file.video_codec} />
                <DetailField label="音频编码" value={file.audio_codec} />
                <DetailField label="声道" value={file.audio_channels} />
                <DetailField label="HDR" value={file.hdr_format} />
              </DetailSection>

              <DetailSection title="路径">
                <DetailField label="原始路径" value={file.original_path} wide />
                <DetailField label="当前路径" value={file.current_path} wide />
                <DetailField label="计划路径" value={file.planned_target} wide />
                <DetailField label="最终路径" value={file.final_path} wide />
                <DetailField label="校验路径" value={file.last_verified_path} wide />
              </DetailSection>

              {(file.error_code || file.error_message) && (
                <DetailSection title="错误">
                  <DetailField label="错误码" value={errorCodeLabel(file.error_code) || file.error_code} />
                  <DetailField label="错误信息" value={file.error_message} wide />
                </DetailSection>
              )}

              <DetailSection title="记录">
                <DetailField label="文件 ID" value={file.file_id} wide />
                <DetailField label="批次 ID" value={file.batch_id} wide />
                <DetailField label="哈希" value={file.file_hash} wide />
                <DetailField label="哈希类型" value={file.hash_type} />
                <DetailField label="创建时间" value={formatFullDate(file.created_at)} />
                <DetailField label="更新时间" value={formatFullDate(file.updated_at)} />
              </DetailSection>
            </div>

            <div className="settingsActions rearchiveActions">
              <button className="secondaryAction" onClick={() => onRearchive(file)} disabled={busy || !canRearchive} type="button">
                <ArchiveRestore size={17} />
                <span>重新归档</span>
              </button>
              <button className="dangerAction" onClick={() => onDelete(file)} disabled={busy} type="button">
                <Trash2 size={17} />
                <span>删除记录</span>
              </button>
            </div>
          </motion.section>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function DetailSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="detailSection">
      <h3>{title}</h3>
      <div className="detailGrid">{children}</div>
    </section>
  );
}

function DetailField({ label, value, wide = false }: { label: string; value: string; wide?: boolean }) {
  const display = value || '-';
  return (
    <div className={wide ? 'detailField wide' : 'detailField'}>
      <span>{label}</span>
      <code title={display}>{display}</code>
    </div>
  );
}

function MediaRowActions({
  file,
  busy,
  onDelete,
  onRearchive,
}: {
  file: MediaFile;
  busy: boolean;
  onDelete?: (file: MediaFile) => void;
  onRearchive?: (file: MediaFile) => void;
}) {
  const canRearchive = ['failed', 'done', 'incomplete_collection'].includes(file.process_status);
  return (
    <div className="tableActions">
      {onRearchive && (
        <button
          className="iconButton compact"
          onClick={(event) => {
            event.stopPropagation();
            onRearchive(file);
          }}
          disabled={busy || !canRearchive}
          title={canRearchive ? '按 TMDB ID 重新归档' : '仅失败或已归档记录可重新归档'}
          type="button"
        >
          <ArchiveRestore size={17} />
        </button>
      )}
      {onDelete && (
        <button
          className="iconButton compact dangerIcon"
          onClick={(event) => {
            event.stopPropagation();
            onDelete(file);
          }}
          disabled={busy}
          title="删除数据库记录"
          type="button"
        >
          <Trash2 size={17} />
        </button>
      )}
    </div>
  );
}

function TablePager({ page, offset, setOffset }: { page: { total: number; limit: number }; offset: number; setOffset: (value: number) => void }) {
  const start = page.total === 0 ? 0 : offset + 1;
  const end = Math.min(offset + page.limit, page.total);
  const canPrev = offset > 0;
  const canNext = offset + page.limit < page.total;
  return (
    <div className="tablePager">
      <span>
        {start}-{end} / {page.total}
      </span>
      <div className="rowActions">
        <button className="iconButton compact" onClick={() => setOffset(Math.max(0, offset - page.limit))} disabled={!canPrev} title="上一页">
          <ChevronLeft size={17} />
        </button>
        <button className="iconButton compact" onClick={() => setOffset(offset + page.limit)} disabled={!canNext} title="下一页">
          <ChevronRight size={17} />
        </button>
      </div>
    </div>
  );
}

function isTVLike(file: MediaFile) {
  return file.media_type === 'tv_episode' || file.season > 0 || file.episode > 0 || /S\d{1,2}E\d{1,3}/i.test(file.original_file_name);
}

function rearchivePayload(draft: RearchiveDraft): RearchivePayload | null {
  const tmdbID = optionalPositiveInt(draft.tmdbID);
  const season = optionalPositiveInt(draft.season, true);
  const episode = optionalPositiveInt(draft.episode);
  const seasonOffset = optionalInt(draft.seasonOffset);
  const episodeOffset = optionalInt(draft.episodeOffset);
  if ([tmdbID, season, episode, seasonOffset, episodeOffset].some((value) => Number.isNaN(value))) return null;
  const payload: RearchivePayload = { media_type: draft.mediaType };
  if (tmdbID !== undefined) payload.tmdb_id = tmdbID;
  if (draft.mediaType === 'tv_episode') {
    if (season !== undefined) payload.season = season;
    if (episode !== undefined) payload.episode = episode;
    if (seasonOffset !== undefined) payload.season_offset = seasonOffset;
    if (episodeOffset !== undefined) payload.episode_offset = episodeOffset;
  }
  return payload;
}

function optionalPositiveInt(value: string, allowZero = false) {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  if (!/^\d+$/.test(trimmed)) return Number.NaN;
  const parsed = Number.parseInt(trimmed, 10);
  if (!Number.isFinite(parsed) || parsed < (allowZero ? 0 : 1)) return Number.NaN;
  return parsed;
}

function optionalInt(value: string) {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  if (!/^-?\d+$/.test(trimmed)) return Number.NaN;
  const parsed = Number.parseInt(trimmed, 10);
  if (!Number.isFinite(parsed)) return Number.NaN;
  return parsed;
}

function Status({ value }: { value: string }) {
  const raw = value || 'unknown';
  const label = statusLabel(raw);
  const ok = raw === 'done' || raw === 'complete' || raw === 'ok';
  const bad = raw === 'failed' || raw === 'interrupted' || raw.includes('FAILED');
  const Icon = ok ? CheckCircle2 : bad ? XCircle : Activity;
  return (
    <span className={bad ? 'status bad' : ok ? 'status ok' : 'status'}>
      <Icon size={15} />
      {label}
    </span>
  );
}

function statusLabel(value: string) {
  const labels: Record<string, string> = {
    idle: '空闲',
    queued: '排队中',
    running: '运行中',
    cancelling: '停止中',
    cancelled: '已停止',
    interrupted: '已中断',
    incoming: '入库中',
    scanned: '已扫描',
    parsed: '已解析',
    scraped: '已识别',
    matched: '已匹配',
    collection_checked: '已检查',
    planned: '已规划',
    moved: '已移动',
    done: '已完成',
    complete: '完成',
    failed: '失败',
    incomplete: '缺失',
    incomplete_collection: '缺失合集',
    ok: '正常',
    unknown: '未知',
  };
  return labels[value] ?? value;
}

function twoDigit(value: number) {
  return String(value || 0).padStart(2, '0');
}

function mediaTypeLabel(value: string) {
  const labels: Record<string, string> = {
    movie: '电影',
    tv_episode: '剧集',
    collection_movie: '合集电影',
  };
  return labels[value] ?? value;
}

function techSummary(file: MediaFile) {
  return [file.resolution, file.video_codec, file.hdr_format, file.audio_codec, file.audio_channels]
    .filter((item) => item && item !== 'Unknown')
    .join(' · ');
}

function errorCodeLabel(value: string) {
  if (!value) return '';
  const labels: Record<string, string> = {
    UNSUPPORTED_EXTENSION: '扩展名不支持',
    FILE_TOO_SMALL: '文件过小',
    FILE_NOT_READABLE: '文件不可读',
    FILE_HASH_FAILED: '哈希失败',
    PARSE_TITLE_EMPTY: '标题为空',
    PARSE_YEAR_EMPTY: '年份为空',
    PARSE_SEASON_EMPTY: '季号为空',
    PARSE_EPISODE_EMPTY: '集号为空',
    SCRAPE_EMPTY_RESULT: '无搜索结果',
    SCRAPE_REQUEST_FAILED: '识别请求失败',
    MATCH_NOT_FOUND: '未找到匹配',
    MATCH_NOT_UNIQUE: '匹配不唯一',
    TV_EPISODE_NOT_FOUND: '剧集分集不存在',
    COLLECTION_FETCH_FAILED: '合集获取失败',
    COLLECTION_CHECK_FAILED: '合集检查失败',
    TEMPLATE_NOT_FOUND: '模板不存在',
    TEMPLATE_FIELD_INVALID: '模板字段无效',
    TEMPLATE_PATH_ESCAPE: '模板路径越界',
    TARGET_PATH_EXISTS: '目标已存在',
    TARGET_DIR_CREATE_FAILED: '目录创建失败',
    MOVE_TO_STAGING_FAILED: '移动到整理目录失败',
    MOVE_TO_FAILED_DIR_FAILED: '移动到失败目录失败',
    MOVE_TO_INCOMPLETE_COLLECTION_FAILED: '移动到缺失合集失败',
    COLLECTION_COMPLETE_MOVE_FAILED: '合集迁移失败',
    CLOUDDRIVE_REQUEST_FAILED: '云端请求失败',
    DATABASE_WRITE_FAILED: '数据库写入失败',
    SUBTITLE_MOVE_FAILED: '字幕移动失败',
    MEDIA_PROBE_FAILED: '媒体参数读取失败',
  };
  return labels[value] ?? value;
}

function sourceLabel(value: string) {
  const labels: Record<string, string> = {
    local: '本地',
    cloud: '云端',
  };
  return labels[value] ?? (value || '本地');
}

function queueLabel(value: string) {
  const labels: Record<string, string> = {
    'queue:scan': '扫描',
    'queue:parse': '解析',
    'queue:scrape': '识别',
    'queue:match': '匹配',
    'queue:collection_check': '合集',
    'queue:organize': '移动',
    'queue:failed': '失败',
  };
  return labels[value] ?? value.replace('queue:', '');
}

function formatSize(value: number) {
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

function formatDate(value: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date);
}

function formatFullDate(value: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date);
}

function formatDuration(value: number) {
  if (!value) return '-';
  if (value < 1000) return `${value} ms`;
  const seconds = Math.round(value / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return rest ? `${minutes} 分 ${rest} 秒` : `${minutes} 分`;
}

function p115TriggerLabel(value: string) {
  const labels: Record<string, string> = {
    manual_export: '手动快照',
    manual_sync: '手动同步',
    manual_cleanup: '手动清理',
    cron: '定时同步',
  };
  return labels[value] ?? value;
}

function p115SyncStatusLabel(value: string) {
  const labels: Record<string, string> = {
    running: '运行中',
    success: '成功',
    partial: '部分失败',
    failed: '失败',
  };
  return labels[value] ?? value;
}

function p115ModeLabel(value: string) {
  const labels: Record<string, string> = {
    refresh: '快照',
    events: '事件',
    scan: '扫描',
    cache: '缓存',
    snapshot: '快照',
    cleanup: '清理',
    sync: '同步',
  };
  return labels[value] ?? (value || '-');
}

function shortPath(value: string) {
  if (!value) return '';
  return value.length > 58 ? `...${value.slice(-55)}` : value;
}

function shortText(value: string, max = 58) {
  if (!value) return '';
  return value.length > max ? `${value.slice(0, max - 3)}...` : value;
}
