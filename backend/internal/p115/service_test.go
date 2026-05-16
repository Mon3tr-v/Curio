package p115

import (
	"strings"
	"testing"

	"curio/internal/models"
)

func TestPlayURLForLinkNameUsesReadableDisplayPathWithoutToken(t *testing.T) {
	service := NewService(nil)
	playURL, err := service.PlayURLForLinkName("link-1", "http://localhost:8080", "电影/敦刻尔克 (2017) - 2160p UHD HEVC DTS-HD MA.iso")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(playURL, "token=") {
		t.Fatalf("expected token-free play url, got %q", playURL)
	}
	if strings.Contains(playURL, "%E6%") {
		t.Fatalf("expected readable utf-8 play url, got %q", playURL)
	}
	if strings.Contains(playURL, " ") {
		t.Fatalf("expected spaces to be escaped for player compatibility, got %q", playURL)
	}
	if !strings.Contains(playURL, "/play/115/电影/敦刻尔克%20(2017)%20-%202160p%20UHD%20HEVC%20DTS-HD%20MA.iso") {
		t.Fatalf("expected chinese display path, got %q", playURL)
	}
}

func TestPlayURLForLinkNameFallsBackToIDRoute(t *testing.T) {
	service := NewService(nil)
	playURL, err := service.PlayURLForLinkName("link-2", "http://localhost:8080", "")
	if err != nil {
		t.Fatal(err)
	}
	if playURL != "http://localhost:8080/play/115/id/link-2" {
		t.Fatalf("unexpected fallback play url %q", playURL)
	}
}

func TestPlayBaseURLFallsBackToEmbyPublicURL(t *testing.T) {
	got := playBaseURL(models.P115Settings{EmbyPublicURL: "http://192.168.10.83:8097"}, "http://localhost:8080")
	if got != "http://192.168.10.83:8097" {
		t.Fatalf("unexpected play base url %q", got)
	}
}

func TestApplyLifeEventsUpdatesNodeTree(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	nodes := []models.P115Node{
		{LibraryCID: "root", RemoteFileID: "dir-1", ParentFileID: "root", RelativePath: "Anime", Name: "Anime", IsDirectory: true, IsAlive: true},
		{LibraryCID: "root", RemoteFileID: "file-1", ParentFileID: "dir-1", RelativePath: "Anime/A.mkv", Name: "A.mkv", PickCode: "pc1", SHA1: "sha1", Size: 10, IsAlive: true, IsMedia: true},
	}
	updated, changed := applyLifeEventsToNodes(lib, nodes, []LifeEvent{
		{ID: 1, Type: 20, FileID: "dir-1", ParentID: "root", Name: "Anime Renamed"},
		{ID: 2, Type: 2, FileID: "file-2", ParentID: "dir-1", Name: "B.mkv", PickCode: "pc2", SHA1: "sha2", Size: 20},
	}, "v2")
	if !changed {
		t.Fatal("expected node tree to change")
	}
	items := treeItemsFromNodes(aliveNodes(updated))
	paths := make(map[string]bool, len(items))
	for _, item := range items {
		paths[item.RelativePath] = true
	}
	for _, want := range []string{"Anime Renamed", "Anime Renamed/A.mkv", "Anime Renamed/B.mkv"} {
		if !paths[want] {
			t.Fatalf("expected path %q in %#v", want, paths)
		}
	}
}

func TestApplyLifeEventsDeletesDirectoryDescendants(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	nodes := []models.P115Node{
		{LibraryCID: "root", RemoteFileID: "dir-1", ParentFileID: "root", RelativePath: "Anime", Name: "Anime", IsDirectory: true, IsAlive: true},
		{LibraryCID: "root", RemoteFileID: "file-1", ParentFileID: "dir-1", RelativePath: "Anime/A.mkv", Name: "A.mkv", PickCode: "pc1", SHA1: "sha1", Size: 10, IsAlive: true, IsMedia: true},
	}
	updated, changed := applyLifeEventsToNodes(lib, nodes, []LifeEvent{{ID: 1, Type: 22, FileID: "dir-1"}}, "v2")
	if !changed {
		t.Fatal("expected delete event to change nodes")
	}
	if got := len(aliveNodes(updated)); got != 0 {
		t.Fatalf("expected no alive nodes, got %d: %#v", got, updated)
	}
}

func TestDirectoryMoveEventStaysIncremental(t *testing.T) {
	events := []LifeEvent{{ID: 1, Type: 6, FileID: "dir-1", ParentID: "root", Name: "tv"}}
	if eventsNeedTreeScan(events) {
		t.Fatal("expected directory-like move event to stay incremental")
	}
	if !eventCreatesDirectory(events[0]) {
		t.Fatal("expected directory-like move event to create a directory node")
	}
}

func TestEventsNeedTreeScanKeepsNormalVideoEventIncremental(t *testing.T) {
	events := []LifeEvent{{ID: 1, Type: 6, FileID: "file-1", ParentID: "root", Name: "甜心格格 - S05E01.mp4"}}
	if eventsNeedTreeScan(events) {
		t.Fatal("expected normal video event to stay incremental")
	}
}

func TestAdvanceCursorWithBatchUsesRawHighWater(t *testing.T) {
	cursor := models.P115EventCursor{LibraryCID: "root", LastEventID: 10, LastEventTime: 1000}
	next := advanceCursorWithBatch(cursor, LifeEventBatch{LastEventID: 20, LastEventTime: 2000})

	if next.LastEventID != 20 || next.LastEventTime != 2000 {
		t.Fatalf("expected raw event cursor to advance, got %#v", next)
	}
}

func TestApplyLifeEventsCreatesMovedDirectoryBeforeChildren(t *testing.T) {
	lib := LibraryConfig{CID: "root"}
	updated, changed := applyLifeEventsToNodes(lib, nil, []LifeEvent{
		{ID: 1, Type: 6, FileID: "dir-1", ParentID: "root", Name: "神兵小将 (2007)"},
		{ID: 2, Type: 6, FileID: "file-1", ParentID: "dir-1", Name: "神兵小将 - S01E52.mkv", PickCode: "pc1", SHA1: "sha1", Size: 10},
	}, "v2")
	if !changed {
		t.Fatal("expected node tree to change")
	}
	items := treeItemsFromNodes(aliveNodes(updated))
	paths := make(map[string]bool, len(items))
	for _, item := range items {
		paths[item.RelativePath] = item.IsDirectory
	}
	if !paths["神兵小将 (2007)"] {
		t.Fatalf("expected moved directory in %#v", items)
	}
	if _, ok := paths["神兵小将 (2007)/神兵小将 - S01E52.mkv"]; !ok {
		t.Fatalf("expected moved child file in %#v", items)
	}
}
