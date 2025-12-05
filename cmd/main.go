package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Marketen/duties-indexer/internal/adapters"
	"github.com/Marketen/duties-indexer/internal/application/services"
	"github.com/Marketen/duties-indexer/internal/config"
	"github.com/Marketen/duties-indexer/internal/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		logger.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	logger.Info("Starting duties-indexer")
	logger.Info("Beacon node URL: %s", cfg.BeaconNodeURL)
	logger.Info("Poll interval: %s", cfg.PollInterval)
	logger.Info("Tracking %d validators", len(cfg.ValidatorIndices))

	beaconAdapter, err := adapters.NewBeaconAttestantAdapter(cfg.BeaconNodeURL)
	if err != nil {
		logger.Error("Failed to create beacon HTTP adapter: %v", err)
		os.Exit(1)
	}

	dutiesChecker := services.NewDutiesChecker(
		beaconAdapter,
		cfg.PollInterval,
		cfg.ValidatorIndices,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT / SIGTERM for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		dutiesChecker.Run(ctx)
	}()

	sig := <-sigCh
	logger.Warn("Received signal %s, shutting down...", sig)
}
