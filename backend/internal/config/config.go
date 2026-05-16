package config

import (
	"os"
	"strings"
)

type Config struct {
	Addr           string
	DatabaseURL    string
	RedisAddr      string
	RedisPassword  string
	AdminToken     string
	TMDBAPIKey     string
	NetworkProxy   string
	CloudDriveAddr string
	FrontendOrigin string
	FrontendDir    string
	DataRoot       string
}

func Load() Config {
	dataRoot := env("CURIO_DATA_ROOT", "/data/Curio")
	return Config{
		Addr:           env("SERVER_ADDR", ":8080"),
		DatabaseURL:    env("DATABASE_URL", "postgres://curio:curio@db:5432/curio?sslmode=disable"),
		RedisAddr:      env("REDIS_ADDR", "redis:6379"),
		RedisPassword:  os.Getenv("REDIS_PASSWORD"),
		AdminToken:     os.Getenv("CURIO_ADMIN_TOKEN"),
		TMDBAPIKey:     os.Getenv("TMDB_API_KEY"),
		NetworkProxy:   firstEnv("NETWORK_PROXY", "TMDB_PROXY", "HTTPS_PROXY", "HTTP_PROXY"),
		CloudDriveAddr: env("CLOUDDRIVE_ADDR", "http://localhost:19798"),
		FrontendOrigin: env("FRONTEND_ORIGIN", "*"),
		FrontendDir:    env("FRONTEND_DIR", "/app/public"),
		DataRoot:       strings.TrimRight(dataRoot, "/\\"),
	}
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
