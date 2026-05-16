package scraper

import (
	"testing"
	"time"

	"curio/internal/parser"
)

func TestPreferChineseUsesZhSGBeforeEnglish(t *testing.T) {
	got := preferChinese("The Movie", "电影", "The Movie")
	if got != "电影" {
		t.Fatalf("expected zh-SG Chinese title, got %q", got)
	}
}

func TestPreferChineseFallsBackToEnglish(t *testing.T) {
	got := preferChinese("The Movie", "", "The Movie")
	if got != "The Movie" {
		t.Fatalf("expected English fallback, got %q", got)
	}
}

func TestReleasedOnOrBeforeTodayExcludesFutureAndUnknown(t *testing.T) {
	if releasedOnOrBeforeToday("") {
		t.Fatal("empty release date should be treated as unreleased")
	}
	future := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if releasedOnOrBeforeToday(future) {
		t.Fatalf("future release date %s should be unreleased", future)
	}
	past := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if !releasedOnOrBeforeToday(past) {
		t.Fatalf("past release date %s should be released", past)
	}
}

func TestBestTVCandidateKeepsExactTitleAboveSeasonYearMatches(t *testing.T) {
	parsed := parser.Result{IsTV: true, ShowTitle: "Dark", ShowYear: 2020, SearchTitles: []string{"Dark"}, Season: 3, Episode: 1}
	candidate, ok, ambiguous := bestTVCandidate([]tmdbTVSearchItem{
		{ID: 105214, Name: "黑暗的欲望", OriginalName: "Oscuro deseo", FirstAirDate: "2020-07-15", Popularity: 200, VoteCount: 5000, AltNames: []string{"Dark Desire"}},
		{ID: 68507, Name: "黑暗物质", OriginalName: "His Dark Materials", FirstAirDate: "2019-11-03", Popularity: 200, VoteCount: 5000},
		{ID: 70523, Name: "暗黑", OriginalName: "Dark", FirstAirDate: "2017-12-01", Popularity: 100, VoteCount: 1000},
	}, parsed)
	if !ok || ambiguous || candidate.ID != 70523 {
		t.Fatalf("expected exact Dark match 70523, got candidate=%+v ok=%v ambiguous=%v", candidate, ok, ambiguous)
	}
}

func TestBestMovieCandidateKeepsExactTitleAboveSearchQueryFallback(t *testing.T) {
	parsed := parser.Result{Title: "Wonder", Year: 2017, SearchTitles: []string{"Wonder", "奇迹男孩"}}
	candidate, ok, ambiguous := bestMovieCandidate([]tmdbMovieSearchItem{
		{ID: 297762, Title: "神奇女侠", OriginalTitle: "Wonder Woman", ReleaseDate: "2017-05-30", Popularity: 500, VoteCount: 20000, QueryMatches: []string{"Wonder"}, BestRank: 1},
		{ID: 406997, Title: "奇迹男孩", OriginalTitle: "Wonder", ReleaseDate: "2017-11-13", Popularity: 50, VoteCount: 1000, BestRank: 2},
	}, parsed)
	if !ok || ambiguous || candidate.ID != 406997 {
		t.Fatalf("expected exact Wonder match 406997, got candidate=%+v ok=%v ambiguous=%v", candidate, ok, ambiguous)
	}
}

func TestBestMovieCandidateDoesNotLetSharedQueryBeatExactTitle(t *testing.T) {
	parsed := parser.Result{Title: "The Predator", Year: 2018, SearchTitles: []string{"The Predator"}}
	candidate, ok, ambiguous := bestMovieCandidate([]tmdbMovieSearchItem{
		{ID: 345940, Title: "巨齿鲨", OriginalTitle: "The Meg", ReleaseDate: "2018-08-09", Popularity: 500, VoteCount: 20000, QueryMatches: []string{"The Predator"}, BestRank: 1},
		{ID: 346910, Title: "铁血战士", OriginalTitle: "The Predator", ReleaseDate: "2018-09-05", Popularity: 50, VoteCount: 1000, BestRank: 2},
	}, parsed)
	if !ok || ambiguous || candidate.ID != 346910 {
		t.Fatalf("expected exact The Predator match 346910, got candidate=%+v ok=%v ambiguous=%v", candidate, ok, ambiguous)
	}
}

func TestBestTVCandidateUsesSearchQueryFallback(t *testing.T) {
	parsed := parser.Result{IsTV: true, ShowTitle: "Youzitsu", SearchTitles: []string{"Youzitsu"}, Season: 2, Episode: 1}
	candidate, ok, ambiguous := bestTVCandidate([]tmdbTVSearchItem{
		{ID: 72517, Name: "Classroom of the Elite", OriginalName: "ようこそ実力至上主義の教室へ", FirstAirDate: "2017-07-12", QueryMatches: []string{"Youzitsu"}, BestRank: 1},
		{ID: 291489, Name: "Incoming", OriginalName: "Incoming", FirstAirDate: "", Popularity: 1000, VoteCount: 1000},
	}, parsed)
	if !ok || ambiguous || candidate.ID != 72517 {
		t.Fatalf("expected Youzitsu query alias match 72517, got candidate=%+v ok=%v ambiguous=%v", candidate, ok, ambiguous)
	}
}
