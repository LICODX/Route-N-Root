package consensus

import (
        "testing"

        "rnr-blockchain/pkg/core"
)

func TestCalculateDynamicBlockSize(t *testing.T) {
        tests := []struct {
                name          string
                pobScore      float64
                expectedSize  int
                description   string
        }{
                {
                        name:         "Low bandwidth validator (PoB 0.5)",
                        pobScore:     0.5,
                        expectedSize: 50,
                        description:  "Minimum block size for underperforming validators",
                },
                {
                        name:         "Good bandwidth validator (PoB 0.9)",
                        pobScore:     0.9,
                        expectedSize: 90,
                        description:  "Slightly below baseline, scales linearly",
                },
                {
                        name:         "Standard bandwidth validator (PoB 1.0)",
                        pobScore:     1.0,
                        expectedSize: 100,
                        description:  "Base block size at standard performance",
                },
                {
                        name:         "High bandwidth validator (PoB 1.5)",
                        pobScore:     1.5,
                        expectedSize: 150,
                        description:  "Above-average performance, larger blocks allowed",
                },
                {
                        name:         "Exceptional bandwidth validator (PoB 10.0)",
                        pobScore:     10.0,
                        expectedSize: 1000,
                        description:  "Maximum block size cap reached",
                },
                {
                        name:         "Edge case: Zero PoB score",
                        pobScore:     0.0,
                        expectedSize: 50,
                        description:  "Fallback to 0.5 → minimum block size",
                },
                {
                        name:         "Edge case: Negative PoB score",
                        pobScore:     -1.0,
                        expectedSize: 50,
                        description:  "Fallback to 0.5 → minimum block size",
                },
                {
                        name:         "Edge case: Very high PoB score (15.0)",
                        pobScore:     15.0,
                        expectedSize: 1000,
                        description:  "Clamped to maximum block size",
                },
                {
                        name:         "Boundary: Exactly at minimum threshold",
                        pobScore:     0.5,
                        expectedSize: 50,
                        description:  "100 * 0.5 = 50 (exact min)",
                },
                {
                        name:         "Boundary: Just above minimum",
                        pobScore:     0.51,
                        expectedSize: 51,
                        description:  "100 * 0.51 = 51 (above min)",
                },
                {
                        name:         "Boundary: Exactly at maximum threshold",
                        pobScore:     10.0,
                        expectedSize: 1000,
                        description:  "100 * 10 = 1000 (exact max)",
                },
                {
                        name:         "Realistic scenario: Datacenter validator (PoB 2.5)",
                        pobScore:     2.5,
                        expectedSize: 250,
                        description:  "High-performance datacenter with great bandwidth",
                },
        }

        vs := &ValidatorService{}

        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        actual := vs.calculateDynamicBlockCapacity(tt.pobScore)

                        if actual != tt.expectedSize {
                                t.Errorf("calculateDynamicBlockCapacity(%.2f) = %d, expected %d\n   Description: %s",
                                        tt.pobScore, actual, tt.expectedSize, tt.description)
                        } else {
                                t.Logf("✓ PoB %.2f → %d txs (expected: %d) - %s",
                                        tt.pobScore, actual, tt.expectedSize, tt.description)
                        }
                })
        }
}

func TestDynamicBlockSizeConstants(t *testing.T) {
        if core.BaseBlockSize != 100 {
                t.Errorf("BaseBlockSize = %d, expected 100", core.BaseBlockSize)
        }
        if core.MinBlockSize != 50 {
                t.Errorf("MinBlockSize = %d, expected 50", core.MinBlockSize)
        }
        if core.MaxBlockSize != 1000 {
                t.Errorf("MaxBlockSize = %d, expected 1000", core.MaxBlockSize)
        }
        t.Logf("✓ Block size constants: Base=%d, Min=%d, Max=%d",
                core.BaseBlockSize, core.MinBlockSize, core.MaxBlockSize)
}

func TestBlockSizeLinearScaling(t *testing.T) {
        vs := &ValidatorService{}

        pobScores := []float64{0.6, 0.7, 0.8, 0.9, 1.0, 1.1, 1.2, 1.3, 1.4, 1.5}
        expectedSizes := []int{60, 70, 80, 90, 100, 110, 120, 130, 140, 150}

        for i, pobScore := range pobScores {
                actual := vs.calculateDynamicBlockCapacity(pobScore)
                expected := expectedSizes[i]

                if actual != expected {
                        t.Errorf("Linear scaling broken: PoB %.1f → %d txs, expected %d txs",
                                pobScore, actual, expected)
                }
        }

        t.Log("✓ Linear scaling verified: 0.6→60, 0.7→70, ..., 1.5→150")
}

func TestBlockSizePreventsNetworkCongestion(t *testing.T) {
        vs := &ValidatorService{}

        lowBandwidth := vs.calculateDynamicBlockCapacity(0.5)
        highBandwidth := vs.calculateDynamicBlockCapacity(2.0)

        if lowBandwidth >= highBandwidth {
                t.Errorf("Low bandwidth validator should produce smaller blocks: %d >= %d",
                        lowBandwidth, highBandwidth)
        }

        ratio := float64(highBandwidth) / float64(lowBandwidth)
        expectedRatio := 2.0 / 0.5

        if ratio != expectedRatio {
                t.Errorf("Block size ratio mismatch: %.2f, expected %.2f",
                        ratio, expectedRatio)
        }

        t.Logf("✓ Congestion prevention verified:")
        t.Logf("   Low bandwidth (0.5) → %d txs", lowBandwidth)
        t.Logf("   High bandwidth (2.0) → %d txs", highBandwidth)
        t.Logf("   Ratio: %.1fx (matches PoB ratio)", ratio)
}
