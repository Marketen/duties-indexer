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

	// Decide which validator indices to track:
	// - If VALIDATOR_INDICES is set in config, use those.
	// - If empty, fall back to all active validators from the beacon node.
	validatorIndices := cfg.ValidatorIndices
	if len(validatorIndices) == 0 {
		logger.Info("No validator indices configured; fetching all active validators from beacon node")
		activeValidatorIndices, err := beaconAdapter.GetAllActiveValidatorIndices(context.Background())
		if err != nil {
			logger.Error("Failed to fetch active validator indices: %v", err)
			os.Exit(1)
		}
		validatorIndices = activeValidatorIndices
	}

	logger.Info("Tracking %d validators", len(validatorIndices))

	dutiesChecker := services.NewDutiesChecker(
		beaconAdapter,
		cfg.PollInterval,
		validatorIndices,
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
