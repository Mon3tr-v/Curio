package p115

import "testing"

func TestP115RSAEncryptMatchesKnownVector(t *testing.T) {
	raw := []byte(`{"pickcode":"abc"}`)
	want := "Yh9WU41Eh1K3HugApZGRhPhHYaf1qR/J8G/Lmlz7KXV9OhleWQZMdaIX4MxGZUjJBbHjOxSEzRk3EV8sMREIcin//zKOpbYmn78rkZkT1wwMHR8BQ4v5E5jL7eLMhcO+DpgjQ86V54m0dEi5zZaKV9i2JMsHbvBshK6RhjwQKCQ="

	if got := p115RSAEncrypt(raw); got != want {
		t.Fatalf("unexpected encrypted payload\nwant %s\n got %s", want, got)
	}
}

func TestExtractDownloadURLFromAppDownurlData(t *testing.T) {
	data := map[string]any{
		"123": map[string]any{
			"url": map[string]any{
				"url": "https://cdn.example.test/movie.mkv",
			},
			"pick_code": "pc1",
		},
	}

	if got := extractDownloadURL(data); got != "https://cdn.example.test/movie.mkv" {
		t.Fatalf("unexpected download url %q", got)
	}
}
