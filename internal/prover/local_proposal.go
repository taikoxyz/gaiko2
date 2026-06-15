package prover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const DefaultLocalProposalAPIURL = "http://127.0.0.1:9876"

type ProposalMetadata struct {
	ProposalID         uint64
	ProposalHash       common.Hash
	ParentProposalHash common.Hash
	Proposer           common.Address
	Timestamp          uint64
}

type ProposalMetadataSource interface {
	ProposalMetadataByID(context.Context, uint64) (ProposalMetadata, error)
}

type LocalProposalAPI struct {
	endpoint string
	client   *http.Client
}

var newProposalMetadataSourceFn = func(rawURL string) (ProposalMetadataSource, error) {
	return NewLocalProposalAPI(rawURL)
}

func NewLocalProposalAPI(rawURL string) (*LocalProposalAPI, error) {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if rawURL == "" {
		rawURL = DefaultLocalProposalAPIURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse local proposal API URL: %w", err)
	}
	if !isLocalHTTPURL(parsed) {
		return nil, fmt.Errorf("tdxgeth proposal API URL must be local, got %q", rawURL)
	}
	return &LocalProposalAPI{
		endpoint: rawURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}, nil
}

func (c *LocalProposalAPI) ProposalMetadataByID(ctx context.Context, proposalID uint64) (ProposalMetadata, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.endpoint+"/internal/shasta/proposals/"+strconv.FormatUint(proposalID, 10),
		nil,
	)
	if err != nil {
		return ProposalMetadata{}, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return ProposalMetadata{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ProposalMetadata{}, fmt.Errorf("local proposal API returned HTTP %d for proposal %d", resp.StatusCode, proposalID)
	}

	var decoded localProposalResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ProposalMetadata{}, fmt.Errorf("decode local proposal API response: %w", err)
	}
	return decoded.metadata(proposalID)
}

type localProposalResponse struct {
	ProposalHash string        `json:"proposal_hash"`
	Proposal     localProposal `json:"proposal"`
}

type localProposal struct {
	ID                 uint64 `json:"id"`
	Timestamp          uint64 `json:"timestamp"`
	Proposer           string `json:"proposer"`
	ParentProposalHash string `json:"parent_proposal_hash"`
}

func (r localProposalResponse) metadata(expectedID uint64) (ProposalMetadata, error) {
	if r.Proposal.ID != expectedID {
		return ProposalMetadata{}, fmt.Errorf(
			"local proposal API id mismatch: got %d expected %d",
			r.Proposal.ID,
			expectedID,
		)
	}
	proposalHash, err := parseHashString(r.ProposalHash)
	if err != nil {
		return ProposalMetadata{}, fmt.Errorf("parse local proposal_hash: %w", err)
	}
	parentProposalHash, err := parseHashString(r.Proposal.ParentProposalHash)
	if err != nil {
		return ProposalMetadata{}, fmt.Errorf("parse local parent_proposal_hash: %w", err)
	}
	proposer, err := parseAddressString(r.Proposal.Proposer)
	if err != nil {
		return ProposalMetadata{}, fmt.Errorf("parse local proposer: %w", err)
	}
	return ProposalMetadata{
		ProposalID:         r.Proposal.ID,
		ProposalHash:       proposalHash,
		ParentProposalHash: parentProposalHash,
		Proposer:           proposer,
		Timestamp:          r.Proposal.Timestamp,
	}, nil
}
