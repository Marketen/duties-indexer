package services

import (
	"context"
	"time"

	"github.com/Marketen/duties-indexer/internal/application/domain"
	"github.com/Marketen/duties-indexer/internal/application/ports"
	"github.com/Marketen/duties-indexer/internal/logger"
)

const SlotsPerEpoch = domain.Slot(32) // Ethereum consensus constant

type DutiesChecker struct {
	BeaconAdapter ports.BeaconChainAdapter
	PollInterval  time.Duration

	// Static set of validators we track, from env
	ValidatorIndices []domain.ValidatorIndex

	lastFinalizedEpoch domain.Epoch
	checkedEpochs      map[domain.ValidatorIndex]domain.Epoch // latest epoch checked for each validator index
}

// NewDutiesChecker constructs a DutiesChecker with dependencies injected.
func NewDutiesChecker(
	beacon ports.BeaconChainAdapter,
	pollInterval time.Duration,
	validatorIndices []domain.ValidatorIndex,
) *DutiesChecker {
	return &DutiesChecker{
		BeaconAdapter:      beacon,
		PollInterval:       pollInterval,
		ValidatorIndices:   validatorIndices,
		checkedEpochs:      make(map[domain.ValidatorIndex]domain.Epoch),
		lastFinalizedEpoch: 0,
	}
}

// Run starts the periodic check loop. If at interval, ticker ticks but check has not
// ended, we won't start a new check, we will just wait for the next tick.
func (a *DutiesChecker) Run(ctx context.Context) {
	ticker := time.NewTicker(a.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.checkLatestFinalizedEpoch(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (a *DutiesChecker) checkLatestFinalizedEpoch(ctx context.Context) {
	finalizedEpoch, err := a.BeaconAdapter.GetFinalizedEpoch(ctx)
	if err != nil {
		logger.Error("Error fetching finalized epoch: %v", err)
		return
	}
	if finalizedEpoch == a.lastFinalizedEpoch {
		logger.Debug("Finalized epoch %d unchanged, skipping check.", finalizedEpoch)
		return
	}
	a.lastFinalizedEpoch = finalizedEpoch
	logger.Info("New finalized epoch %d detected.", finalizedEpoch)

	if len(a.ValidatorIndices) == 0 {
		logger.Warn("No validator indices configured; nothing to do.")
		return
	}

	logger.Info("Tracking %d validator indices", len(a.ValidatorIndices))
	validatorIndices := a.getValidatorsToCheck(a.ValidatorIndices, finalizedEpoch)
	if len(validatorIndices) == 0 {
		logger.Debug("No validators left to check for epoch %d", finalizedEpoch)
		return
	}

	// Split proposal vs attestation logic
	a.checkProposals(ctx, finalizedEpoch, validatorIndices)
	a.checkAttestations(ctx, finalizedEpoch, validatorIndices)
}

func (a *DutiesChecker) checkProposals(
	ctx context.Context,
	finalizedEpoch domain.Epoch,
	indices []domain.ValidatorIndex,
) {
	proposerDuties, err := a.BeaconAdapter.GetProposerDuties(ctx, finalizedEpoch, indices)
	if err != nil {
		logger.Error("Error fetching proposer duties: %v", err)
		return
	}

	if len(proposerDuties) == 0 {
		logger.Warn("No proposer duties found for finalized epoch %d.", finalizedEpoch)
		return
	}

	for _, duty := range proposerDuties {
		didPropose, err := a.BeaconAdapter.DidProposeBlock(ctx, duty.Slot)
		if err != nil {
			logger.Warn("⚠️ Could not determine if block was proposed at slot %d: %v", duty.Slot, err)
			continue
		}
		if didPropose {
			logger.Info("✅ Validator %d successfully proposed a block at slot %d",
				duty.ValidatorIndex, duty.Slot)
		} else {
			logger.Warn("❌ Validator %d was scheduled to propose at slot %d but did not",
				duty.ValidatorIndex, duty.Slot)
		}
	}
}

// Scalable per-attestation processing.
func (a *DutiesChecker) checkAttestations(
	ctx context.Context,
	finalizedEpoch domain.Epoch,
	validatorIndices []domain.ValidatorIndex,
) {
	// 1) Get attestation duties for our validators (same as before)
	duties, err := a.BeaconAdapter.GetValidatorDutiesBatch(ctx, finalizedEpoch, validatorIndices)
	if err != nil {
		logger.Error("Error fetching validator duties: %v", err)
		return
	}
	if len(duties) == 0 {
		logger.Warn("No duties found for finalized epoch %d. This should not happen!", finalizedEpoch)
		return
	}

	// Map of "validators we care about"
	tracked := make(map[domain.ValidatorIndex]struct{}, len(validatorIndices))
	for _, idx := range validatorIndices {
		tracked[idx] = struct{}{}
	}

	// 2) Compute epoch slot range
	startSlot := domain.Slot(uint64(finalizedEpoch) * uint64(SlotsPerEpoch))
	endSlot := startSlot + SlotsPerEpoch - 1

	// 3) Get full committees for this epoch (for all validators, not just ours)
	epochCommittees, err := a.BeaconAdapter.GetEpochCommittees(ctx, finalizedEpoch)
	if err != nil {
		logger.Error("Error fetching epoch committees for epoch %d: %v", finalizedEpoch, err)
		return
	}

	// 4) Preload attestations for the inclusion window [startSlot+1 .. endSlot+32]
	slotAttestations := preloadSlotAttestations(ctx, a.BeaconAdapter, startSlot, endSlot)

	// 5) attested[vIdx] == true if we see an aggregation bit set for that validator in this epoch
	attested := make(map[domain.ValidatorIndex]bool, len(validatorIndices))

	// Process all attestations once
	for includedSlot, atts := range slotAttestations {
		if len(atts) == 0 {
			continue
		}
		for _, att := range atts {
			// Only care about attestations whose *data slot* is within the finalized epoch
			dataSlot := att.DataSlot
			if dataSlot < startSlot || dataSlot > endSlot {
				continue
			}

			// Get committees for this data slot
			slotCommittees, ok := epochCommittees[dataSlot]
			if !ok {
				logger.Warn("No committees found for data slot %d (included in block slot %d)", dataSlot, includedSlot)
				continue
			}

			// Decode which committees are aggregated in this attestation
			aggregatedCommittees := getTrueBitIndices(att.CommitteeBits)
			if len(aggregatedCommittees) == 0 {
				continue
			}

			// Now walk through committees in the order of committeeBits and map aggregation bits → validators
			bitBase := 0
			for _, commIdxInt := range aggregatedCommittees {
				commIdx := domain.CommitteeIndex(commIdxInt)
				validators, ok := slotCommittees[commIdx]
				if !ok || len(validators) == 0 {
					// This can happen if beacon node committee info and block data are inconsistent
					logger.Warn("Committee %d not found for data slot %d while processing attestation in included slot %d",
						commIdx, dataSlot, includedSlot)
					continue
				}

				for localPos, valIndex := range validators {
					// Global bit position inside AggregationBits
					globalBit := bitBase + localPos
					if !isBitSet(att.AggregationBits, globalBit) {
						continue
					}
					if _, ok := tracked[valIndex]; !ok {
						// We don't care about non-tracked validators
						continue
					}
					attested[valIndex] = true
				}

				bitBase += len(validators)
			}
		}
	}

	// 6) For each duty, decide if the validator attested or not (end result same as before)
	for _, duty := range duties {
		if attested[duty.ValidatorIndex] {
			logger.Info("✅ Validator %d attested for duty slot %d in finalized epoch %d",
				duty.ValidatorIndex, duty.Slot, finalizedEpoch)
		} else {
			logger.Warn("❌ No attestation found for validator %d in finalized epoch %d (duty slot %d)",
				duty.ValidatorIndex, finalizedEpoch, duty.Slot)
		}
		a.markCheckedThisEpoch(duty.ValidatorIndex, finalizedEpoch)
	}
}

// getValidatorsToCheck filters out validators already checked for this epoch.
func (a *DutiesChecker) getValidatorsToCheck(indices []domain.ValidatorIndex, epoch domain.Epoch) []domain.ValidatorIndex {
	var result []domain.ValidatorIndex
	for _, index := range indices {
		if a.wasCheckedThisEpoch(index, epoch) {
			continue
		}
		result = append(result, index)
	}
	return result
}

func (a *DutiesChecker) wasCheckedThisEpoch(index domain.ValidatorIndex, epoch domain.Epoch) bool {
	if a.checkedEpochs == nil {
		return false
	}
	return a.checkedEpochs[index] == epoch
}

func (a *DutiesChecker) markCheckedThisEpoch(index domain.ValidatorIndex, epoch domain.Epoch) {
	if a.checkedEpochs == nil {
		a.checkedEpochs = make(map[domain.ValidatorIndex]domain.Epoch)
	}
	a.checkedEpochs[index] = epoch
}

// preloadSlotAttestations loads attestations for [minSlot+1 .. maxSlot+32], to cover inclusion distances up to 32.
func preloadSlotAttestations(ctx context.Context, beacon ports.BeaconChainAdapter, minSlot, maxSlot domain.Slot) map[domain.Slot][]domain.Attestation {
	result := make(map[domain.Slot][]domain.Attestation)
	for slot := minSlot + 1; slot <= maxSlot+32; slot++ {
		att, err := beacon.GetBlockAttestations(ctx, slot)
		if err != nil {
			logger.Warn("Error fetching attestations for slot %d: %v. Was this slot missed?", slot, err)
			continue
		}
		result[slot] = att
	}
	return result
}

// getTrueBitIndices returns the indices of bits that are 1 in the given bitfield.
func getTrueBitIndices(bits []byte) []int {
	var indices []int
	for i := 0; i < len(bits)*8; i++ {
		if isBitSet(bits, i) {
			indices = append(indices, i)
		}
	}
	return indices
}

func isBitSet(bits []byte, index int) bool {
	byteIndex := index / 8
	bitIndex := index % 8
	if byteIndex >= len(bits) {
		return false
	}
	return (bits[byteIndex] & (1 << uint(bitIndex))) != 0
}
