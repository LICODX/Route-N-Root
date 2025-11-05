package consensus

import (
	"math/big"
	"net"
	"strings"

	"rnr-blockchain/pkg/core"
)

type NetworkGroup struct {
	ASN        string
	Validators []string
	TotalScore float64
}

func GroupValidatorsByNetwork(validators map[string]*core.ValidatorInfo) map[string]*NetworkGroup {
	groups := make(map[string]*NetworkGroup)

	for _, validator := range validators {
		asn := validator.NetworkASN
		if asn == "" {
			asn = extractASNFromIP(validator.IPAddress)
		}

		if asn == "" {
			asn = "unknown"
		}

		if _, exists := groups[asn]; !exists {
			groups[asn] = &NetworkGroup{
				ASN:        asn,
				Validators: []string{},
				TotalScore: 0,
			}
		}

		groups[asn].Validators = append(groups[asn].Validators, validator.ID)
		groups[asn].TotalScore += validator.PoBScore
	}

	return groups
}

func extractASNFromIP(ipAddr string) string {
	if ipAddr == "" {
		return "unknown"
	}

	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return "unknown"
	}

	if ip.IsLoopback() {
		return "local"
	}

	if ip.IsPrivate() {
		return "private"
	}

	subnet := getSubnet24(ipAddr)
	return subnet
}

func getSubnet24(ipAddr string) string {
	parts := strings.Split(ipAddr, ".")
	if len(parts) >= 3 {
		return parts[0] + "." + parts[1] + "." + parts[2] + ".0/24"
	}
	return "unknown"
}

func DistributePoBRewardFairly(pobReward *big.Int, groups map[string]*NetworkGroup, topCount int) map[string]*big.Int {
	rewards := make(map[string]*big.Int)

	if len(groups) == 0 {
		return rewards
	}

	sharePerGroup := new(big.Int).Div(pobReward, big.NewInt(int64(len(groups))))

	for _, group := range groups {
		if len(group.Validators) == 0 {
			continue
		}

		sharePerValidator := new(big.Int).Div(sharePerGroup, big.NewInt(int64(len(group.Validators))))

		for _, validatorID := range group.Validators {
			if _, exists := rewards[validatorID]; !exists {
				rewards[validatorID] = big.NewInt(0)
			}
			rewards[validatorID] = new(big.Int).Add(rewards[validatorID], sharePerValidator)
		}
	}

	return rewards
}

func GetTopPoBContributorsByGroup(validators map[string]*core.ValidatorInfo, maxGroups int) []string {
	groups := GroupValidatorsByNetwork(validators)

	type groupScore struct {
		asn   string
		score float64
	}

	groupScores := make([]groupScore, 0, len(groups))
	for asn, group := range groups {
		avgScore := group.TotalScore / float64(len(group.Validators))
		groupScores = append(groupScores, groupScore{asn: asn, score: avgScore})
	}

	for i := 0; i < len(groupScores); i++ {
		for j := i + 1; j < len(groupScores); j++ {
			if groupScores[j].score > groupScores[i].score {
				groupScores[i], groupScores[j] = groupScores[j], groupScores[i]
			}
		}
	}

	selectedValidators := []string{}
	count := 0
	for _, gs := range groupScores {
		if count >= maxGroups {
			break
		}
		group := groups[gs.asn]
		selectedValidators = append(selectedValidators, group.Validators...)
		count++
	}

	return selectedValidators
}
