package consensus

import (
        "math/big"
        "testing"
)

// TestRollingAverageWindow tests 50-block rolling window calculation (Whitepaper Bab 9.1)
func TestRollingAverageWindow(t *testing.T) {
        mgr := NewPoBRetargetManager()

        // Test case 1: Empty history
        if avg := mgr.calculateAverageValidatorCount(); avg != 0 {
                t.Errorf("Expected 0 for empty history, got %d", avg)
        }

        // Test case 2: Less than 50 blocks
        for i := 1; i <= 30; i++ {
                mgr.RecordValidatorCount(100)
        }
        if avg := mgr.calculateAverageValidatorCount(); avg != 100 {
                t.Errorf("Expected 100 for 30 blocks, got %d", avg)
        }

        // Test case 3: Exactly 50 blocks
        for i := 31; i <= 50; i++ {
                mgr.RecordValidatorCount(100)
        }
        if avg := mgr.calculateAverageValidatorCount(); avg != 100 {
                t.Errorf("Expected 100 for 50 blocks, got %d", avg)
        }
        if len(mgr.validatorHistory) != 50 {
                t.Errorf("Expected history length 50, got %d", len(mgr.validatorHistory))
        }

        // Test case 4: More than 50 blocks - should keep only last 50
        for i := 1; i <= 20; i++ {
                mgr.RecordValidatorCount(200)
        }
        if len(mgr.validatorHistory) != 50 {
                t.Errorf("Expected history length 50 after overflow, got %d", len(mgr.validatorHistory))
        }
        // Average should shift toward 200
        avg := mgr.calculateAverageValidatorCount()
        if avg < 100 || avg > 200 {
                t.Errorf("Expected average between 100-200, got %d", avg)
        }

        // Test case 5: Rolling window behavior
        mgr2 := NewPoBRetargetManager()
        // Add 50 blocks of 100 validators
        for i := 0; i < 50; i++ {
                mgr2.RecordValidatorCount(100)
        }
        if avg := mgr2.calculateAverageValidatorCount(); avg != 100 {
                t.Errorf("Expected 100, got %d", avg)
        }
        // Add 50 more blocks of 200 validators - should completely replace
        for i := 0; i < 50; i++ {
                mgr2.RecordValidatorCount(200)
        }
        if avg := mgr2.calculateAverageValidatorCount(); avg != 200 {
                t.Errorf("Expected 200 after full replacement, got %d", avg)
        }
}

// TestRetargetTiming tests retargeting triggers every 50 blocks (Whitepaper Bab 9.1)
func TestRetargetTiming(t *testing.T) {
        mgr := NewPoBRetargetManager()

        testCases := []struct {
                height   uint64
                expected bool
        }{
                {0, false},   // Genesis block
                {1, false},   // Too early
                {49, false},  // Just before window
                {50, true},   // First retarget
                {51, false},  // After retarget
                {100, true},  // Second retarget
                {150, true},  // Third retarget
                {200, true},  // Fourth retarget
                {199, false}, // Just before
        }

        for _, tc := range testCases {
                result := mgr.ShouldRetarget(tc.height)
                if result != tc.expected {
                        t.Errorf("Block %d: expected %v, got %v", tc.height, tc.expected, result)
                }
        }
}

// TestDifficultyAdjustmentBounds tests ±20% adjustment bounds (Whitepaper Bab 9.2)
func TestDifficultyAdjustmentBounds(t *testing.T) {
        mgr := NewPoBRetargetManager()

        // Record 50 blocks with very low validator count to trigger max loosening
        for i := 0; i < 50; i++ {
                mgr.RecordValidatorCount(10) // Far below target minimum of 50
        }

        oldUpload := mgr.currentThresholds.MinUploadBandwidth
        oldLatency := mgr.currentThresholds.TargetLatency
        oldPacketLoss := mgr.currentThresholds.TargetPacketLoss

        // Trigger retarget at block 50
        err := mgr.AdjustDifficulty(50)
        if err != nil {
                t.Fatalf("AdjustDifficulty failed: %v", err)
        }

        // Check bounds: max change should be ±20%
        uploadChange := (mgr.currentThresholds.MinUploadBandwidth - oldUpload) / oldUpload
        if uploadChange > 0.21 || uploadChange < -0.21 {
                t.Errorf("Upload change %.2f%% exceeds ±20%% bound", uploadChange*100)
        }

        latencyChange := (mgr.currentThresholds.TargetLatency - oldLatency) / oldLatency
        if latencyChange > 0.21 || latencyChange < -0.21 {
                t.Errorf("Latency change %.2f%% exceeds ±20%% bound", latencyChange*100)
        }

        packetLossChange := (mgr.currentThresholds.TargetPacketLoss - oldPacketLoss) / oldPacketLoss
        if packetLossChange > 0.21 || packetLossChange < -0.21 {
                t.Errorf("PacketLoss change %.2f%% exceeds ±20%% bound", packetLossChange*100)
        }
}

// TestObserverDurationPhases tests 3 phases of observer duration (Whitepaper Bab 5.1.2)
func TestObserverDurationPhases(t *testing.T) {
        testCases := []struct {
                validatorCount int
                minHours       float64
                maxHours       float64
                description    string
        }{
                {50, 6, 6, "Phase 1: <100 validators = 6h fixed"},
                {99, 6, 6, "Phase 1: edge case 99 validators"},
                {100, 6, 24, "Phase 2: 100 validators = start of linear"},
                {550, 6, 24, "Phase 2: 550 validators = mid linear"},
                {1000, 24, 24, "Phase 3: >=1000 validators = 24h fixed"},
                {2000, 24, 24, "Phase 3: >1000 validators = 24h fixed"},
        }

        for _, tc := range testCases {
                duration := calculateObserverDuration(tc.validatorCount)
                hours := duration.Hours()

                if hours < tc.minHours || hours > tc.maxHours {
                        t.Errorf("%s: got %.1fh, expected %.1f-%.1fh", 
                                tc.description, hours, tc.minHours, tc.maxHours)
                }
        }

        // Test linear interpolation in phase 2
        duration100 := calculateObserverDuration(100)
        duration1000 := calculateObserverDuration(1000)

        if duration100.Hours() >= duration1000.Hours() {
                t.Errorf("Phase 2 should increase linearly: 100 validators (%.1fh) should be < 1000 validators (%.1fh)",
                        duration100.Hours(), duration1000.Hours())
        }
}

// TestEntryFeeBoundaries tests edge cases for entry fee calculation
func TestEntryFeeBoundaries(t *testing.T) {
        testCases := []struct {
                validatorCount int
                description    string
        }{
                {1, "Very low validator count"},
                {50, "Minimum threshold"},
                {100, "Phase boundary"},
                {1000, "Maximum threshold"},
                {10000, "Extreme validator count"},
        }

        for _, tc := range testCases {
                duration := calculateObserverDuration(tc.validatorCount)
                entryFee := calculateDynamicEntryFee(tc.validatorCount, duration)

                // Entry fee should never be zero or negative
                if entryFee.Sign() <= 0 {
                        t.Errorf("%s: entry fee should be positive, got %s", tc.description, entryFee.String())
                }

                // Entry fee should scale with validator count (more validators = longer duration = higher fee)
                // Minimum: 7 MB/s × 6h × 3600s = 151,200 MB
                // Maximum: 7 MB/s × 24h × 3600s = 604,800 MB
                minExpected := int64(151200 * 1e8) // 6 hours
                maxExpected := int64(604800 * 1e8) // 24 hours

                if entryFee.Cmp(bigIntFromInt64(minExpected-1000000000)) < 0 {
                        t.Errorf("%s: entry fee %s too low (min ~%d)", tc.description, entryFee.String(), minExpected)
                }
                if entryFee.Cmp(bigIntFromInt64(maxExpected+1000000000)) > 0 {
                        t.Errorf("%s: entry fee %s too high (max ~%d)", tc.description, entryFee.String(), maxExpected)
                }
        }
}

// Helper untuk testing
func bigIntFromInt64(n int64) *big.Int {
        return big.NewInt(n)
}
