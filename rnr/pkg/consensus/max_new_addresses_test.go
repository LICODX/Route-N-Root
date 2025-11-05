package consensus

import (
        "math/big"
        "testing"

        "rnr-blockchain/pkg/core"
)

func TestMaxNewAddressesPerBlock(t *testing.T) {
        tests := []struct {
                name           string
                newAddrCount   int
                shouldPass     bool
                description    string
        }{
                {
                        name:           "Within limit (10 new addresses)",
                        newAddrCount:   10,
                        shouldPass:     true,
                        description:    "Block with 10 new addresses should pass validation",
                },
                {
                        name:           "At limit exactly (15 new addresses)",
                        newAddrCount:   15,
                        shouldPass:     true,
                        description:    "Block with exactly 15 new addresses should pass",
                },
                {
                        name:           "Exceeds limit (16 new addresses)",
                        newAddrCount:   16,
                        shouldPass:     false,
                        description:    "Block with 16 new addresses should be rejected (spam protection)",
                },
                {
                        name:           "Spam attack (100 new addresses)",
                        newAddrCount:   100,
                        shouldPass:     false,
                        description:    "Block with 100 new addresses should be rejected (spam attack)",
                },
                {
                        name:           "No new addresses",
                        newAddrCount:   0,
                        shouldPass:     true,
                        description:    "Block with no new addresses should pass",
                },
        }

        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        // Simulate validation logic
                        if tt.newAddrCount > core.MaxNewAddressesPerBlock {
                                if tt.shouldPass {
                                        t.Errorf("%s: Expected to pass but should fail with %d new addresses (max %d)", 
                                                tt.description, tt.newAddrCount, core.MaxNewAddressesPerBlock)
                                }
                        } else {
                                if !tt.shouldPass {
                                        t.Errorf("%s: Expected to fail but should pass with %d new addresses (max %d)", 
                                                tt.description, tt.newAddrCount, core.MaxNewAddressesPerBlock)
                                }
                        }

                        t.Logf("✓ %s: %d new addresses, max allowed: %d, result: %v", 
                                tt.description, tt.newAddrCount, core.MaxNewAddressesPerBlock, 
                                tt.newAddrCount <= core.MaxNewAddressesPerBlock)
                })
        }
}

func TestAntiSpamWalletCreation(t *testing.T) {
        t.Log("=== Anti-Spam Wallet Creation Test ===")
        t.Logf("Maximum new addresses allowed per block: %d", core.MaxNewAddressesPerBlock)

        scenarios := []struct {
                scenario       string
                senders        int
                recipients     int
                totalNew       int
                shouldBeValid  bool
        }{
                {
                        scenario:      "Normal usage: 5 senders, 5 recipients (10 total new)",
                        senders:       5,
                        recipients:    5,
                        totalNew:      10,
                        shouldBeValid: true,
                },
                {
                        scenario:      "Edge case: 8 senders, 7 recipients (15 total new)",
                        senders:       8,
                        recipients:    7,
                        totalNew:      15,
                        shouldBeValid: true,
                },
                {
                        scenario:      "Spam attack: 20 senders, 20 recipients (40 total new)",
                        senders:       20,
                        recipients:    20,
                        totalNew:      40,
                        shouldBeValid: false,
                },
                {
                        scenario:      "Sybil attack: 50 new wallet addresses",
                        senders:       25,
                        recipients:    25,
                        totalNew:      50,
                        shouldBeValid: false,
                },
        }

        for _, scenario := range scenarios {
                t.Run(scenario.scenario, func(t *testing.T) {
                        // Count unique new addresses
                        uniqueNewAddrs := make(map[string]bool)
                        
                        // Simulate sender addresses
                        for i := 0; i < scenario.senders; i++ {
                                addr := generateTestAddress(i)
                                uniqueNewAddrs[addr] = true
                        }
                        
                        // Simulate recipient addresses  
                        for i := 0; i < scenario.recipients; i++ {
                                addr := generateTestAddress(1000 + i) // Different range to avoid overlap
                                uniqueNewAddrs[addr] = true
                        }

                        actualNewCount := len(uniqueNewAddrs)
                        isValid := actualNewCount <= core.MaxNewAddressesPerBlock

                        if isValid != scenario.shouldBeValid {
                                t.Errorf("Scenario: %s\n  Expected valid=%v but got valid=%v\n  New addresses: %d, Max allowed: %d", 
                                        scenario.scenario, scenario.shouldBeValid, isValid, actualNewCount, core.MaxNewAddressesPerBlock)
                        }

                        if isValid {
                                t.Logf("✅ PASS: %s - %d new addresses (within limit)", scenario.scenario, actualNewCount)
                        } else {
                                t.Logf("❌ BLOCK REJECTED: %s - %d new addresses (exceeds limit of %d)", 
                                        scenario.scenario, actualNewCount, core.MaxNewAddressesPerBlock)
                        }
                })
        }
}

func TestSpamAttackPrevention(t *testing.T) {
        t.Log("=== Spam Attack Prevention Simulation ===")
        
        attackScenarios := []struct {
                name        string
                description string
                addresses   int
                blocked     bool
        }{
                {
                        name:        "Micro spam (16 addresses)",
                        description: "Attacker tries to create 16 new wallets in one block",
                        addresses:   16,
                        blocked:     true,
                },
                {
                        name:        "Medium spam (50 addresses)",
                        description: "Attacker tries to create 50 new wallets in one block",
                        addresses:   50,
                        blocked:     true,
                },
                {
                        name:        "Large spam (1000 addresses)",
                        description: "Attacker tries to create 1000 new wallets in one block",
                        addresses:   1000,
                        blocked:     true,
                },
                {
                        name:        "Legitimate usage (15 addresses)",
                        description: "Normal exchange batch processing 15 withdrawals",
                        addresses:   15,
                        blocked:     false,
                },
        }

        for _, attack := range attackScenarios {
                t.Run(attack.name, func(t *testing.T) {
                        isBlocked := attack.addresses > core.MaxNewAddressesPerBlock

                        if isBlocked != attack.blocked {
                                t.Errorf("Attack: %s\n  Expected blocked=%v but got blocked=%v\n  Addresses: %d, Limit: %d",
                                        attack.description, attack.blocked, isBlocked, attack.addresses, core.MaxNewAddressesPerBlock)
                        }

                        if isBlocked {
                                t.Logf("🛡️  ATTACK BLOCKED: %s (%d addresses > %d limit)", 
                                        attack.description, attack.addresses, core.MaxNewAddressesPerBlock)
                        } else {
                                t.Logf("✅ ALLOWED: %s (%d addresses ≤ %d limit)", 
                                        attack.description, attack.addresses, core.MaxNewAddressesPerBlock)
                        }
                })
        }
}

// Helper function to generate test addresses
func generateTestAddress(id int) string {
        return "rnr" + big.NewInt(int64(id)).Text(16) // Generate hex address with rnr prefix
}
