package consensus

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"rnr-blockchain/pkg/core"
)

type Proposal struct {
	ID        string
	Title     string
	Content   string
	AuthorID  string
	CreatedAt time.Time
	Votes     map[string]bool
	Status    ProposalStatus
	VoteCount int
}

type ProposalStatus int

const (
	Proposed ProposalStatus = iota
	Voting
	Passed
	Failed
)

type Governance struct {
	proposals        map[string]*Proposal
	activeValidators map[string]bool
	mu               sync.RWMutex
}

func NewGovernance(validators map[string]*core.ValidatorInfo) *Governance {
	activeValidators := make(map[string]bool)
	for id, v := range validators {
		if v.IsActive {
			activeValidators[id] = true
		}
	}

	return &Governance{
		proposals:        make(map[string]*Proposal),
		activeValidators: activeValidators,
	}
}

func (g *Governance) SubmitProposal(title, content, authorID string) (*Proposal, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.activeValidators[authorID] {
		return nil, errors.New("only active validators can submit proposals")
	}

	proposal := &Proposal{
		ID:        generateProposalID(title, authorID),
		Title:     title,
		Content:   content,
		AuthorID:  authorID,
		CreatedAt: time.Now(),
		Votes:     make(map[string]bool),
		Status:    Proposed,
	}
	g.proposals[proposal.ID] = proposal

	return proposal, nil
}

func (g *Governance) Vote(proposalID, validatorID string, vote bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	proposal, ok := g.proposals[proposalID]
	if !ok {
		return errors.New("proposal not found")
	}

	if proposal.Status != Voting {
		return errors.New("proposal is not in voting phase")
	}

	if !g.activeValidators[validatorID] {
		return errors.New("only active validators can vote")
	}

	if _, hasVoted := proposal.Votes[validatorID]; hasVoted {
		return errors.New("validator has already voted")
	}

	proposal.Votes[validatorID] = vote
	proposal.VoteCount++

	return nil
}

func (g *Governance) TallyVotes(proposalID string) (bool, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	proposal, ok := g.proposals[proposalID]
	if !ok {
		return false, errors.New("proposal not found")
	}

	totalActiveValidators := len(g.activeValidators)
	if totalActiveValidators == 0 {
		proposal.Status = Failed
		return false, errors.New("no active validators to vote")
	}

	yesVotes := 0
	for _, vote := range proposal.Votes {
		if vote {
			yesVotes++
		}
	}

	requiredVotes := float64(totalActiveValidators) * core.SupermajorityThreshold

	if float64(yesVotes) >= requiredVotes {
		proposal.Status = Passed
		return true, nil
	} else {
		proposal.Status = Failed
		return false, nil
	}
}

func generateProposalID(title, authorID string) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%d", title, authorID, time.Now().UnixNano())))
	return hex.EncodeToString(hash[:])
}

func (g *Governance) RevokeValidatorStatus(validatorID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.activeValidators[validatorID] {
		return errors.New("validator is not active")
	}

	delete(g.activeValidators, validatorID)
	log.Printf("Validator %s has been forcibly removed by governance vote.", validatorID)

	return nil
}
