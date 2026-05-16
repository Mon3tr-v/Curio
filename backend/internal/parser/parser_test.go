package parser

import "testing"

func TestParseMovie(t *testing.T) {
	result, err := Parse("Avatar.2009.1080p.BluRay.x264.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Avatar" || result.Year != 2009 || result.Resolution != "Unknown" || result.Source != "BluRay" || result.VideoCodec != "Unknown" {
		t.Fatalf("unexpected movie parse: %+v", result)
	}
}

func TestParseTVEpisode(t *testing.T) {
	result, err := Parse("The.Last.of.Us.S01E03.1080p.WEB-DL.x265.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "The Last of Us" || result.Season != 1 || result.Episode != 3 || result.Season2 != "01" || result.Episode2 != "03" {
		t.Fatalf("unexpected episode parse: %+v", result)
	}
}

func TestParseTVEpisodeAlternateFormat(t *testing.T) {
	result, err := Parse("Show.Name.1x03.720p.HDTV.H264.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Show Name" || result.Season != 1 || result.Episode != 3 {
		t.Fatalf("unexpected alternate episode parse: %+v", result)
	}
}

func TestParseTVEpisodeFromSeasonFolder(t *testing.T) {
	result, err := ParsePath("/media/The Last of Us (2023)/Season 1/E03.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "The Last of Us" || result.ShowYear != 2023 || result.Season != 1 || result.Episode != 3 {
		t.Fatalf("unexpected path episode parse: %+v", result)
	}
}

func TestParseTVEpisodeFromNamedSeasonFolder(t *testing.T) {
	result, err := ParsePath("/media/了不起的麦瑟尔夫人.S01-S03.2017-2019.1080p.WEB-DL/了不起的麦瑟尔夫人.S03.2019.1080p.WEB-DL/The.Marvelous.Mrs.Maisel.S03E01.2019.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "The Marvelous Mrs Maisel" || result.ShowYear != 2017 || result.Season != 3 || result.Episode != 1 {
		t.Fatalf("unexpected named season folder parse: %+v", result)
	}
}

func TestParseTVEpisodeUsesSeriesYearFromSeasonRangeParent(t *testing.T) {
	result, err := ParsePath("/media/Dark.S01-S03.2017-2020.1080p.WEB-DL/Dark.S02.2019.1080p.WEB-DL/Dark.S02E01.2019.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Dark" || result.ShowYear != 2017 || result.Season != 2 || result.Episode != 1 {
		t.Fatalf("unexpected dark season parse: %+v", result)
	}
}

func TestParseChineseEpisode(t *testing.T) {
	result, err := Parse("庆余年.第2季.第03集.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "庆余年" || result.Season != 2 || result.Episode != 3 {
		t.Fatalf("unexpected chinese episode parse: %+v", result)
	}
}

func TestParseMovieWithTMDBIDFromFolder(t *testing.T) {
	result, err := ParsePath("/media/长安三万里 (2023) [tmdbid=1110037]/长安三万里.2160p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "长安三万里" || result.Year != 2023 || result.TMDBID != 1110037 {
		t.Fatalf("unexpected tmdb movie parse: %+v", result)
	}
}

func TestParseMovieWithoutYear(t *testing.T) {
	result, err := Parse("Inception.1080p.BluRay.x264.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Inception" || result.Year != 0 {
		t.Fatalf("unexpected no-year movie parse: %+v", result)
	}
}

func TestParseFGTFolderMovie(t *testing.T) {
	result, err := ParsePath("/115open/2267部2160p remux FGT/0[007.大破天幕杀机]Skyfall.2012.2160p.BluRay.REMUX.HEVC.DTS-HD.MA.5.1-FGT/Skyfall.2012.2160p.BluRay.REMUX.HEVC.DTS-HD.MA.5.1-FGT.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Skyfall" || result.Year != 2012 {
		t.Fatalf("unexpected fgt movie parse: %+v", result)
	}
}

func TestParseSGNBMovie(t *testing.T) {
	result, err := Parse("001.异形：契约／异形：圣约(港／台) [SGNB首部UHD原盘DIY BDJ菜单修改 次时代国语音轨 简繁英特效四字幕]Alien Covenant 2017 ULTRAHD Blu-ray 2160p HEVC Atoms TrueHD 7.1-SGnb@CHDBits.iso")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Alien Covenant" || result.Year != 2017 {
		t.Fatalf("unexpected sgnb movie parse: %+v", result)
	}
}

func TestParseSGNBMovieWithYearInChineseTitle(t *testing.T) {
	result, err := Parse("224.神奇女侠1984 [SGNB第224部UHD原盘中文字幕DIY]Wonder Woman 1984 2020 ULTRAHD BluRay 2160p HEVC Atmos TrueHD 7.1-sGnb@CHDBits.iso")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsTV || result.Title != "Wonder Woman 1984" || result.Year != 2020 {
		t.Fatalf("unexpected sgnb year-in-title parse: %+v", result)
	}
}

func TestParseNyaaEpisode(t *testing.T) {
	result, err := Parse("[ANi] Kusuriya no Hitorigoto - 01 [1080P][Baha][WEB-DL][AAC AVC][CHT].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Kusuriya no Hitorigoto" || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected nyaa episode parse: %+v", result)
	}
}

func TestParseNyaaChineseGroupEpisode(t *testing.T) {
	result, err := Parse("[喵萌奶茶屋&LoliHouse] 葬送的芙莉莲 - 05 [WebRip 1080p HEVC-10bit AAC][简繁日内封字幕].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "葬送的芙莉莲" || result.Season != 1 || result.Episode != 5 {
		t.Fatalf("unexpected nyaa chinese group episode parse: %+v", result)
	}
}

func TestParseCloudRootDoesNotUseIncomingAsShowTitle(t *testing.T) {
	result, err := ParsePath("/115open/incoming/[Nekomoe kissaten&VCB-Studio] Youkoso Jitsuryoku Shijou Shugi no Kyoushitsu e [05][Ma10p_1080p][x265_flac].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Youkoso Jitsuryoku Shijou Shugi no Kyoushitsu e" || result.Season != 1 || result.Episode != 5 {
		t.Fatalf("unexpected incoming-root anime parse: %+v", result)
	}
	for _, title := range result.SearchTitles {
		if title == "Incoming" || title == "incoming" {
			t.Fatalf("incoming directory leaked into search titles: %+v", result.SearchTitles)
		}
	}
}

func TestParseBroadcastPrefixedChineseTVTitle(t *testing.T) {
	result, err := ParsePath("/115open/incoming/[中国广电重温经典频道 虹猫蓝兔七侠传].CWJDTV.Legend.of.Howie,Landau.and.the.Seven.Swordsmen.2006.S01.Complete.1080i.HDTV.AVC.DD5.1-QHstudIo/[中国广电重温经典频道 虹猫蓝兔七侠传].CWJDTV.Legend.of.Howie,Landau.and.the.Seven.Swordsmen.2006.S01E01.1080i.HDTV.AVC.DD5.1-QHstudIo.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "虹猫蓝兔七侠传" || result.ShowYear != 2006 || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected broadcast-prefixed tv parse: %+v", result)
	}
	if len(result.SearchTitles) == 0 || result.SearchTitles[0] != "虹猫蓝兔七侠传" {
		t.Fatalf("expected exact chinese title first, got %+v", result.SearchTitles)
	}
}

func TestParseAnimeAbbreviationSeasonSuffix(t *testing.T) {
	result, err := ParsePath("/115open/incoming/[DMG&VCB-Studio] Youzitsu2 [01][Ma10p_1080p][x265_flac_aac].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Youzitsu" || result.Season != 2 || result.Episode != 1 {
		t.Fatalf("unexpected anime season suffix parse: %+v", result)
	}
}

func TestParseTHXVCBReleaseGroup(t *testing.T) {
	result, err := Parse("[T.H.X&VCB-Studio] Hyouka [01][Ma10p_1080p][x265_flac_aac].mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Hyouka" || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected thx vcb parse: %+v", result)
	}
}

func TestParseMultiEpisode(t *testing.T) {
	result, err := Parse("Stargate.Atlantis.S01E01E02.2004.Blu-ray.x265.AC3.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Stargate Atlantis" || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected multi episode parse: %+v", result)
	}
}

func TestParseBareEpisodeFromSeasonFolder(t *testing.T) {
	parts := splitPath("/media/神盾局特工S01-S05.Marvels.Agents.of.S.H.I.E.L.D.2013-2017/神盾局特工S01.Marvels.Agents.of.S.H.I.E.L.D.2013/07.mkv")
	if season := seasonFromParts(parts); season != 1 {
		t.Fatalf("unexpected nearest season: %d rangeParent=%v singleParent=%v parts=%v", season, seasonRangeRE.MatchString(parts[2]), singleSeasonRE.FindStringSubmatch(parts[2]), parts)
	}
	result, err := ParsePath("/media/神盾局特工S01-S05.Marvels.Agents.of.S.H.I.E.L.D.2013-2017/神盾局特工S01.Marvels.Agents.of.S.H.I.E.L.D.2013/07.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.Season != 1 || result.Episode != 7 || result.ShowTitle != "Marvels Agents of S H I E L D" {
		t.Fatalf("unexpected bare episode parse: %+v", result)
	}
}

func TestParseSeasonNumberAsEpisode(t *testing.T) {
	result, err := ParsePath("/media/难以置信.Unbelievable.S01/Unbelievable.S06.1080p.WEB-DL.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Unbelievable" || result.Season != 1 || result.Episode != 6 {
		t.Fatalf("unexpected season-as-episode parse: %+v", result)
	}
}

func TestParseMiniseriesPart(t *testing.T) {
	result, err := Parse("Battlestar.Galactica.Miniseries.Part.2.2003.1080p.Blu-ray.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Battlestar Galactica" || result.Season != 1 || result.Episode != 2 {
		t.Fatalf("unexpected miniseries parse: %+v", result)
	}
}

func TestParseEpisodeZero(t *testing.T) {
	result, err := Parse("Sherlock.S01E00.2010.1080p.Blu-ray.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Sherlock" || result.Season != 1 || result.Episode != 0 || result.Episode2 != "00" {
		t.Fatalf("unexpected episode zero parse: %+v", result)
	}
}

func TestParseHaloForwardUntoDawn(t *testing.T) {
	result, err := Parse("Halo.4.Forward.Unto.Dawn.2012.2160p.BluRay.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTV || result.ShowTitle != "Halo 4 Forward Unto Dawn" || result.Season != 1 || result.Episode != 1 {
		t.Fatalf("unexpected halo parse: %+v", result)
	}
}
