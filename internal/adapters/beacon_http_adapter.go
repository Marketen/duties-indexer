package adapters

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	nethttp "net/http"
	"time"

	"github.com/Marketen/duties-indexer/internal/application/domain"
	"github.com/Marketen/duties-indexer/internal/application/ports"

	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	eth2http "github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/rs/zerolog"
)

// beaconHTTPClient implements ports.BeaconChainAdapter using go-eth2-client.
type beaconHTTPClient struct {
	client *eth2http.Service
}

// NewBeaconHTTPAdapter is the constructor used from main.go.
func NewBeaconHTTPAdapter(endpoint string) (ports.BeaconChainAdapter, error) {
	// Silence go-eth2-client logs unless they are warnings+.
	zerolog.SetGlobalLevel(zerolog.WarnLevel)

	customHTTPClient := &nethttp.Client{
		Timeout: 2000 * time.Second, // global upper bound; per-request timeout below
	}

	client, err := eth2http.New(
		context.Background(),
		eth2http.WithAddress(endpoint),
		eth2http.WithHTTPClient(customHTTPClient),
		// This is the per-request timeout used by go-eth2-client.
		eth2http.WithTimeout(20*time.Second),
	)
	if err != nil {
		return nil, err
	}

	return &beaconHTTPClient{client: client.(*eth2http.Service)}, nil
}

// GetFinalizedEpoch returns the latest finalized epoch.
func (b *beaconHTTPClient) GetFinalizedEpoch(ctx context.Context) (domain.Epoch, error) {
	finality, err := b.client.Finality(ctx, &api.FinalityOpts{State: "head"})
	if err != nil {
		return 0, err
	}
	return domain.Epoch(finality.Data.Finalized.Epoch), nil
}

// GetValidatorDutiesBatch returns attester duties for given validators in an epoch.
func (b *beaconHTTPClient) GetValidatorDutiesBatch(
	ctx context.Context,
	epoch domain.Epoch,
	indices []domain.ValidatorIndex,
) ([]domain.ValidatorDuty, error) {
	beaconIndices := make([]phase0.ValidatorIndex, 0, len(indices))
	for _, idx := range indices {
		beaconIndices = append(beaconIndices, phase0.ValidatorIndex(idx))
	}

	duties, err := b.client.AttesterDuties(ctx, &api.AttesterDutiesOpts{
		Epoch:   phase0.Epoch(epoch),
		Indices: beaconIndices,
	})
	if err != nil {
		return nil, err
	}

	result := make([]domain.ValidatorDuty, 0, len(duties.Data))
	for _, d := range duties.Data {
		result = append(result, domain.ValidatorDuty{
			ValidatorIndex:        domain.ValidatorIndex(d.ValidatorIndex),
			Slot:                  domain.Slot(d.Slot),
			CommitteeIndex:        domain.CommitteeIndex(d.CommitteeIndex),
			ValidatorCommitteeIdx: d.ValidatorCommitteeIndex,
		})
	}
	return result, nil
}

// GetProposerDuties returns proposer duties for given validators in an epoch.
func (b *beaconHTTPClient) GetProposerDuties(
	ctx context.Context,
	epoch domain.Epoch,
	indices []domain.ValidatorIndex,
) ([]domain.ProposerDuty, error) {
	beaconIndices := make([]phase0.ValidatorIndex, 0, len(indices))
	for _, idx := range indices {
		beaconIndices = append(beaconIndices, phase0.ValidatorIndex(idx))
	}

	resp, err := b.client.ProposerDuties(ctx, &api.ProposerDutiesOpts{
		Epoch:   phase0.Epoch(epoch),
		Indices: beaconIndices,
	})
	if err != nil {
		return nil, err
	}

	duties := make([]domain.ProposerDuty, 0, len(resp.Data))
	for _, d := range resp.Data {
		duties = append(duties, domain.ProposerDuty{
			ValidatorIndex: domain.ValidatorIndex(d.ValidatorIndex),
			Slot:           domain.Slot(d.Slot),
		})
	}
	return duties, nil
}

// DidProposeBlock checks whether a block exists at a given slot.
func (b *beaconHTTPClient) DidProposeBlock(
	ctx context.Context,
	slot domain.Slot,
) (bool, error) {
	block, err := b.client.SignedBeaconBlock(ctx, &api.SignedBeaconBlockOpts{
		Block: fmt.Sprintf("%d", slot),
	})
	if err != nil {
		// Missed slot → 404.
		if apiErr, ok := err.(*api.Error); ok && apiErr.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	return block != nil && block.Data != nil, nil
}

// GetEpochCommittees returns:
//
//	data-slot → committee-index → []validatorIndex
func (b *beaconHTTPClient) GetEpochCommittees(
	ctx context.Context,
	epoch domain.Epoch,
) (domain.EpochCommittees, error) {
	e := phase0.Epoch(epoch)
	resp, err := b.client.BeaconCommittees(ctx, &api.BeaconCommitteesOpts{
		// Epoch filters by epoch, state defaults to "head".
		Epoch: &e,
	})
	if err != nil {
		return nil, err
	}

	result := make(domain.EpochCommittees)
	for _, c := range resp.Data {
		slot := domain.Slot(c.Slot)
		index := domain.CommitteeIndex(c.Index)

		vals := make([]domain.ValidatorIndex, len(c.Validators))
		for i, v := range c.Validators {
			vals[i] = domain.ValidatorIndex(v)
		}

		slotMap, ok := result[slot]
		if !ok {
			slotMap = make(map[domain.CommitteeIndex][]domain.ValidatorIndex)
			result[slot] = slotMap
		}
		slotMap[index] = vals
	}
	return result, nil
}

// GetBlockAttestations returns all attestations included in the block at `slot`.
//
// We:
//   - treat 404 as "missed slot": return (nil, nil)
//   - currently only support Electra blocks (as your logic assumes committee_bits).
func (b *beaconHTTPClient) GetBlockAttestations(
	ctx context.Context,
	slot domain.Slot,
) ([]domain.Attestation, error) {
	block, err := b.client.SignedBeaconBlock(ctx, &api.SignedBeaconBlockOpts{
		Block: fmt.Sprintf("%d", slot),
	})
	if err != nil {
		if apiErr, ok := err.(*api.Error); ok && apiErr.StatusCode == 404 {
			// No block at this slot → no attestations.
			return nil, nil
		}
		return nil, err
	}

	if block == nil || block.Data == nil || block.Data.Electra == nil {
		// Pre-Electra or unexpected shape: skip for now.
		return nil, nil
	}

	var out []domain.Attestation
	for _, att := range block.Data.Electra.Message.Body.Attestations {
		out = append(out, domain.Attestation{
			DataSlot:        domain.Slot(att.Data.Slot),
			CommitteeBits:   att.CommitteeBits,
			AggregationBits: att.AggregationBits,
		})
	}
	return out, nil
}

// (Optional) still useful if you want standalone index→pubkey mapping elsewhere.
func (b *beaconHTTPClient) GetValidatorIndicesByPubkeys(
	ctx context.Context,
	pubkeys []string,
) ([]domain.ValidatorIndex, error) {
	var beaconPubkeys []phase0.BLSPubKey

	for _, hexPubkey := range pubkeys {
		if len(hexPubkey) >= 2 && hexPubkey[:2] == "0x" {
			hexPubkey = hexPubkey[2:]
		}

		bytes, err := hex.DecodeString(hexPubkey)
		if err != nil {
			return nil, errors.New("failed to decode pubkey: " + hexPubkey)
		}
		if len(bytes) != 48 {
			return nil, errors.New("invalid pubkey length for: " + hexPubkey)
		}

		var blsPubkey phase0.BLSPubKey
		copy(blsPubkey[:], bytes)
		beaconPubkeys = append(beaconPubkeys, blsPubkey)
	}

	validators, err := b.client.Validators(ctx, &api.ValidatorsOpts{
		State:   "head",
		PubKeys: beaconPubkeys,
		ValidatorStates: []apiv1.ValidatorState{
			apiv1.ValidatorStateActiveOngoing,
			apiv1.ValidatorStateActiveExiting,
			apiv1.ValidatorStateActiveSlashed,
		},
	})
	if err != nil {
		return nil, err
	}

	indices := make([]domain.ValidatorIndex, 0, len(validators.Data))
	for _, v := range validators.Data {
		indices = append(indices, domain.ValidatorIndex(v.Index))
	}
	return indices, nil
}
