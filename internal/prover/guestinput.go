package prover

import (
	"encoding/json"
	"fmt"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

type GuestInputView struct {
	Raw             protocol.ShastaGuestInput
	Blocks          []BlockView
	Carry           CarryView
	ChainID         uint64
	Witnesses       []GuestInputWitnessView
	TaikoRaw        json.RawMessage
	Taiko           GuestInputTaikoView
	DataSourcesRaw  []json.RawMessage
	DataSourceCount int
}

type GuestInputWitnessView struct {
	Raw          json.RawMessage
	BlockRaw     json.RawMessage
	ChainSpecRaw json.RawMessage
	WitnessRaw   json.RawMessage
	AccountsRaw  json.RawMessage
	ReplayBlock  protocol.ReplayBlock
}

type GuestInputTaikoView struct {
	ProposalID              uint64
	ProposalEventProposalID uint64
	DataSourcesRaw          []json.RawMessage
	DataSourceCount         int
}

type rawGuestInputWitness struct {
	Block     json.RawMessage `json:"block"`
	ChainSpec json.RawMessage `json:"chain_spec"`
	Witness   json.RawMessage `json:"witness"`
	Accounts  json.RawMessage `json:"accounts"`
}

type rawGuestInputTaiko struct {
	ProposalID    json.RawMessage         `json:"proposal_id"`
	ProposalEvent rawGuestInputTaikoEvent `json:"proposal_event"`
	DataSources   []json.RawMessage       `json:"data_sources"`
}

type rawGuestInputTaikoEvent struct {
	Proposal rawGuestInputTaikoProposal `json:"proposal"`
}

type rawGuestInputTaikoProposal struct {
	ID json.RawMessage `json:"id"`
}

func DecodeGuestInput(input protocol.ShastaGuestInput) (*GuestInputView, error) {
	if len(input.Witnesses) == 0 {
		return nil, fmt.Errorf("guest input must include at least one witness")
	}

	witnesses := make([]GuestInputWitnessView, 0, len(input.Witnesses))
	blocks := make([]BlockView, 0, len(input.Witnesses))
	for index, raw := range input.Witnesses {
		witness, block, err := decodeGuestInputWitness(raw)
		if err != nil {
			return nil, fmt.Errorf("decode guest input witness %d: %w", index, err)
		}
		witnesses = append(witnesses, witness)
		blocks = append(blocks, block)
	}

	carry, err := decodeCarry(input.ProofCarryData)
	if err != nil {
		return nil, err
	}

	taiko, err := decodeGuestInputTaiko(input.Taiko)
	if err != nil {
		return nil, err
	}

	return &GuestInputView{
		Raw:             input,
		Blocks:          blocks,
		Carry:           carry,
		ChainID:         carry.ChainID,
		Witnesses:       witnesses,
		TaikoRaw:        input.Taiko,
		Taiko:           taiko,
		DataSourcesRaw:  taiko.DataSourcesRaw,
		DataSourceCount: taiko.DataSourceCount,
	}, nil
}

func decodeGuestInputWitness(raw json.RawMessage) (GuestInputWitnessView, BlockView, error) {
	var decoded rawGuestInputWitness
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return GuestInputWitnessView{}, BlockView{}, fmt.Errorf("unmarshal witness: %w", err)
	}
	if len(decoded.Block) == 0 {
		return GuestInputWitnessView{}, BlockView{}, fmt.Errorf("missing witness.block")
	}
	if len(decoded.ChainSpec) == 0 {
		return GuestInputWitnessView{}, BlockView{}, fmt.Errorf("missing witness.chain_spec")
	}
	if len(decoded.Witness) == 0 {
		return GuestInputWitnessView{}, BlockView{}, fmt.Errorf("missing witness.witness")
	}
	if len(decoded.Accounts) == 0 {
		return GuestInputWitnessView{}, BlockView{}, fmt.Errorf("missing witness.accounts")
	}

	replay := protocol.ReplayBlock{
		Block:     decoded.Block,
		ChainSpec: decoded.ChainSpec,
		Witness:   decoded.Witness,
		Accounts:  decoded.Accounts,
	}
	block, err := decodeBlock(replay)
	if err != nil {
		return GuestInputWitnessView{}, BlockView{}, err
	}

	return GuestInputWitnessView{
		Raw:          raw,
		BlockRaw:     decoded.Block,
		ChainSpecRaw: decoded.ChainSpec,
		WitnessRaw:   decoded.Witness,
		AccountsRaw:  decoded.Accounts,
		ReplayBlock:  replay,
	}, block, nil
}

func decodeGuestInputTaiko(raw json.RawMessage) (GuestInputTaikoView, error) {
	var decoded rawGuestInputTaiko
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return GuestInputTaikoView{}, fmt.Errorf("unmarshal taiko: %w", err)
	}

	var proposalID uint64
	if len(decoded.ProposalID) != 0 {
		value, err := parseUint64JSON(decoded.ProposalID)
		if err != nil {
			return GuestInputTaikoView{}, fmt.Errorf("parse taiko.proposal_id: %w", err)
		}
		proposalID = value
	}

	var proposalEventProposalID uint64
	if len(decoded.ProposalEvent.Proposal.ID) != 0 {
		value, err := parseUint64JSON(decoded.ProposalEvent.Proposal.ID)
		if err != nil {
			return GuestInputTaikoView{}, fmt.Errorf("parse taiko.proposal_event.proposal.id: %w", err)
		}
		proposalEventProposalID = value
	}

	return GuestInputTaikoView{
		ProposalID:              proposalID,
		ProposalEventProposalID: proposalEventProposalID,
		DataSourcesRaw:          decoded.DataSources,
		DataSourceCount:         len(decoded.DataSources),
	}, nil
}
