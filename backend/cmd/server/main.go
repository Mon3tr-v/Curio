package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"curio/internal/api"
	"curio/internal/collection"
	"curio/internal/config"
	"curio/internal/embyproxy"
	"curio/internal/models"
	"curio/internal/naming"
	"curio/internal/p115"
	"curio/internal/repository"
	"curio/internal/scraper"
	"curio/internal/worker"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()
	db := connectPostgres(ctx, cfg.DatabaseURL)
	defer db.Close()
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword})
	store := repository.New(db)
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	defaultDirs := models.DirectoryConfig{
		IncomingPath:              filepath.Join(cfg.DataRoot, "incoming"),
		StagingPath:               filepath.Join(cfg.DataRoot, "staging"),
		FailedPath:                filepath.Join(cfg.DataRoot, "failed"),
		IncompleteCollectionsPath: filepath.Join(cfg.DataRoot, "incomplete_collections"),
	}
	ensureDirs(defaultDirs)
	defaultSettings := models.SystemSettings{
		TMDBAPIKey:               cfg.TMDBAPIKey,
		NetworkProxy:             cfg.NetworkProxy,
		ClassificationYAML:       config.DefaultClassificationYAML,
		CloudDriveAddress:        cfg.CloudDriveAddr,
		CloudDriveRootPath:       "/",
		CloudDriveStagingPath:    "/Curio/staging",
		CloudDriveFailedPath:     "/Curio/failed",
		CloudDriveIncompletePath: "/Curio/incomplete_collections",
	}
	if err := store.Seed(ctx, defaultDirs, defaultSettings, naming.DefaultTemplates()); err != nil {
		log.Fatal(err)
	}
	settings, err := store.Settings(ctx)
	if err != nil {
		log.Fatal(err)
	}
	scraperClient := scraper.New(settings.TMDBAPIKey, settings.NetworkProxy, redisClient)
	checker := collection.New(store)
	workerService := worker.New(store, scraperClient, checker, redisClient)
	if err := workerService.Recover(ctx); err != nil {
		log.Fatal(err)
	}
	p115Service := p115.NewService(store)
	p115Service.StartScheduler(ctx)
	embyproxy.StartPortManager(ctx, store, p115Service)
	handler := api.NewWithP115(store, workerService, scraperClient, redisClient, p115Service, cfg.FrontendOrigin, cfg.FrontendDir, cfg.AdminToken)
	log.Printf("curio listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		log.Fatal(err)
	}
}

func connectPostgres(ctx context.Context, databaseURL string) *pgxpool.Pool {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		db, err := pgxpool.New(ctx, databaseURL)
		if err == nil {
			if pingErr := db.Ping(ctx); pingErr == nil {
				return db
			} else {
				lastErr = pingErr
			}
			db.Close()
		} else {
			lastErr = err
		}
		time.Sleep(time.Second)
	}
	log.Fatal(lastErr)
	return nil
}

func ensureDirs(dirs models.DirectoryConfig) {
	for _, path := range []string{dirs.IncomingPath, dirs.StagingPath, dirs.FailedPath, dirs.IncompleteCollectionsPath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			log.Fatal(err)
		}
	}
}
