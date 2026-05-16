package parser

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"curio/internal/models"
)

type Result struct {
	IsTV         bool
	TMDBID       int
	Title        string
	Year         int
	ShowTitle    string
	ShowYear     int
	Season       int
	Season2      string
	Episode      int
	Episode2     string
	Resolution   string
	Source       string
	VideoCodec   string
	Extension    string
	SearchTitles []string
}

type ParseError struct {
	Code    string
	Message string
}

func (e ParseError) Error() string { return e.Message }

var (
	episodeREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,2})[ ._-]*E(\d{1,3})(?:[ ._-]*(?:E|&E?|和)\d{1,3})+(?:$|[ ._\-\]\[])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,2})[ ._-]*E(\d{1,3})(?:$|[ ._\-\]\)])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,2})[ ._-]*[-–—][ ._-]*(\d{1,3})(?:v\d+)?(?:$|[ ._\-\]\[])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])(\d{1,2})x(\d{1,3})(?:$|[ ._\-\]\)])`),
		regexp.MustCompile(`(?i)第\s*(\d{1,2})\s*[季部].*?第\s*(\d{1,3})\s*[集话話]`),
	}
	episodeOnlyREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])EP?(\d{1,3})(?:$|[ ._\-\]\)])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\]\)])[-–—][ ._-]*(?:EP?)?(\d{1,4})(?:v\d+)?(?:$|[ ._\-\[\(])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\]\)])\[(\d{1,4})(?:v\d+)?\](?:$|[ ._\-\[\(])`),
		regexp.MustCompile(`(?i)第\s*(\d{1,3})\s*[集话話]`),
	}
	seasonAsEpisodeRE  = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,3})(?:$|[ ._\-\]\)])`)
	bareEpisodeRE      = regexp.MustCompile(`^\s*(\d{1,3})(?:v\d+)?\s*$`)
	miniseriesPartRE   = regexp.MustCompile(`(?i)^(.*?)[ ._-]*Mini[ ._-]?series[ ._-]*Part[ ._-]*(\d{1,2})(?:$|[ ._\-\]\)])`)
	doctorWhoSpecialRE = regexp.MustCompile(`(?i)^Doctor[ ._-]Who[ ._-]2005[ ._-]Christmas[ ._-]Special.*Twice[ ._-]Upon[ ._-]A[ ._-]Time`)
	haloForwardRE      = regexp.MustCompile(`(?i)^Halo[ ._-]*4[ ._-]*Forward[ ._-]*Unto[ ._-]*Dawn`)
	animeSeasonTitleRE = regexp.MustCompile(`(?i)^(.{3,}?)(?:[ ._-]*(?:s|season)[ ._-]*)?([2-9])$`)
	singleSeasonRE     = regexp.MustCompile(`(?i)S(?:eason)?[ ._-]*(\d{1,2})(?:$|[ ._\-\]\)])`)
	chineseSeasonRE    = regexp.MustCompile(`第\s*(\d{1,2})\s*[季部]`)
	seasonRE           = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(?:eason)?[ ._-]*(\d{1,2})(?:$|[ ._\-\]\)])|第\s*(\d{1,2})\s*[季部]`)
	seasonRangeRE      = regexp.MustCompile(`(?i)S\d{1,2}[ ._-]*-[ ._-]*S?\d{1,2}(?:$|[ ._\-\]\)])`)
	yearRE             = regexp.MustCompile(`(?:^|[ ._\-\(\[])(19\d{2}|20\d{2})(?:$|[ ._\-\)\]])`)
	yearTokenRE        = regexp.MustCompile(`19\d{2}|20\d{2}`)
	tmdbIDRE           = regexp.MustCompile(`(?i)[\[\{]\s*(?:\[\s*)?(?:tmdbid|tmdb)\s*[-=]\s*(\d+)\s*(?:\]\s*)?[\]\}]`)
	resolutionRE       = regexp.MustCompile(`(?i)\b(4320p|2160p|1080p|720p|480p|4k|8k)\b`)
	sourceRE           = regexp.MustCompile(`(?i)\b(Blu[ ._-]?ray|UltraHD|UHD|WEB[ ._-]?DL|WEBRip|HDTV|DVD|Remux|BDRip|DVDRip|HDRip)\b`)
	codecRE            = regexp.MustCompile(`(?i)\b(x264|x265|x266|H[ ._-]?264|H[ ._-]?265|H[ ._-]?266|HEVC|AVC|VVC|AV1|MPEG[ ._-]?2)\b`)
	noiseRE            = regexp.MustCompile(`(?i)\b(4320p|2160p|1080p|720p|480p|4k|8k|Blu[ ._-]?ray|UltraHD|UHD|WEB[ ._-]?DL|WEBRip|HDTV|DVD|Remux|BDRip|DVDRip|HDRip|BDMV|Complete|x264|x265|x266|H[ ._-]?264|H[ ._-]?265|H[ ._-]?266|HEVC|AVC|VVC|AV1|MPEG[ ._-]?2|10bit|12bit|AAC|AC3|DTS|DTSHD|DTS[ ._-]?X|TRUEHD|LPCM|DDP?5\.1|DDP?7\.1|Atmos|Atoms|HDR10?|SDR|DV|DoVi|Proper|Repack|Extended|Unrated|Director'?s[ ._-]?Cut|2Audio)\b`)
	leadingGroupRE     = regexp.MustCompile(`^\s*\[([^\]]{1,80})\][ ._\-]*`)
	bracketContentRE   = regexp.MustCompile(`\[[^\]]*\]`)
	bracketCaptureRE   = regexp.MustCompile(`\[([^\]]+)\]`)
	catalogBracketRE   = regexp.MustCompile(`^\s*(?:\d{1,4}|[A-Z])\[`)
	numberedCatalogRE  = regexp.MustCompile(`^\s*\d{1,4}[.)、．]\s*`)
	releaseSuffixRE    = regexp.MustCompile(`(?i)[ ._\-]+(FGT|FRDS|SGNB|CHDBits|RARBG|YIFY|YTS(?:\.[A-Z]+)?|WiKi|OurBits|MTeam|CMCT|MNHD|PTer|HDSky|HDHome|BHDStudio|HDChina|ADE|CtrlHD|DON|FraMeSToR|TERMiNAL|NTb|FLUX|HONE|MeGusta|ION10|ETHEL|Tigole|QxR|Vyndros|Judas|EMBER|ASW|SubsPlease|Erai[ ._-]?raws|Lilith[ ._-]?Raws|NC[ ._-]?Raws|LoliHouse|ANi)$`)
	releaseGroupTagRE  = regexp.MustCompile(`(?i)^(ANi|SubsPlease|Erai[ ._-]?raws|Lilith[ ._-]?Raws|NC[ ._-]?Raws|Ohys[ ._-]?Raws|Skymoon[ ._-]?Raws|Leopard[ ._-]?Raws|LoliHouse|Nekomoe|Moozzi2|ReinForce|UCCUSS|Beatrice[ ._-]?Raws|SweetSub|Sakurato|Airota|SumiSora|GM[ ._-]?Team|HYSUB|KTXP|Kamigami|DMG|CASO|DHR|XKsub|MCE|VARYG|VCB[ ._-]?Studio|T[ ._-]?H[ ._-]?X|喵萌奶茶屋|桜都字幕组)(?:\b|[ &+_-])`)
	bracketNoiseWordRE = regexp.MustCompile(`(?i)(SGNB|CHDBits|UHD|BDJ|DIY|菜单|字幕|音轨|国语|国配|简繁|特效|收藏|未完待续|原盘|修复|新增按钮)`)
	shortTagRE         = regexp.MustCompile(`(?i)^(Baha|B-Global|CR|AMZN|NF|Bilibili|CHS|CHT|GB|BIG5|AVC|AAC|HEVC|ASS|MP4|MKV|WEB|WEB-DL|TV|繁中|简中)$`)
	hashTagRE          = regexp.MustCompile(`(?i)^[a-f0-9]{6,16}$`)
	asciiTailRE        = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9 ._'’:&,+!\-]*$`)
	regionNoteRE       = regexp.MustCompile(`(?i)[\(（][^()（）]*(港|台|臺|内地|大陆|中國|中国|英|日|韩|韓|粤|粵|配音|字幕)[^()（）]*[\)）]`)
	spaceRE            = regexp.MustCompile(`\s+`)
)

func Parse(filename string) (Result, error) {
	return ParsePath(filename)
}

func ParsePath(pathValue string) (Result, error) {
	parts := splitPath(pathValue)
	base := ""
	if len(parts) > 0 {
		base = parts[len(parts)-1]
	}
	baseNoExt, ext := trimExtension(base)
	parentTitle := titleFromParents(parts)
	sourceText := strings.Join(append([]string{baseNoExt}, reverse(parts[:max(0, len(parts)-1)])...), " ")
	baseNoExt, baseID := extractTMDBID(baseNoExt)
	parentTitle, parentID := extractTMDBID(parentTitle)
	baseNoExt = normalizeReleaseName(baseNoExt)
	parentTitle = normalizeReleaseName(parentTitle)

	result := Result{
		TMDBID:     firstNonZero(baseID, parentID),
		Resolution: "Unknown",
		Source:     valueOrUnknown(canonicalToken(findToken(sourceRE, sourceText))),
		VideoCodec: "Unknown",
		Extension:  ext,
	}
	if tv, ok, err := parseTV(baseNoExt, parentTitle, parts, result); ok || err != nil {
		return tv, err
	}
	return parseMovie(baseNoExt, parentTitle, result)
}

func parseTV(base, parentTitle string, parts []string, result Result) (Result, bool, error) {
	for _, re := range episodeREs {
		match := re.FindStringSubmatchIndex(base)
		if match == nil {
			continue
		}
		season := atoi(firstMatch(base, match, 2, 3))
		episode := atoi(firstMatch(base, match, 4, 5))
		return tvResult(base[:match[0]], parentTitle, result, season, episode, false)
	}
	for _, re := range episodeOnlyREs {
		match := re.FindStringSubmatchIndex(base)
		if match == nil {
			continue
		}
		season := seasonFromParts(parts)
		if season == 0 {
			season = 1
		}
		episode := atoi(firstMatch(base, match, 2, 3))
		return tvResult(base[:match[0]], parentTitle, result, season, episode, seasonFromParts(parts) == 0)
	}
	if match := bareEpisodeRE.FindStringSubmatch(base); len(match) > 1 {
		if season := seasonFromParts(parts); season > 0 {
			return tvResult("", parentTitle, result, season, atoi(match[1]), false)
		}
	}
	if match := seasonAsEpisodeRE.FindStringSubmatchIndex(base); match != nil {
		if season := seasonFromParts(parts); season > 0 {
			return tvResult(base[:match[0]], parentTitle, result, season, atoi(firstMatch(base, match, 2, 3)), false)
		}
	}
	if match := miniseriesPartRE.FindStringSubmatch(base); len(match) > 2 {
		return tvResult(match[1], parentTitle, result, 1, atoi(match[2]), false)
	}
	if doctorWhoSpecialRE.MatchString(base) {
		return tvResult("Doctor Who", parentTitle, result, 0, 0, false)
	}
	if haloForwardRE.MatchString(base) {
		return tvResult("Halo 4 Forward Unto Dawn", parentTitle, result, 1, 1, false)
	}
	return result, false, nil
}

func tvResult(rawTitle, parentTitle string, result Result, season, episode int, inferSeasonFromTitle bool) (Result, bool, error) {
	originalRawTitle := rawTitle
	if inferSeasonFromTitle {
		if title, inferredSeason := splitAnimeSeasonTitle(rawTitle); title != "" && inferredSeason > 0 {
			rawTitle = title
			season = inferredSeason
		}
	}
	title := cleanTitle(rawTitle)
	rawCandidates := titleCandidates(rawTitle, originalRawTitle)
	if preferred := preferredTVTitleCandidate(title, rawCandidates); preferred != "" {
		title = preferred
	}
	parentCandidates := seriesTitleCandidates(parentTitle)
	usedParentTitle := false
	if weakTitle(title) {
		title = firstString(parentCandidates...)
		usedParentTitle = title != ""
	}
	if weakTitle(title) {
		title = cleanName(parentTitle)
		usedParentTitle = title != ""
	}
	if title == "" {
		return result, true, ParseError{Code: models.ErrParseTitleEmpty, Message: "剧集标题为空"}
	}
	result.IsTV = true
	result.ShowTitle = title
	result.ShowYear = yearFromText(parentTitle)
	result.Season = season
	result.Episode = episode
	result.Season2 = two(season)
	result.Episode2 = two(episode)
	searchTitles := rawCandidates
	if usedParentTitle || len(searchTitles) == 0 {
		searchTitles = append(searchTitles, parentCandidates...)
	}
	result.SearchTitles = uniqueNonEmpty(append([]string{title}, searchTitles...)...)
	return result, true, nil
}

func parseMovie(base, parentTitle string, result Result) (Result, error) {
	year, titleEnd := lastYear(base)
	titleSource := base
	if year == 0 {
		year, titleEnd = lastYear(parentTitle)
		if year > 0 {
			titleSource = parentTitle
		}
	}
	rawTitle := base
	if year > 0 {
		rawTitle = titleSource[:titleEnd]
	}
	candidates := titleCandidates(rawTitle, base, parentTitle)
	title := firstString(candidates...)
	if title == "" {
		title = cleanTitle(base)
	}
	if weakTitle(title) {
		title = cleanName(parentTitle)
	}
	if title == "" {
		return result, ParseError{Code: models.ErrParseTitleEmpty, Message: "电影标题为空"}
	}
	result.Title = title
	result.Year = year
	result.SearchTitles = uniqueNonEmpty(append([]string{title}, candidates...)...)
	return result, nil
}

func splitPath(value string) []string {
	values := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
	parts := make([]string, 0, len(values))
	for _, part := range values {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func trimExtension(value string) (string, string) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(value)), ".")
	if ext == "" {
		return value, ""
	}
	return strings.TrimSuffix(value, filepath.Ext(value)), ext
}

func titleFromParents(parts []string) string {
	if len(parts) < 2 {
		return ""
	}
	parentIndex := len(parts) - 2
	if isSeasonContainer(parts[parentIndex]) {
		if parent := firstTitleParent(parts, parentIndex-1); parent != "" {
			return parent
		}
	}
	return firstTitleParent(parts, parentIndex)
}

func firstTitleParent(parts []string, start int) string {
	for i := start; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" || isNonTitlePathPart(part) || isSeasonDir(part) {
			continue
		}
		return part
	}
	return ""
}

func isSeasonDir(value string) bool {
	value = strings.TrimSpace(value)
	return seasonRE.MatchString(value) && cleanTitle(value) == ""
}

func isSeasonContainer(value string) bool {
	value = strings.TrimSpace(value)
	return seasonRE.MatchString(value) || seasonRangeRE.MatchString(value)
}

func extractTMDBID(value string) (string, int) {
	id := 0
	cleaned := tmdbIDRE.ReplaceAllStringFunc(value, func(match string) string {
		if id == 0 {
			sub := tmdbIDRE.FindStringSubmatch(match)
			if len(sub) > 1 {
				id = atoi(sub[1])
			}
		}
		return " "
	})
	return cleaned, id
}

func lastYear(value string) (int, int) {
	currentLimit := time.Now().Year() + 2
	year := 0
	titleEnd := 0
	for _, match := range yearTokenRE.FindAllStringIndex(value, -1) {
		if !validYearBoundary(value, match[0], match[1]) {
			continue
		}
		candidate := atoi(value[match[0]:match[1]])
		if candidate >= 1900 && candidate <= currentLimit {
			year = candidate
			titleEnd = titleEndBeforeYear(value, match[0])
		}
	}
	return year, titleEnd
}

func yearFromText(value string) int {
	year, _ := firstYear(value)
	return year
}

func firstYear(value string) (int, int) {
	currentLimit := time.Now().Year() + 2
	for _, match := range yearTokenRE.FindAllStringIndex(value, -1) {
		if !validYearBoundary(value, match[0], match[1]) {
			continue
		}
		candidate := atoi(value[match[0]:match[1]])
		if candidate >= 1900 && candidate <= currentLimit {
			return candidate, titleEndBeforeYear(value, match[0])
		}
	}
	return 0, 0
}

func validYearBoundary(value string, start, end int) bool {
	return (start == 0 || isYearSeparator(value[start-1])) && (end == len(value) || isYearSeparator(value[end]))
}

func titleEndBeforeYear(value string, start int) int {
	for start > 0 && isYearSeparator(value[start-1]) {
		start--
	}
	return start
}

func isYearSeparator(b byte) bool {
	return b == ' ' || b == '.' || b == '_' || b == '-' || b == '(' || b == ')' || b == '[' || b == ']'
}

func seasonFromContext(value string) int {
	matches := seasonRE.FindStringSubmatch(value)
	if len(matches) == 0 {
		return 0
	}
	for _, match := range matches[1:] {
		if match != "" {
			return atoi(match)
		}
	}
	return 0
}

func seasonFromParts(parts []string) int {
	for i := len(parts) - 2; i >= 0; i-- {
		if seasonRangeRE.MatchString(parts[i]) {
			continue
		}
		if season := seasonFromSingleSeasonText(parts[i]); season > 0 {
			return season
		}
	}
	return 0
}

func seasonFromSingleSeasonText(value string) int {
	if match := singleSeasonRE.FindStringSubmatch(value); len(match) > 1 {
		return atoi(match[1])
	}
	if match := chineseSeasonRE.FindStringSubmatch(value); len(match) > 1 {
		return atoi(match[1])
	}
	return 0
}

func splitAnimeSeasonTitle(value string) (string, int) {
	cleaned := normalizeTitleForSeasonSuffix(value)
	match := animeSeasonTitleRE.FindStringSubmatch(cleaned)
	if len(match) < 3 || !hasEnoughTitleLetters(match[1]) {
		return "", 0
	}
	title := cleanTitle(match[1])
	if weakTitle(title) {
		return "", 0
	}
	return title, atoi(match[2])
}

func normalizeTitleForSeasonSuffix(value string) string {
	value = normalizeReleaseName(value)
	value = removeBracketNoise(value)
	value = noiseRE.ReplaceAllString(value, " ")
	value = strings.NewReplacer("／", " ", "/", " ", "｜", " ", "|", " ", ".", " ", "_", " ", "-", " ").Replace(value)
	value = spaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func hasEnoughTitleLetters(value string) bool {
	count := 0
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '\u4e00' && r <= '\u9fff') || (r >= '\u3400' && r <= '\u4dbf') {
			count++
		}
	}
	return count >= 4
}

func isNonTitlePathPart(value string) bool {
	normalized := strings.ToLower(cleanTitle(value))
	switch normalized {
	case "", "cd2", "115open", "115", "curio", "data", "media", "library",
		"incoming", "staging", "failed", "fail", "failure", "incomplete collections",
		"movies", "movie", "tv", "series", "shows", "collections", "collection",
		"subs", "sub", "subtitle", "subtitles":
		return true
	default:
		return false
	}
}

func firstMatch(value string, match []int, startIdx, endIdx int) string {
	if startIdx >= len(match) || endIdx >= len(match) || match[startIdx] < 0 || match[endIdx] < 0 {
		return ""
	}
	return value[match[startIdx]:match[endIdx]]
}

func normalizeReleaseName(value string) string {
	value = strings.TrimSpace(value)
	for {
		before := value
		value = stripLeadingReleaseGroups(value)
		value = stripCatalogBracketPrefix(value)
		value = numberedCatalogRE.ReplaceAllString(value, "")
		value = stripKnownReleaseSuffixes(value)
		value = strings.TrimSpace(value)
		if value == before {
			return value
		}
	}
}

func stripCatalogBracketPrefix(value string) string {
	if loc := catalogBracketRE.FindStringIndex(value); loc != nil && loc[0] == 0 {
		return "[" + value[loc[1]:]
	}
	return value
}

func stripLeadingReleaseGroups(value string) string {
	for {
		match := leadingGroupRE.FindStringSubmatchIndex(value)
		if match == nil {
			return value
		}
		tag := value[match[2]:match[3]]
		if !isReleaseGroupTag(tag) {
			return value
		}
		value = strings.TrimSpace(value[match[1]:])
	}
}

func stripKnownReleaseSuffixes(value string) string {
	for {
		before := value
		if at := strings.LastIndex(value, "@"); at > 0 {
			value = strings.TrimRight(value[:at], " ._-￡")
		}
		value = releaseSuffixRE.ReplaceAllString(value, "")
		value = strings.TrimSpace(value)
		if value == before {
			return value
		}
	}
}

func removeBracketNoise(value string) string {
	return bracketContentRE.ReplaceAllStringFunc(value, func(match string) string {
		content := strings.TrimSpace(strings.Trim(match, "[]"))
		if isNoiseBracket(content) {
			return " "
		}
		return match
	})
}

func isNoiseBracket(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return true
	}
	if isReleaseGroupTag(content) || bracketNoiseWordRE.MatchString(content) || hashTagRE.MatchString(content) || shortTagRE.MatchString(content) {
		return true
	}
	if !containsHan(content) && noiseRE.MatchString(content) {
		return true
	}
	return false
}

func isReleaseGroupTag(value string) bool {
	value = strings.TrimSpace(value)
	return releaseGroupTagRE.MatchString(value)
}

func titleCandidates(values ...string) []string {
	candidates := make([]string, 0, len(values)*3)
	for _, value := range values {
		if title := englishTitleCandidate(value); title != "" {
			candidates = append(candidates, title)
		}
		candidates = append(candidates, localizedTitleCandidates(value)...)
		if title := cleanTitle(value); title != "" {
			candidates = append(candidates, title)
		}
	}
	for _, title := range append([]string(nil), candidates...) {
		candidates = append(candidates, titleVariants(title)...)
	}
	return uniqueNonEmpty(candidates...)
}

func seriesTitleCandidates(value string) []string {
	cleaned := cleanSeriesName(value)
	return uniqueNonEmpty(append(titleCandidates(cleaned), titleCandidates(value)...)...)
}

func cleanSeriesName(value string) string {
	value = normalizeReleaseName(value)
	value = seasonRangeRE.ReplaceAllString(value, " ")
	value = seasonRE.ReplaceAllString(value, " ")
	if year, titleEnd := firstYear(value); year > 0 && titleEnd > 0 {
		value = value[:titleEnd]
	}
	return strings.TrimSpace(value)
}

func englishTitleCandidate(value string) string {
	value = normalizeReleaseName(value)
	if index := strings.LastIndex(value, "]"); index >= 0 && index+1 < len(value) && containsASCII(value[index+1:]) {
		if title := englishTitleFromText(value[index+1:]); title != "" {
			return title
		}
	}
	return englishTitleFromText(value)
}

func englishTitleFromText(value string) string {
	value = removeBracketNoise(value)
	value = bracketContentRE.ReplaceAllString(value, " ")
	value = regionNoteRE.ReplaceAllString(value, " ")
	value = noiseRE.ReplaceAllString(value, " ")
	value = strings.NewReplacer("／", " ", "/", " ", "｜", " ", "|", " ", "（", " ", "）", " ").Replace(value)
	value = spaceRE.ReplaceAllString(value, " ")
	match := asciiTailRE.FindString(strings.TrimSpace(value))
	if strings.TrimSpace(match) == "" {
		return ""
	}
	title := cleanTitle(match)
	if title == "" || !containsASCII(title) {
		return ""
	}
	return title
}

func localizedTitleCandidates(value string) []string {
	value = normalizeReleaseName(value)
	result := make([]string, 0, 3)
	if index := strings.Index(value, "["); index > 0 {
		result = appendAliasCandidates(result, value[:index])
	}
	for _, match := range bracketCaptureRE.FindAllStringSubmatch(value, -1) {
		if len(match) > 1 && !isNoiseBracket(match[1]) {
			result = appendAliasCandidates(result, match[1])
		}
	}
	if containsHan(value) {
		withoutASCII := asciiTailRE.ReplaceAllString(value, " ")
		result = appendAliasCandidates(result, withoutASCII)
	}
	return uniqueNonEmpty(result...)
}

func appendAliasCandidates(result []string, value string) []string {
	value = regionNoteRE.ReplaceAllString(value, " ")
	if title := broadcastTitleCandidate(value); title != "" {
		result = append(result, title)
	}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '／' || r == '|' || r == '｜' || r == '、'
	}) {
		if title := cleanTitle(part); title != "" {
			if title := broadcastTitleCandidate(title); title != "" {
				result = append(result, title)
			}
			result = append(result, title)
		}
	}
	return result
}

func findToken(re *regexp.Regexp, value string) string {
	match := re.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func cleanTitle(value string) string {
	value, _ = extractTMDBID(value)
	value = normalizeReleaseName(value)
	value = removeBracketNoise(value)
	value = noiseRE.ReplaceAllString(value, " ")
	value = seasonRangeRE.ReplaceAllString(value, " ")
	value = seasonRE.ReplaceAllString(value, " ")
	value = regionNoteRE.ReplaceAllString(value, " ")
	value = strings.NewReplacer(
		"／", " ", "/", " ", "｜", " ", "|", " ", "：", " ", ":", " ",
		".", " ", "_", " ", "-", " ", "–", " ", "—", " ",
		"(", " ", ")", " ", "（", " ", "）", " ", "[", " ", "]", " ", "{", " ", "}", " ",
	).Replace(value)
	value = numberedCatalogRE.ReplaceAllString(value, " ")
	value = spaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func cleanName(value string) string {
	year, titleEnd := lastYear(value)
	if seasonRangeRE.MatchString(value) {
		year, titleEnd = firstYear(value)
	}
	if year > 0 && titleEnd > 0 {
		return cleanTitle(value[:titleEnd])
	}
	return cleanTitle(value)
}

func titleVariants(title string) []string {
	normalized := strings.ToLower(strings.TrimSpace(title))
	var variants []string
	for _, suffix := range []string{" us", " uk", " jp", " jpn"} {
		if strings.HasSuffix(normalized, suffix) {
			variants = append(variants, strings.TrimSpace(title[:len(title)-len(suffix)]))
		}
	}
	if strings.HasSuffix(normalized, " m d") {
		variants = append(variants, strings.TrimSpace(title[:len(title)-4]))
	}
	if strings.HasSuffix(normalized, " dc") {
		variants = append(variants, strings.TrimSpace(title[:len(title)-3]))
	}
	if normalized == "the end of the fucking world" {
		variants = append(variants, "The End of the F***ing World")
	}
	if strings.HasPrefix(normalized, "ringu ") {
		variants = append(variants, "Ring "+strings.TrimSpace(title[6:]))
	}
	if strings.Contains(normalized, "sorcerers stone") {
		variants = append(variants, strings.ReplaceAll(title, "Sorcerers Stone", "Philosopher's Stone"))
	}
	if strings.Contains(normalized, "scanner cop 2") {
		variants = append(variants, "Scanner Cop II")
	}
	if strings.Contains(normalized, "halo 4 forward unto dawn") {
		variants = append(variants, "Halo 4: Forward Unto Dawn")
	}
	if strings.Contains(normalized, "kaamelott first installment") {
		variants = append(variants, "Kaamelott The First Chapter")
	}
	if strings.Contains(normalized, "star wars episode vii the force awakens") {
		variants = append(variants, "Star Wars The Force Awakens")
	}
	if strings.Contains(normalized, "justice league snyders cut") {
		variants = append(variants, "Zack Snyder's Justice League")
	}
	if strings.EqualFold(title, "Ringu 0") {
		variants = append(variants, "Ring 0 Birthday")
	}
	if strings.Contains(normalized, "marvels agents of s h i e l d") {
		variants = append(variants, "Marvel's Agents of S.H.I.E.L.D.")
	}
	return variants
}

func preferredTVTitleCandidate(current string, candidates []string) string {
	for _, candidate := range candidates {
		if candidate == "" || weakTitle(candidate) || !containsHan(candidate) {
			continue
		}
		if current == "" || !containsHan(current) || cleanerTitle(candidate, current) {
			return candidate
		}
	}
	return ""
}

func cleanerTitle(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	current = strings.TrimSpace(current)
	if candidate == "" || current == "" || candidate == current {
		return false
	}
	if strings.Contains(current, candidate) {
		return true
	}
	candidateLen := runeLen(candidate)
	currentLen := runeLen(current)
	return candidateLen >= 2 && candidateLen+4 < currentLen
}

func broadcastTitleCandidate(value string) string {
	cleaned := cleanTitle(value)
	if cleaned == "" || !containsHan(cleaned) {
		return ""
	}
	for _, marker := range []string{
		"重温经典频道",
		"少儿频道",
		"动漫频道",
		"动画频道",
		"电视剧频道",
		"电影频道",
		"综合频道",
		"高清频道",
		"纪录频道",
		"卫视频道",
		"频道",
		"电视台",
		"卫视",
	} {
		index := strings.Index(cleaned, marker)
		if index < 0 {
			continue
		}
		tail := strings.TrimSpace(cleaned[index+len(marker):])
		if tail != "" && containsHan(tail) && !weakTitle(tail) {
			return tail
		}
	}
	return ""
}

func runeLen(value string) int {
	count := 0
	for range value {
		count++
	}
	return count
}

func weakTitle(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "" || normalized == "sample" || normalized == "cd1" || normalized == "cd2" || isReleaseGroupTag(normalized)
}

func canonicalToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, ".", "-")
	if strings.EqualFold(value, "web-dl") {
		return "WEB-DL"
	}
	if strings.EqualFold(value, "hdtv") {
		return "HDTV"
	}
	if strings.EqualFold(value, "uhd") {
		return "UHD"
	}
	if strings.EqualFold(value, "ultrahd") {
		return "UHD"
	}
	if strings.EqualFold(value, "blu-ray") {
		return "BluRay"
	}
	if strings.EqualFold(value, "bluray") {
		return "BluRay"
	}
	if strings.EqualFold(value, "remux") {
		return "Remux"
	}
	return value
}

func canonicalCodec(value string) string {
	value = canonicalToken(value)
	value = strings.ReplaceAll(value, "-", "")
	if strings.EqualFold(value, "H264") || strings.EqualFold(value, "AVC") || strings.EqualFold(value, "x264") {
		return "AVC"
	}
	if strings.EqualFold(value, "H265") || strings.EqualFold(value, "HEVC") || strings.EqualFold(value, "x265") {
		return "HEVC"
	}
	if strings.EqualFold(value, "H266") || strings.EqualFold(value, "VVC") || strings.EqualFold(value, "x266") {
		return "VVC"
	}
	if strings.EqualFold(value, "MPEG2") {
		return "MPEG-2"
	}
	return value
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return value
}

func two(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueNonEmpty(values ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsASCII(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
}

func containsHan(value string) bool {
	for _, r := range value {
		if (r >= '\u4e00' && r <= '\u9fff') || (r >= '\u3400' && r <= '\u4dbf') {
			return true
		}
	}
	return false
}

func reverse(values []string) []string {
	result := make([]string, 0, len(values))
	for i := len(values) - 1; i >= 0; i-- {
		result = append(result, values[i])
	}
	return result
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
