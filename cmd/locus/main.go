// Command locus is a Go reimplementation of a8.locus.LocusMain: a Maven/sbt
// artifact-repository proxy server. It reads the same HOCON config as the
// original (config/config.hocon, path locus.server.app).
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/accur8/locus-go/internal/buildinfo"
	"github.com/accur8/locus-go/internal/config"
	"github.com/accur8/locus-go/internal/repo"
	"github.com/accur8/locus-go/internal/server"
	"github.com/accur8/locus-go/internal/upstream"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config/config.hocon", "path to the HOCON config file")
	appPath := flag.String("app-path", config.DefaultAppPath, "HOCON object path holding the locus config")
	region := flag.String("s3-region", "us-east-1", "AWS region for S3 repos")
	debug := flag.Bool("debug", false, "enable debug logging")
	showVersion := flag.Bool("version", false, "print build info and exit")
	checkConfig := flag.Bool("check", false, "load and validate the config, print a summary, and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.String())
		return nil
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	cfg, err := config.Load(*configPath, *appPath)
	if err != nil {
		return err
	}

	if *checkConfig {
		fmt.Printf("config OK: %s\n  port=%d  repos=%d  users=%d  s3=%t  dataDir=%s\n",
			*configPath, cfg.Port, len(cfg.Repos), len(cfg.Users), cfg.S3 != nil, cfg.DataDirectory)
		for _, r := range cfg.Repos {
			fmt.Printf("  - %-12s %s\n", r.Type, r.Name)
		}
		return nil
	}

	slog.Info("starting locus-go", "version", buildinfo.Version, "commit", buildinfo.GitCommit, "go", buildinfo.GoVersion)

	httpClient := upstream.NewHTTPClient()

	var s3Client *upstream.S3Client
	if cfg.S3 != nil {
		s3Client = upstream.NewS3Client(cfg.S3.AccessKey, cfg.S3.SecretKey, *region)
	} else if hasS3Repo(cfg) {
		slog.Warn("config has s3 repos but no s3 credentials; s3 access will fail")
	}

	m, err := repo.NewModel(cfg, httpClient, s3Client)
	if err != nil {
		return err
	}

	subnets := config.NewSubnetManager(cfg.ProxyServerAddresses, cfg.AnonymousSubnets)
	srv := server.New(cfg, m, subnets)

	addr := ":" + strconv.Itoa(cfg.Port)
	slog.Info("http server is listening", "port", cfg.Port)

	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}
	if err := httpServer.ListenAndServe(); err != nil {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func hasS3Repo(cfg *config.LocusConfig) bool {
	for _, r := range cfg.Repos {
		if r.Type == "url" && r.HasURL && (r.URL.Scheme == "s3") {
			return true
		}
	}
	return false
}
