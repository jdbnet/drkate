package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jamie/drkate/internal/auth"
	"github.com/jamie/drkate/internal/config"
	"github.com/jamie/drkate/internal/crypto"
	"github.com/jamie/drkate/internal/k8s"
	"github.com/jamie/drkate/internal/server"
	"github.com/jamie/drkate/internal/server/handlers"
	"github.com/jamie/drkate/internal/storage"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 && os.Args[1] == "--config" && len(os.Args) > 2 {
		configPath = os.Args[2]
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	encKey, err := cfg.EncryptionKey()
	if err != nil {
		log.Fatalf("encryption key: %v", err)
	}
	enc, err := crypto.NewEncryptor(encKey)
	if err != nil {
		log.Fatalf("encryptor: %v", err)
	}

	sessionSecret, err := cfg.SessionSecret()
	if err != nil {
		log.Fatalf("session secret: %v", err)
	}

	if err := os.MkdirAll(cfg.Storage.Path, 0755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	store, err := storage.NewStore(cfg.Storage.Path, enc)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	userStore, err := auth.NewUserStore(cfg.Storage.Path, enc)
	if err != nil {
		log.Fatalf("users: %v", err)
	}

	if userStore.Count() == 0 {
		password := os.Getenv("DRKATE_BOOTSTRAP_PASSWORD")
		if password == "" {
			log.Fatalf("no users found; set DRKATE_BOOTSTRAP_PASSWORD to create admin user")
		}
		if err := userStore.Bootstrap("admin", password, auth.RoleAdmin); err != nil {
			log.Fatalf("bootstrap: %v", err)
		}
		log.Println("created bootstrap admin user")
	}

	source, err := k8s.NewClusterClients(cfg.Source.Kubeconfig)
	if err != nil {
		log.Fatalf("source cluster: %v", err)
	}

	dr, err := k8s.NewClusterClients(cfg.DR.Kubeconfig)
	if err != nil {
		log.Fatalf("dr cluster: %v", err)
	}

	sanitizer := k8s.NewSanitizer(cfg.Scrape)
	scraper := k8s.NewScraper(source, store, cfg.Scrape, cfg.Source.Namespaces)
	coordinator := k8s.NewScrapeCoordinator()
	comparator := k8s.NewComparator(dr, store, sanitizer)
	drCache := k8s.NewDrStatusCache(comparator)
	deployer := k8s.NewDeployer(dr, store)

	sessions := auth.NewSessionManager(sessionSecret)
	h := handlers.NewHandler(userStore, sessions, store, scraper, coordinator, comparator, drCache, deployer, sanitizer)

	srv := server.New(h, sessions)

	drCache.StartBackgroundRefresh(60 * time.Second)

	interval, err := cfg.ScrapeInterval()
	if err != nil {
		log.Fatalf("scrape interval: %v", err)
	}
	if interval > 0 {
		go periodicScrape(interval, coordinator, scraper, drCache)
	}

	httpServer := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: srv.Router(),
	}

	go func() {
		log.Printf("DrKate listening on %s", cfg.Server.Listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func periodicScrape(interval time.Duration, coordinator *k8s.ScrapeCoordinator, scraper *k8s.Scraper, drCache *k8s.DrStatusCache) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		result, err := coordinator.Run(ctx, scraper)
		cancel()
		if err != nil {
			if err != k8s.ErrScrapeInProgress {
				log.Printf("periodic scrape error: %v", err)
			}
		} else {
			log.Printf("periodic scrape: %d resources in %d namespaces", result.ResourceCount, len(result.Namespaces))
			drCache.RefreshAsync()
		}
	}
}
