package worker

import (
	"strings"
	"testing"

	"curio/internal/scanner"
)

func TestSubtitleTargetUsesChineseLanguageSuffix(t *testing.T) {
	counts := map[string]int{}
	target := subtitleTarget("/library/Movie (2020).mkv", scanner.Sidecar{Name: "Movie (2020).zh-SG.srt", Extension: "srt"}, counts)
	if !strings.HasSuffix(target, ".chs.srt") {
		t.Fatalf("expected simplified suffix, got %s", target)
	}

	target = subtitleTarget("/library/Movie (2020).mkv", scanner.Sidecar{Name: "Movie (2020).zh-Hant.ass", Extension: "ass"}, counts)
	if !strings.HasSuffix(target, ".cht.ass") {
		t.Fatalf("expected traditional suffix, got %s", target)
	}
}

func TestSubtitleTargetUsesCompactAnimeSubtitleTags(t *testing.T) {
	counts := map[string]int{}
	simplified := subtitleTarget("/library/Apocalypse Hotel - S01E01.mkv", scanner.Sidecar{
		Path:      "/incoming/[Nekomoe kissaten] Apocalypse Hotel [01].JPSC.ass",
		Name:      "[Nekomoe kissaten] Apocalypse Hotel [01].JPSC.ass",
		Extension: "ass",
	}, counts)
	if !strings.HasSuffix(simplified, ".chs.ass") {
		t.Fatalf("expected JPSC to become simplified suffix, got %s", simplified)
	}

	traditional := subtitleTarget("/library/Apocalypse Hotel - S01E01.mkv", scanner.Sidecar{
		Path:      "/incoming/[Nekomoe kissaten] Apocalypse Hotel [01].JPTC.ass",
		Name:      "[Nekomoe kissaten] Apocalypse Hotel [01].JPTC.ass",
		Extension: "ass",
	}, counts)
	if !strings.HasSuffix(traditional, ".cht.ass") {
		t.Fatalf("expected JPTC to become traditional suffix, got %s", traditional)
	}
}

func TestSubtitleTargetUsesLanguageDirectoryHint(t *testing.T) {
	counts := map[string]int{}
	target := subtitleTarget("/library/Show - S01E01.mkv", scanner.Sidecar{
		Path:      "/incoming/Show/Subs/繁體/Show - S01E01.ass",
		Name:      "Show - S01E01.ass",
		Extension: "ass",
	}, counts)
	if !strings.HasSuffix(target, ".cht.ass") {
		t.Fatalf("expected directory language hint, got %s", target)
	}
}

func TestSubtitleTargetIgnoresEmbeddedSCLetters(t *testing.T) {
	counts := map[string]int{}
	target := subtitleTarget("/library/Show - S01E01.mkv", scanner.Sidecar{Name: "discussion.ass", Extension: "ass"}, counts)
	if strings.HasSuffix(target, ".chs.ass") || strings.HasSuffix(target, ".cht.ass") {
		t.Fatalf("expected no language suffix from embedded letters, got %s", target)
	}
}

func TestSubtitleTargetKeepsDuplicateSubtitles(t *testing.T) {
	counts := map[string]int{}
	first := subtitleTarget("/library/Movie.mkv", scanner.Sidecar{Name: "Movie.zh-CN.srt", Extension: "srt"}, counts)
	second := subtitleTarget("/library/Movie.mkv", scanner.Sidecar{Name: "Movie.SC.srt", Extension: "srt"}, counts)
	if first == second || !strings.HasSuffix(second, ".chs.2.srt") {
		t.Fatalf("expected duplicate suffix, got first=%s second=%s", first, second)
	}
}

func TestTemplateWithImplicitCategoryKeepsMediaTopLevelFirst(t *testing.T) {
	values := map[string]string{"category": "欧美电影"}
	got := templateWithImplicitCategory("movies/{title}/{title}.{extension}", values)
	want := "movies/{category}/{title}/{title}.{extension}"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateWithImplicitCategoryPrefixesCustomTemplate(t *testing.T) {
	values := map[string]string{"category": "欧美电影"}
	got := templateWithImplicitCategory("{title}/{title}.{extension}", values)
	want := "{category}/{title}/{title}.{extension}"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
