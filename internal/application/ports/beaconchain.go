package ports

import (
	"context"

	"github.com/Marketen/duties-indexer/internal/application/domain"
)

// BeaconChainAdapter is the hexagonal port for accessing beacon chain data.
// The duties checker depends only on this interface, not on any concrete client.
type BeaconChainAdapter interface {
	// GetFinalizedEpoch returns the latest finalized epoch known by the node.
	GetFinalizedEpoch(ctx context.Context) (domain.Epoch, error)

	// GetValidatorDutiesBatch returns attestation duties for the given validators in an epoch.
	GetValidatorDutiesBatch(
		ctx context.Context,
		epoch domain.Epoch,
		indices []domain.ValidatorIndex,
	) ([]domain.ValidatorDuty, error)

	// GetProposerDuties returns proposal duties for the given validators in an epoch.
	GetProposerDuties(
		ctx context.Context,
		epoch domain.Epoch,
		indices []domain.ValidatorIndex,
	) ([]domain.ProposerDuty, error)

	// DidProposeBlock checks if a block was proposed at a specific slot (i.e. block exists).
	DidProposeBlock(ctx context.Context, slot domain.Slot) (bool, error)

	// GetBlockAttestations returns all attestations included in the block at the given slot.
	GetBlockAttestations(ctx context.Context, slot domain.Slot) ([]domain.Attestation, error)

	// GetCommitteeSizeMap returns the size of each attestation committee for a specific slot.
	GetCommitteeSizeMap(ctx context.Context, slot domain.Slot) (domain.CommitteeSizeMap, error)
}
