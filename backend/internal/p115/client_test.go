package p115

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"curio/internal/models"
)

func TestFileInfoFromOpenFolderMap(t *testing.T) {
	row := decodeMap(t, `{"fid":"3428179986607432503","fc":0,"fn":"collections","pc":"fednuli5o05t483h77"}`)
	info := fileInfoFromMap(row)
	if info.ID != "3428179986607432503" {
		t.Fatalf("unexpected id %q", info.ID)
	}
	if info.Name != "collections" {
		t.Fatalf("unexpected name %q", info.Name)
	}
	if !info.IsDirectory {
		t.Fatal("expected folder to be detected as directory")
	}
}

func TestFileInfoFromOpenFileMap(t *testing.T) {
	row := decodeMap(t, `{"fid":"3427411743768765275","sha1":"ABC","fn":"movie.iso","fs":62467801088,"pc":"d1yv62zvcl0omxipp","fc":1}`)
	info := fileInfoFromMap(row)
	if info.ID != "3427411743768765275" || info.Name != "movie.iso" {
		t.Fatalf("unexpected file info %#v", info)
	}
	if info.SHA1 != "ABC" || info.Size != 62467801088 || info.PickCode != "d1yv62zvcl0omxipp" {
		t.Fatalf("unexpected media fields %#v", info)
	}
	if info.IsDirectory {
		t.Fatal("expected media file, got directory")
	}
}

func TestCookiesFromLoginResponseMap(t *testing.T) {
	payload := decodeMap(t, `{"state":1,"data":{"cookie":{"UID":"u","CID":"c","SEID":"s","KID":"k"}}}`)
	cookies := cookiesFromLoginResponse(payload, nil)
	for _, want := range []string{"UID=u", "CID=c", "SEID=s", "KID=k"} {
		if !strings.Contains(cookies, want) {
			t.Fatalf("expected %q in %q", want, cookies)
		}
	}
}

func TestCookiesFromLoginResponseHeader(t *testing.T) {
	header := http.Header{}
	header.Add("Set-Cookie", "UID=u; Path=/; HttpOnly")
	header.Add("Set-Cookie", "SEID=s; Path=/; HttpOnly")
	cookies := cookiesFromLoginResponse(nil, header)
	if cookies != "UID=u; SEID=s" {
		t.Fatalf("unexpected cookies %q", cookies)
	}
}

func TestClientUsesCookiesBeforeOpenToken(t *testing.T) {
	cookieClient := NewClient(models.P115Settings{
		AuthMode:    authModeCookies,
		Cookies:     "UID=u; CID=c; SEID=s",
		AccessToken: "open-token",
	})
	if cookieClient.preferOpen() {
		t.Fatal("expected cookies mode to avoid Open API when cookies are available")
	}

	openClient := NewClient(models.P115Settings{
		AuthMode:    authModeOpen,
		Cookies:     "UID=u; CID=c; SEID=s",
		AccessToken: "open-token",
	})
	if openClient.preferOpen() {
		t.Fatal("expected cookies to stay first even when open mode was previously selected")
	}

	openOnlyClient := NewClient(models.P115Settings{
		AuthMode:    authModeOpen,
		AccessToken: "open-token",
	})
	if !openOnlyClient.preferOpen() {
		t.Fatal("expected Open API when cookies are not available")
	}
}

func TestLifeEventFromRecentOperationItem(t *testing.T) {
	row := decodeMap(t, `{
		"behavior_type":"file_rename",
		"relation_id":"9001",
		"file_id":"3427411743768765275",
		"parent_id":"3429318291990438503",
		"file_name":"暗黑 S01E01.mkv",
		"pick_code":"pc1",
		"sha1":"ABC",
		"file_size":123456,
		"update_time":1710000000
	}`)
	event := lifeEventFromRecentMap(row, LifeEvent{})
	if event.Type != 24 || event.EventName != "file_rename" {
		t.Fatalf("unexpected event type %#v", event)
	}
	if event.ID != 9001 || event.FileID != "3427411743768765275" || event.ParentID != "3429318291990438503" {
		t.Fatalf("unexpected identifiers %#v", event)
	}
	if event.Name != "暗黑 S01E01.mkv" || event.PickCode != "pc1" || event.SHA1 != "ABC" || event.Size != 123456 {
		t.Fatalf("unexpected file fields %#v", event)
	}
}

func TestLifeEventFromBehaviorDetailItem(t *testing.T) {
	row := decodeMap(t, `{
		"id":"91001",
		"type":6,
		"file_id":"3427411743768765275",
		"parent_id":"3429318291990438503",
		"file_name":"数码宝贝 - S02E41.mkv",
		"pick_code":"pc1",
		"sha1":"ABC",
		"file_size":"1836098519",
		"create_time":1778856000,
		"update_time":1778856001
	}`)

	event := lifeEventFromBehaviorMap(row)

	if event.ID != 91001 || event.Type != 6 || event.EventName != "move_file" {
		t.Fatalf("unexpected event identity %#v", event)
	}
	if event.FileID != "3427411743768765275" || event.ParentID != "3429318291990438503" {
		t.Fatalf("unexpected ids %#v", event)
	}
	if event.Name != "数码宝贝 - S02E41.mkv" || event.PickCode != "pc1" || event.SHA1 != "ABC" {
		t.Fatalf("unexpected file fields %#v", event)
	}
	if event.Size != 1836098519 || event.CreateTime != 1778856000 || event.UpdateTime != 1778856001 {
		t.Fatalf("unexpected numeric fields %#v", event)
	}
}

func TestLifeEventFromRecentOperationGeneratesStableID(t *testing.T) {
	row := decodeMap(t, `{"behavior_type":"move_file","file_id":"f1","parent_id":"p2","file_name":"movie.mkv","date":"2026-05-15"}`)
	first := lifeEventFromRecentMap(row, LifeEvent{})
	second := lifeEventFromRecentMap(row, LifeEvent{})
	if first.ID == 0 {
		t.Fatal("expected generated id")
	}
	if first.ID != second.ID {
		t.Fatalf("expected stable id, got %d and %d", first.ID, second.ID)
	}
	if first.Type != 6 {
		t.Fatalf("unexpected type %d", first.Type)
	}
}

func TestLifeEventFromRecentOperationItemDateOnlyKeepsBaseTime(t *testing.T) {
	baseTime := parseRecentTime("2026-05-15 18:30:00")
	base := LifeEvent{Type: 6, EventName: "move_file", UpdateTime: baseTime}
	row := decodeMap(t, `{"behavior_type":"move_file","file_id":"f1","parent_id":"p2","file_name":"movie.mkv","date":"2026-05-15"}`)

	event := lifeEventFromRecentMap(row, base)

	if event.UpdateTime != baseTime {
		t.Fatalf("expected base update time %d, got %d", baseTime, event.UpdateTime)
	}
	if event.CreateTime != baseTime {
		t.Fatalf("expected base create time %d, got %d", baseTime, event.CreateTime)
	}
}

func TestLifeEventFromRecentOperationDateOnlyWithoutBaseUsesEndOfDay(t *testing.T) {
	row := decodeMap(t, `{"behavior_type":"move_file","file_id":"f1","parent_id":"p2","file_name":"movie.mkv","date":"2026-05-15"}`)

	event := lifeEventFromRecentMap(row, LifeEvent{})

	if event.UpdateTime != parseRecentDateEnd("2026-05-15") {
		t.Fatalf("expected end-of-day time, got %d", event.UpdateTime)
	}
}

func TestLifeEventBeforeCursorPrefersNewerTimeOverOlderID(t *testing.T) {
	event := LifeEvent{ID: 100, UpdateTime: 2000}

	if lifeEventBeforeCursor(event, 999999, 1000) {
		t.Fatal("newer event time must not be filtered by an older-looking id")
	}
}

func TestLifeEventBeforeCursorUsesIDWhenTimeIsEqual(t *testing.T) {
	event := LifeEvent{ID: 100, UpdateTime: 1000}

	if !lifeEventBeforeCursor(event, 100, 1000) {
		t.Fatal("same-time event with known older id should be filtered")
	}
}

func TestLifeEventBeforeCursorKeepsUnknownTimeWhenIDSourceDiffers(t *testing.T) {
	event := LifeEvent{ID: 100, UpdateTime: 0}

	if lifeEventBeforeCursor(event, 999999, 1000) {
		t.Fatal("event without time should not be filtered only by a mismatched cursor id source")
	}
}

func TestLifeEventBatchCursorTracksIgnoredEvents(t *testing.T) {
	batch := advanceLifeEventBatchCursor(LifeEventBatch{}, LifeEvent{ID: 200, Type: 8, FileID: "f1", UpdateTime: 3000})

	if batch.LastEventID != 200 || batch.LastEventTime != 3000 {
		t.Fatalf("expected raw high-water cursor, got %#v", batch)
	}
}

func TestLifeEventStartTimeAppliesLookback(t *testing.T) {
	fromTime := int64(10_000)

	if got := lifeEventStartTime(fromTime); got != fromTime-lifeEventLookbackSeconds {
		t.Fatalf("unexpected lookback start time %d", got)
	}
}

func decodeMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var row map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&row); err != nil {
		t.Fatal(err)
	}
	return row
}
