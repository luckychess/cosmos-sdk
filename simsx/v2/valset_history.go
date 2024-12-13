package v2

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"math/rand"
	"slices"
	"time"

	"cosmossdk.io/core/comet"

	"github.com/cosmos/cosmos-sdk/simsx"
)

type historicValSet struct {
	blockTime time.Time
	vals      WeightedValidators
}
type ValSetHistory struct {
	maxElements int
	blockOffset int
	vals        []historicValSet
}

func NewValSetHistory(maxElements int) *ValSetHistory {
	return &ValSetHistory{
		maxElements: maxElements,
		blockOffset: 1, // start at height 1
		vals:        make([]historicValSet, 0, maxElements),
	}
}

func (h *ValSetHistory) Add(blockTime time.Time, vals WeightedValidators) {
	slices.DeleteFunc(vals, func(validator WeightedValidator) bool {
		return validator.Power == 0
	})
	slices.SortFunc(vals, func(a, b WeightedValidator) int {
		return b.Compare(a)
	})
	newEntry := historicValSet{blockTime: blockTime, vals: vals}
	if len(h.vals) >= h.maxElements {
		h.vals = append(h.vals[1:], newEntry)
		h.blockOffset++
		return
	}
	h.vals = append(h.vals, newEntry)
}

// MissBehaviour determines if a random validator misbehaves, creating and returning evidence for duplicate voting.
// Returns a slice of comet.Evidence if misbehavior is detected; otherwise, returns nil.
// Has a 1% chance of generating evidence for a validator's misbehavior.
// Recursively checks for other misbehavior instances and combines their evidence if any.
// Utilizes a random generator to select a validator and evidence-related attributes.
func (h *ValSetHistory) MissBehaviour(r *rand.Rand) []comet.Evidence {
	//if r.Intn(100) != 0 { // 1% chance
	//	return nil
	//}
	n := r.Intn(len(h.vals))
	badVal := simsx.OneOf(r, h.vals[n].vals)
	fmt.Printf("++ duplicate vote val: %s\n", sdk.ValAddress(badVal.Address).String())
	evidence := comet.Evidence{
		Type:             comet.DuplicateVote,
		Validator:        comet.Validator{Address: badVal.Address, Power: badVal.Power},
		Height:           int64(h.blockOffset + n),
		Time:             h.vals[n].blockTime,
		TotalVotingPower: h.vals[n].vals.TotalPower(),
	}
	//if otherEvidence := h.MissBehaviour(r); otherEvidence != nil {
	//	return append([]comet.Evidence{evidence}, otherEvidence...)
	//}
	return []comet.Evidence{evidence}
}
