package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Marketen/duties-indexer/internal/application/domain"
)

// Config holds runtime configuration for the duties-indexer service.
type Config struct {
	BeaconNodeURL    string
	PollInterval     time.Duration
	ValidatorIndices []domain.ValidatorIndex
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	beaconURL := strings.TrimSpace(os.Getenv("BEACON_NODE_URL"))
	if beaconURL == "" {
		return nil, fmt.Errorf("BEACON_NODE_URL is required")
	}

	intervalStr := strings.TrimSpace(os.Getenv("POLL_INTERVAL_SECONDS"))
	if intervalStr == "" {
		intervalStr = "60"
	}
	sec, err := strconv.Atoi(intervalStr)
	if err != nil || sec <= 0 {
		return nil, fmt.Errorf("invalid POLL_INTERVAL_SECONDS: %q", intervalStr)
	}
	pollInterval := time.Duration(sec) * time.Second

	// VALIDATOR_INDICES is now optional. If empty, we leave ValidatorIndices
	// empty and the main program will fall back to tracking all active validators.
	valStr := strings.TrimSpace(os.Getenv("VALIDATOR_INDICES"))
	indices := []domain.ValidatorIndex{}
	if valStr != "" {
		rawParts := strings.Split(valStr, ",")
		indices = make([]domain.ValidatorIndex, 0, len(rawParts))
		for _, p := range rawParts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			n, err := strconv.ParseUint(p, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid validator index %q in VALIDATOR_INDICES: %w", p, err)
			}
			indices = append(indices, domain.ValidatorIndex(n))
		}
	}

	return &Config{
		BeaconNodeURL:    beaconURL,
		PollInterval:     pollInterval,
		ValidatorIndices: indices,
	}, nil
}
