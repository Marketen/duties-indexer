package domain

// Basic consensus types
type Epoch uint64
type Slot uint64
type ValidatorIndex uint64
type CommitteeIndex uint64

// ProposerDuty describes a scheduled block proposal for a validator.
type ProposerDuty struct {
	ValidatorIndex ValidatorIndex
	Slot           Slot
}

// ValidatorDuty describes an attestation duty for a validator.
type ValidatorDuty struct {
	ValidatorIndex        ValidatorIndex
	Slot                  Slot
	CommitteeIndex        CommitteeIndex
	ValidatorCommitteeIdx uint64
	CommitteeLength       uint64 // NEW electra: size of this committee
	CommitteesAtSlot      uint64 // NEW electra: number of committees in this slot
}

// Attestation is a simplified representation of a beacon block attestation
// sufficient for us to detect if a validator attested or not.
type Attestation struct {
	// Slot that the attestation data refers to (the duty slot).
	DataSlot Slot

	// Bitfield of which committees are aggregated in this attestation.
	CommitteeBits []byte

	// Bitfield of which validators (across all aggregated committees) participated.
	AggregationBits []byte
}

// EpochCommittees maps:
//
//	data-slot -> committee-index -> list of validator indices in that committee
type EpochCommittees map[Slot]map[CommitteeIndex][]ValidatorIndex

type CommitteeSizeMap map[CommitteeIndex]int
