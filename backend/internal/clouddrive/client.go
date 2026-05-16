package clouddrive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"curio/internal/clouddrive/pb"
	"curio/internal/models"
	"curio/internal/organizer"
	"curio/internal/scanner"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

const Scheme = "cd2://"

type Client struct {
	settings models.CloudDriveSettings
}

type File struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	URI         string `json:"uri"`
	Size        int64  `json:"size"`
	Extension   string `json:"extension"`
	IsDirectory bool   `json:"is_directory"`
	IsCloudFile bool   `json:"is_cloud_file"`
	IsLocal     bool   `json:"is_local"`
	Hash        string `json:"hash"`
	HashType    string `json:"hash_type"`
}

type Status struct {
	Ready     bool   `json:"ready"`
	LoggedIn  bool   `json:"logged_in"`
	UserName  string `json:"user_name"`
	Message   string `json:"message"`
	Address   string `json:"address"`
	RootPath  string `json:"root_path"`
	CanBrowse bool   `json:"can_browse"`
}

type DownloadSource struct {
	URL       string
	Headers   map[string]string
	UserAgent string
	Mode      string
}

type DownloadMode string

const (
	DownloadModeAuto   DownloadMode = "auto"
	DownloadModeDirect DownloadMode = "direct"
	DownloadModeProxy  DownloadMode = "proxy"
)

type ByteRange struct {
	Start  uint64
	Length uint64
}

type session struct {
	client pb.CloudDriveFileSrvClient
	token  string
}

type DriveSession struct {
	conn    *grpc.ClientConn
	s       *session
	address string
}

func New(settings models.CloudDriveSettings) *Client {
	return &Client{settings: settings}
}

func NormalizeDownloadMode(value string) DownloadMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(DownloadModeDirect):
		return DownloadModeDirect
	case string(DownloadModeProxy):
		return DownloadModeProxy
	default:
		return DownloadModeAuto
	}
}

func ProbeDownloadMode() DownloadMode {
	return NormalizeDownloadMode(os.Getenv("CURIO_CD2_PROBE_MODE"))
}

func ProbePrefetchEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CURIO_CD2_PREFETCH"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func IsURI(value string) bool {
	return strings.HasPrefix(value, Scheme)
}

func URI(value string) string {
	return Scheme + NormalizePath(value)
}

func FromURI(value string) string {
	return NormalizePath(strings.TrimPrefix(value, Scheme))
}

func NormalizePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, Scheme)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	clean := path.Clean(value)
	if clean == "." {
		return "/"
	}
	return clean
}

func Join(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			clean = append(clean, strings.Trim(part, "/"))
		}
	}
	if len(clean) == 0 {
		return "/"
	}
	return "/" + path.Join(clean...)
}

func (c *Client) Test(ctx context.Context) (Status, error) {
	conn, client, err := c.connect(ctx)
	if err != nil {
		return Status{}, err
	}
	defer conn.Close()
	info, err := client.GetSystemInfo(ctx, &emptypb.Empty{})
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Ready:    info.GetSystemReady(),
		LoggedIn: info.GetIsLogin(),
		UserName: info.GetUserName(),
		Address:  c.settings.Address,
		RootPath: NormalizePath(c.settings.RootPath),
	}
	if info.SystemMessage != nil {
		status.Message = info.GetSystemMessage()
	}
	if c.hasCredentials() {
		s := &session{client: client}
		if err := c.authenticate(ctx, s); err != nil {
			return status, err
		}
		if _, err := s.list(ctx, NormalizePath(c.settings.RootPath)); err != nil {
			return status, err
		}
		status.CanBrowse = true
	}
	return status, nil
}

func (c *Client) List(ctx context.Context, dir string) ([]File, error) {
	drive, err := c.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer drive.Close()
	return drive.List(ctx, dir)
}

func (c *Client) Scan(ctx context.Context, root string) ([]scanner.File, error) {
	drive, err := c.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer drive.Close()
	return drive.Scan(ctx, root)
}

func (c *Client) MoveFile(ctx context.Context, sourceURI, targetPath string) (string, error) {
	drive, err := c.Open(ctx)
	if err != nil {
		return "", err
	}
	defer drive.Close()
	return drive.MoveFile(ctx, sourceURI, targetPath)
}

func (c *Client) ArchiveFailure(ctx context.Context, failedRoot string, file models.MediaFile, code, message string) (string, error) {
	drive, err := c.Open(ctx)
	if err != nil {
		return "", err
	}
	defer drive.Close()
	return drive.ArchiveFailure(ctx, failedRoot, file, code, message)
}

func (c *Client) MigrateCollection(ctx context.Context, sourceRoot, targetRoot string) error {
	drive, err := c.Open(ctx)
	if err != nil {
		return err
	}
	defer drive.Close()
	return drive.MigrateCollection(ctx, sourceRoot, targetRoot)
}

func (c *Client) Open(ctx context.Context) (*DriveSession, error) {
	conn, s, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	return &DriveSession{conn: conn, s: s, address: c.settings.Address}, nil
}

func (d *DriveSession) Close() error {
	if d == nil || d.conn == nil {
		return nil
	}
	return d.conn.Close()
}

func (d *DriveSession) List(ctx context.Context, dir string) ([]File, error) {
	return d.s.list(ctx, dir)
}

func (d *DriveSession) Scan(ctx context.Context, root string, excludes ...string) ([]scanner.File, error) {
	files := make([]scanner.File, 0)
	sidecars := make([]scanner.Sidecar, 0)
	excluded := normalizeExcludes(root, excludes)
	err := d.s.walk(ctx, NormalizePath(root), func(file File) bool {
		if remotePathExcluded(file.Path, excluded) {
			return false
		}
		if scanned, ok := toScannedFile(file); ok {
			files = append(files, scanned)
			return true
		}
		if scanner.IsSubtitleExtension("." + file.Extension) {
			sidecars = append(sidecars, scanner.Sidecar{Path: file.URI, Name: file.Name, Extension: file.Extension, Size: file.Size})
		}
		return true
	})
	return scanner.AttachSidecars(files, sidecars), err
}

func (d *DriveSession) DownloadURL(ctx context.Context, sourceURI string) (DownloadSource, error) {
	return d.DownloadURLWithMode(ctx, sourceURI, DownloadModeAuto)
}

func (d *DriveSession) DownloadURLWithMode(ctx context.Context, sourceURI string, mode DownloadMode) (DownloadSource, error) {
	mode = NormalizeDownloadMode(string(mode))
	info, err := d.s.client.GetDownloadUrlPath(d.s.auth(ctx), &pb.GetDownloadUrlPathRequest{
		Path:         FromURI(sourceURI),
		LazyRead:     true,
		GetDirectUrl: mode != DownloadModeProxy,
	})
	if err != nil {
		return DownloadSource{}, err
	}
	downloadURL, actualMode, err := buildDownloadURLWithMode(d.address, info, mode)
	if err != nil {
		return DownloadSource{}, err
	}
	return DownloadSource{
		URL:       downloadURL,
		Headers:   info.GetAdditionalHeaders(),
		UserAgent: info.GetUserAgent(),
		Mode:      string(actualMode),
	}, nil
}

func (d *DriveSession) Prefetch(ctx context.Context, sourceURI string, ranges []ByteRange) error {
	if len(ranges) == 0 {
		return nil
	}
	items := make([]*pb.ByteRange, 0, len(ranges))
	for _, item := range ranges {
		if item.Length == 0 {
			continue
		}
		items = append(items, &pb.ByteRange{Start: item.Start, Length: item.Length})
	}
	if len(items) == 0 {
		return nil
	}
	_, err := d.s.client.PrefetchFileRanges(d.s.auth(ctx), &pb.PrefetchFileRangesRequest{
		Path:            FromURI(sourceURI),
		Ranges:          items,
		Priority:        pb.HintPriority_HINT_PRIORITY_HIGH,
		TtlSeconds:      60,
		ReplaceExisting: true,
	})
	return err
}

func (d *DriveSession) CloseFileReader(ctx context.Context, sourceURI string) error {
	_, err := d.s.client.CloseFileReader(d.s.auth(ctx), &pb.FileRequest{Path: FromURI(sourceURI)})
	return err
}

func (d *DriveSession) MoveFile(ctx context.Context, sourceURI, targetPath string) (string, error) {
	target := NormalizePath(targetPath)
	source := FromURI(sourceURI)
	if NormalizePath(source) == target {
		return URI(target), nil
	}
	parent := path.Dir(target)
	finalName := path.Base(target)
	if err := d.s.ensureDir(ctx, parent); err != nil {
		return "", err
	}
	if exists, _, err := d.s.child(ctx, target); err != nil {
		return "", err
	} else if exists {
		if sourceExists, _, sourceErr := d.s.child(ctx, source); sourceErr == nil && !sourceExists {
			return URI(target), nil
		}
		return "", errors.New(models.ErrTargetPathExists)
	}
	if exists, isDir, err := d.s.child(ctx, source); err != nil {
		return "", err
	} else if !exists {
		intermediate := Join(parent, path.Base(source))
		if NormalizePath(path.Dir(source)) != NormalizePath(parent) {
			if movedExists, movedIsDir, movedErr := d.s.child(ctx, intermediate); movedErr != nil {
				return "", movedErr
			} else if movedExists && !movedIsDir {
				source = intermediate
			} else {
				return "", fmt.Errorf("源文件不存在：%s", FromURI(sourceURI))
			}
		} else {
			return "", fmt.Errorf("源文件不存在：%s", FromURI(sourceURI))
		}
	} else if isDir {
		return "", fmt.Errorf("源路径不是文件：%s", source)
	}
	if NormalizePath(path.Dir(source)) != NormalizePath(parent) {
		policy := pb.MoveFileRequest_Skip
		result, err := d.s.client.MoveFile(d.s.auth(ctx), &pb.MoveFileRequest{
			TheFilePaths:   []string{source},
			DestPath:       parent,
			ConflictPolicy: &policy,
		})
		if err != nil {
			return "", err
		}
		if err := operationErr(result); err != nil {
			return "", err
		}
		source = Join(parent, path.Base(source))
	}
	if path.Base(source) != finalName {
		result, err := d.s.client.RenameFile(d.s.auth(ctx), &pb.RenameFileRequest{TheFilePath: source, NewName: finalName})
		if err != nil {
			return "", err
		}
		if err := operationErr(result); err != nil {
			return "", err
		}
	}
	return URI(target), nil
}

func (d *DriveSession) ArchiveFailure(ctx context.Context, failedRoot string, file models.MediaFile, code, message string) (string, error) {
	_ = message
	target := Join(failedRoot, code, file.BatchID, "files", organizer.FailureFileName(file))
	return d.MoveFile(ctx, file.CurrentPath, target)
}

func (d *DriveSession) PathExists(ctx context.Context, value string) bool {
	exists, _, err := d.s.child(ctx, FromURI(value))
	return err == nil && exists
}

func (d *DriveSession) DeleteEmptyParents(ctx context.Context, startDir, stopRoot string) {
	current := NormalizePath(startDir)
	stop := NormalizePath(stopRoot)
	for current != "" && current != "/" && current != stop {
		files, err := d.s.list(ctx, current)
		if err != nil || len(files) > 0 {
			return
		}
		result, err := d.s.client.DeleteFile(d.s.auth(ctx), &pb.FileRequest{Path: current})
		if err != nil || operationErr(result) != nil {
			return
		}
		next := path.Dir(current)
		if next == current {
			return
		}
		current = next
	}
}

func (d *DriveSession) MigrateCollection(ctx context.Context, sourceRoot, targetRoot string) error {
	sourceRoot = NormalizePath(sourceRoot)
	targetRoot = NormalizePath(targetRoot)
	if exists, isDir, err := d.s.child(ctx, sourceRoot); err != nil {
		return err
	} else if !exists || !isDir {
		return nil
	}
	if err := d.s.ensureDir(ctx, targetRoot); err != nil {
		return err
	}
	files, err := d.s.list(ctx, sourceRoot)
	if err != nil {
		return err
	}
	policy := pb.MoveFileRequest_Skip
	for _, file := range files {
		result, err := d.s.client.MoveFile(d.s.auth(ctx), &pb.MoveFileRequest{
			TheFilePaths:   []string{file.Path},
			DestPath:       targetRoot,
			ConflictPolicy: &policy,
		})
		if err != nil {
			return err
		}
		if err := operationErr(result); err != nil {
			return err
		}
	}
	result, err := d.s.client.DeleteFile(d.s.auth(ctx), &pb.FileRequest{Path: sourceRoot})
	if err != nil {
		return err
	}
	return operationErr(result)
}

func (c *Client) open(ctx context.Context) (*grpc.ClientConn, *session, error) {
	conn, client, err := c.connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	s := &session{client: client}
	if err := c.authenticate(ctx, s); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, s, nil
}

func (c *Client) connect(ctx context.Context) (*grpc.ClientConn, pb.CloudDriveFileSrvClient, error) {
	target, secure, err := dialTarget(c.settings.Address)
	if err != nil {
		return nil, nil, err
	}
	var creds credentials.TransportCredentials = insecure.NewCredentials()
	if secure {
		creds = credentials.NewClientTLSFromCert(nil, "")
	}
	dialCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		dialCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
	}
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, target, grpc.WithTransportCredentials(creds), grpc.WithBlock())
	if err != nil {
		return nil, nil, err
	}
	return conn, pb.NewCloudDriveFileSrvClient(conn), nil
}

func (c *Client) authenticate(ctx context.Context, s *session) error {
	if token := strings.TrimSpace(c.settings.Token); token != "" {
		s.token = token
		return nil
	}
	if !c.hasCredentials() {
		return errors.New("需要配置 CloudDrive2 令牌或用户名密码")
	}
	reply, err := s.client.GetToken(ctx, &pb.GetTokenRequest{UserName: c.settings.Username, Password: c.settings.Password})
	if err != nil {
		return err
	}
	if !reply.GetSuccess() {
		return errors.New(reply.GetErrorMessage())
	}
	if strings.TrimSpace(reply.GetToken()) == "" {
		return errors.New("CloudDrive2 返回了空令牌")
	}
	s.token = reply.GetToken()
	return nil
}

func (c *Client) hasCredentials() bool {
	return strings.TrimSpace(c.settings.Token) != "" || (strings.TrimSpace(c.settings.Username) != "" && strings.TrimSpace(c.settings.Password) != "")
}

func (s *session) auth(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+s.token)
}

func (s *session) list(ctx context.Context, dir string) ([]File, error) {
	stream, err := s.client.GetSubFiles(s.auth(ctx), &pb.ListSubFileRequest{Path: NormalizePath(dir)})
	if err != nil {
		return nil, err
	}
	files := make([]File, 0)
	for {
		reply, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		for _, file := range reply.GetSubFiles() {
			files = append(files, mapFile(file))
		}
	}
}

func (s *session) walk(ctx context.Context, dir string, visit func(File) bool) error {
	files, err := s.list(ctx, dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if file.IsDirectory {
			if !visit(file) {
				continue
			}
			if err := s.walk(ctx, file.Path, visit); err != nil {
				return err
			}
			continue
		}
		visit(file)
	}
	return nil
}

func normalizeExcludes(root string, excludes []string) []string {
	root = NormalizePath(root)
	values := make([]string, 0, len(excludes))
	seen := map[string]struct{}{}
	for _, item := range excludes {
		value := NormalizePath(item)
		if value == "/" || value == root || !remotePathWithin(value, root) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func remotePathExcluded(value string, excludes []string) bool {
	value = NormalizePath(value)
	for _, exclude := range excludes {
		if value == exclude || strings.HasPrefix(value, exclude+"/") {
			return true
		}
	}
	return false
}

func remotePathWithin(value, root string) bool {
	value = NormalizePath(value)
	root = NormalizePath(root)
	return root == "/" || value == root || strings.HasPrefix(value, root+"/")
}

func (s *session) ensureDir(ctx context.Context, dir string) error {
	dir = NormalizePath(dir)
	if dir == "/" {
		return nil
	}
	current := "/"
	for _, part := range strings.Split(strings.Trim(dir, "/"), "/") {
		exists, isDir, err := s.childIn(ctx, current, part)
		if err != nil {
			return err
		}
		current = Join(current, part)
		if exists {
			if !isDir {
				return fmt.Errorf("%s 不是目录", current)
			}
			continue
		}
		result, err := s.client.CreateFolder(s.auth(ctx), &pb.CreateFolderRequest{ParentPath: path.Dir(current), FolderName: path.Base(current)})
		if err != nil {
			return err
		}
		if result.GetResult() != nil && !result.GetResult().GetSuccess() {
			return errors.New(result.GetResult().GetErrorMessage())
		}
	}
	return nil
}

func (s *session) child(ctx context.Context, remotePath string) (bool, bool, error) {
	parent := path.Dir(NormalizePath(remotePath))
	name := path.Base(NormalizePath(remotePath))
	return s.childIn(ctx, parent, name)
}

func (s *session) childIn(ctx context.Context, parent, name string) (bool, bool, error) {
	files, err := s.list(ctx, parent)
	if err != nil {
		return false, false, err
	}
	for _, file := range files {
		if file.Name == name {
			return true, file.IsDirectory, nil
		}
	}
	return false, false, nil
}

func mapFile(file *pb.CloudDriveFile) File {
	hash, hashType := pickHash(file.GetFileHashes(), file.GetFullPathName(), file.GetSize())
	name := file.GetName()
	if name == "" {
		name = path.Base(file.GetFullPathName())
	}
	return File{
		ID:          file.GetId(),
		Name:        name,
		Path:        NormalizePath(file.GetFullPathName()),
		URI:         URI(file.GetFullPathName()),
		Size:        file.GetSize(),
		Extension:   strings.TrimPrefix(strings.ToLower(path.Ext(name)), "."),
		IsDirectory: file.GetIsDirectory(),
		IsCloudFile: file.GetIsCloudFile(),
		IsLocal:     file.GetIsLocal(),
		Hash:        hash,
		HashType:    hashType,
	}
}

func toScannedFile(file File) (scanner.File, bool) {
	scanned := scanner.File{
		Path:      file.URI,
		Name:      file.Name,
		Extension: file.Extension,
		Size:      file.Size,
		Hash:      file.Hash,
		HashType:  file.HashType,
	}
	if !scanner.IsMediaExtension("." + file.Extension) {
		return scanner.File{}, false
	}
	if file.Size < scanner.MinFileSize {
		scanned.ErrorCode = models.ErrFileTooSmall
		scanned.ErrorMessage = fmt.Sprintf("文件大小 %d，无有效内容", file.Size)
	}
	return scanned, true
}

func pickHash(values map[uint32]string, remotePath string, size int64) (string, string) {
	if hash := strings.TrimSpace(values[2]); hash != "" {
		return hash, "cd2_sha1"
	}
	if hash := strings.TrimSpace(values[1]); hash != "" {
		return hash, "cd2_md5"
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", NormalizePath(remotePath), size)))
	return hex.EncodeToString(sum[:]), "cd2_metadata_sha256"
}

func operationErr(result *pb.FileOperationResult) error {
	if result == nil || result.GetSuccess() {
		return nil
	}
	if message := strings.TrimSpace(result.GetErrorMessage()); message != "" {
		return errors.New(message)
	}
	return errors.New(models.ErrCloudDriveRequestFailed)
}

func dialTarget(address string) (string, bool, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		address = "http://localhost:19798"
	}
	parsed, err := url.Parse(address)
	if err == nil && parsed.Scheme != "" {
		if parsed.Host == "" {
			return "", false, fmt.Errorf("CloudDrive2 地址无效：%s", address)
		}
		switch parsed.Scheme {
		case "http":
			return parsed.Host, false, nil
		case "https":
			return parsed.Host, true, nil
		default:
			return "", false, fmt.Errorf("不支持的 CloudDrive2 地址协议：%s", parsed.Scheme)
		}
	}
	return address, false, nil
}

func buildDownloadURL(address string, info *pb.DownloadUrlPathInfo) (string, error) {
	value, _, err := buildDownloadURLWithMode(address, info, DownloadModeAuto)
	return value, err
}

func buildDownloadURLWithMode(address string, info *pb.DownloadUrlPathInfo, mode DownloadMode) (string, DownloadMode, error) {
	mode = NormalizeDownloadMode(string(mode))
	directURL := strings.TrimSpace(info.GetDirectUrl())
	if mode != DownloadModeProxy && directURL != "" {
		return directURL, DownloadModeDirect, nil
	}
	if strings.TrimSpace(info.GetDownloadUrlPath()) != "" {
		proxyURL, err := buildProxyDownloadURL(address, info)
		if err == nil {
			return proxyURL, DownloadModeProxy, nil
		}
		return "", "", err
	}
	if directURL != "" {
		return directURL, DownloadModeDirect, nil
	}
	_, err := buildProxyDownloadURL(address, info)
	return "", "", err
}

func buildProxyDownloadURL(address string, info *pb.DownloadUrlPathInfo) (string, error) {
	rawPath := strings.TrimSpace(info.GetDownloadUrlPath())
	if rawPath == "" {
		if direct := strings.TrimSpace(info.GetDirectUrl()); direct != "" {
			return direct, nil
		}
		return "", errors.New("CloudDrive2 未返回下载地址")
	}
	base := strings.TrimSpace(address)
	if base == "" {
		base = "http://localhost:19798"
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("CloudDrive2 地址无效：%s", address)
	}
	rawPath = strings.ReplaceAll(rawPath, "{SCHEME}", parsed.Scheme)
	rawPath = strings.ReplaceAll(rawPath, "{HOST}", parsed.Host)
	rawPath = strings.ReplaceAll(rawPath, "{PREVIEW}", "false")
	if parsedPath, err := url.Parse(rawPath); err == nil && parsedPath.Scheme != "" && parsedPath.Host != "" {
		return rawPath, nil
	}
	return parsed.Scheme + "://" + parsed.Host + "/" + strings.TrimLeft(rawPath, "/"), nil
}
