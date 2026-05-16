package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"curio/internal/models"
)

const MinFileSize int64 = 1

var SupportedExtensions = map[string]struct{}{
	".mkv": {}, ".mp4": {}, ".avi": {}, ".mov": {}, ".ts": {}, ".m2ts": {}, ".iso": {},
}

var episodeTokenRE = regexp.MustCompile(`(?i)s\d{1,2}[ ._-]*e\d{1,3}|\b\d{1,2}x\d{1,3}\b`)

type File struct {
	Path         string
	Name         string
	Extension    string
	Size         int64
	Hash         string
	HashType     string
	Sidecars     []Sidecar
	ErrorCode    string
	ErrorMessage string
}

type Sidecar struct {
	Path      string
	Name      string
	Extension string
	Size      int64
}

func Scan(ctx context.Context, incomingPath string) ([]File, error) {
	files := make([]File, 0)
	sidecars := make([]Sidecar, 0)
	err := filepath.WalkDir(incomingPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		file := File{
			Path:      path,
			Name:      info.Name(),
			Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(info.Name())), "."),
			Size:      info.Size(),
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if IsSubtitleExtension(ext) {
			sidecars = append(sidecars, Sidecar{Path: path, Name: info.Name(), Extension: strings.TrimPrefix(ext, "."), Size: info.Size()})
			return nil
		}
		if !IsMediaExtension(ext) {
			return nil
		}
		if info.Size() < MinFileSize {
			file.ErrorCode = models.ErrFileTooSmall
			file.ErrorMessage = fmt.Sprintf("文件大小 %d，无有效内容", info.Size())
			files = append(files, file)
			return nil
		}
		hash, hashType, err := Hash(path, info.Size())
		if err != nil {
			file.ErrorCode = models.ErrFileHashFailed
			if strings.HasPrefix(err.Error(), "文件不可读") {
				file.ErrorCode = models.ErrFileNotReadable
			}
			file.ErrorMessage = err.Error()
			files = append(files, file)
			return nil
		}
		file.Hash = hash
		file.HashType = hashType
		files = append(files, file)
		return nil
	})
	return AttachSidecars(files, sidecars), err
}

func IsMediaExtension(ext string) bool {
	_, ok := SupportedExtensions[strings.ToLower(ext)]
	return ok
}

func IsSubtitleExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".srt", ".ass", ".ssa", ".sub", ".sup", ".vtt":
		return true
	default:
		return false
	}
}

func AttachSidecars(files []File, sidecars []Sidecar) []File {
	if len(files) == 0 || len(sidecars) == 0 {
		return files
	}
	byDir := map[string][]Sidecar{}
	for _, sidecar := range sidecars {
		dir := strings.ToLower(slashDir(sidecar.Path))
		byDir[dir] = append(byDir[dir], sidecar)
	}
	mediaCount := map[string]int{}
	for _, file := range files {
		mediaCount[strings.ToLower(slashDir(file.Path))]++
	}
	for i := range files {
		mediaDir := strings.ToLower(slashDir(files[i].Path))
		mediaStem := slashStem(files[i].Name)
		seen := map[string]struct{}{}
		for _, sidecar := range byDir[mediaDir] {
			if matchesSidecar(mediaStem, slashStem(sidecar.Name)) {
				appendSidecar(&files[i], sidecar, seen)
			}
		}
		for dir, items := range byDir {
			if !isSubtitleChildDir(mediaDir, dir) {
				continue
			}
			singleMediaDir := mediaCount[mediaDir] == 1
			for _, sidecar := range items {
				if singleMediaDir || matchesSidecar(mediaStem, slashStem(sidecar.Name)) || sharesEpisodeToken(mediaStem, slashStem(sidecar.Name)) {
					appendSidecar(&files[i], sidecar, seen)
				}
			}
		}
	}
	return files
}

func appendSidecar(file *File, sidecar Sidecar, seen map[string]struct{}) {
	if _, ok := seen[sidecar.Path]; ok {
		return
	}
	seen[sidecar.Path] = struct{}{}
	file.Sidecars = append(file.Sidecars, sidecar)
}

func matchesSidecar(mediaStem, sidecarStem string) bool {
	mediaStem = strings.ToLower(strings.TrimSpace(mediaStem))
	sidecarStem = strings.ToLower(strings.TrimSpace(sidecarStem))
	return mediaStem != "" && (sidecarStem == mediaStem || strings.HasPrefix(sidecarStem, mediaStem+".") || strings.HasPrefix(sidecarStem, mediaStem+" "))
}

func sharesEpisodeToken(mediaStem, sidecarStem string) bool {
	sidecarStem = normalizeEpisodeTokenText(sidecarStem)
	for _, token := range episodeTokenRE.FindAllString(normalizeEpisodeTokenText(mediaStem), -1) {
		if strings.Contains(sidecarStem, token) {
			return true
		}
	}
	return false
}

func normalizeEpisodeTokenText(value string) string {
	return strings.NewReplacer(" ", "", ".", "", "_", "", "-", "").Replace(strings.ToLower(value))
}

func isSubtitleChildDir(mediaDir, sidecarDir string) bool {
	parent, name := slashParentBase(sidecarDir)
	return parent == mediaDir && isSubtitleDirName(name)
}

func isSubtitleDirName(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "subs", "sub", "subtitle", "subtitles", "字幕", "简体", "繁体":
		return true
	default:
		return false
	}
}

func slashParentBase(value string) (string, string) {
	value = strings.TrimRight(strings.ReplaceAll(value, `\`, "/"), "/")
	index := strings.LastIndex(value, "/")
	if index < 0 {
		return "", value
	}
	return value[:index], value[index+1:]
}

func slashDir(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		return value[:index]
	}
	return ""
}

func slashStem(value string) string {
	ext := filepath.Ext(value)
	return strings.TrimSuffix(value, ext)
}

func Hash(path string, size int64) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("文件不可读: %w", err)
	}
	defer file.Close()
	if size <= 64*1024*1024 {
		sum := sha256.New()
		if _, err := io.Copy(sum, file); err != nil {
			return "", "", err
		}
		return hex.EncodeToString(sum.Sum(nil)), "full_sha256", nil
	}
	sum := sha256.New()
	_, _ = fmt.Fprintf(sum, "size:%d\n", size)
	offsets := []int64{0, max(0, size/2-512*1024), max(0, size-1024*1024)}
	buf := make([]byte, 1024*1024)
	for _, offset := range offsets {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return "", "", err
		}
		n, err := io.ReadFull(file, buf)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return "", "", err
		}
		sum.Write(buf[:n])
	}
	return hex.EncodeToString(sum.Sum(nil)), "quick_sha256", nil
}
