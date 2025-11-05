package consensus

import (
        "log"
        "rnr-blockchain/pkg/core"
        "time"
)

// PoBRetargetManager handles difficulty adjustment every 50 blocks (Whitepaper Bab 9.1-9.2)
type PoBRetargetManager struct {
        currentThresholds *PoBThresholds
        validatorHistory  []int // Rolling window of validator counts (50 blocks)
}

// PoBThresholds stores current difficulty thresholds
type PoBThresholds struct {
        MinUploadBandwidth float64 // MB/s
        TargetLatency      float64 // ms
        TargetPacketLoss   float64 // %
        LastAdjustHeight   uint64
        LastAdjustTime     time.Time
}

const (
        RetargetWindow      = 50   // Whitepaper: every 50 blocks
        MaxAdjustmentFactor = 0.20 // Whitepaper: ±20% max adjustment
)

func NewPoBRetargetManager() *PoBRetargetManager {
        return &PoBRetargetManager{
                currentThresholds: &PoBThresholds{
                        MinUploadBandwidth: core.MinUploadBandwidth,
                        TargetLatency:      core.TargetLatency,
                        TargetPacketLoss:   core.TargetPacketLoss,
                        LastAdjustHeight:   0,
                        LastAdjustTime:     time.Now(),
                },
                validatorHistory: make([]int, 0, RetargetWindow),
        }
}

// RecordValidatorCount adds current validator count to rolling window (Whitepaper Bab 9.1)
// "protokol akan mengevaluasi jumlah rata-rata validator aktif dalam window 50 blok"
func (prm *PoBRetargetManager) RecordValidatorCount(validatorCount int) {
        prm.validatorHistory = append(prm.validatorHistory, validatorCount)
        
        // Keep only last 50 blocks
        if len(prm.validatorHistory) > RetargetWindow {
                prm.validatorHistory = prm.validatorHistory[len(prm.validatorHistory)-RetargetWindow:]
        }
        
        // Log setiap 10 blocks untuk monitoring (bukan setiap block agar tidak spam)
        if len(prm.validatorHistory)%10 == 0 {
                avgCount := prm.calculateAverageValidatorCount()
                log.Printf("📈 Validator History: %d blocks recorded, rolling avg: %d validators", 
                        len(prm.validatorHistory), avgCount)
        }
}

// calculateAverageValidatorCount computes mean over last 50 blocks (Whitepaper Bab 9.1)
func (prm *PoBRetargetManager) calculateAverageValidatorCount() int {
        if len(prm.validatorHistory) == 0 {
                return 0
        }
        
        sum := 0
        for _, count := range prm.validatorHistory {
                sum += count
        }
        
        return sum / len(prm.validatorHistory)
}

// ShouldRetarget checks if we're at a retarget window
func (prm *PoBRetargetManager) ShouldRetarget(blockHeight uint64) bool {
        return blockHeight > 0 && blockHeight%RetargetWindow == 0
}

// AdjustDifficulty adjusts PoB thresholds based on validator count (Whitepaper Bab 9.2)
func (prm *PoBRetargetManager) AdjustDifficulty(blockHeight uint64) error {
        if !prm.ShouldRetarget(blockHeight) {
                return nil
        }

        // Whitepaper Bab 9.1: "protokol akan mengevaluasi jumlah rata-rata validator aktif dalam window 50 blok"
        // MUST use rolling 50-block average, not instantaneous count
        avgValidatorCount := prm.calculateAverageValidatorCount()
        
        if avgValidatorCount == 0 {
                log.Printf("⚠️  PoB Retarget (Block %d): No validator history, skipping adjustment", blockHeight)
                return nil
        }

        // Whitepaper: "Difficulty akan disesuaikan untuk menjaga validator count dalam rentang sehat"
        // Target range: adjust if too many or too few validators
        const (
                targetMinValidators = 50
                targetMaxValidators = 500
        )

        adjustmentFactor := 0.0

        if avgValidatorCount < targetMinValidators {
                // Too few validators: LOOSEN requirements (decrease difficulty)
                adjustmentFactor = -MaxAdjustmentFactor
                log.Printf("📉 PoB Retarget (Block %d): Too few validators (%d < %d), LOOSENING difficulty by %.0f%%",
                        blockHeight, avgValidatorCount, targetMinValidators, MaxAdjustmentFactor*100)
        } else if avgValidatorCount > targetMaxValidators {
                // Too many validators: TIGHTEN requirements (increase difficulty)
                adjustmentFactor = MaxAdjustmentFactor
                log.Printf("📈 PoB Retarget (Block %d): Too many validators (%d > %d), TIGHTENING difficulty by %.0f%%",
                        blockHeight, avgValidatorCount, targetMaxValidators, MaxAdjustmentFactor*100)
        } else {
                log.Printf("✅ PoB Retarget (Block %d): Validator count healthy (%d), no adjustment needed",
                        blockHeight, avgValidatorCount)
                return nil
        }

        // Apply bounded adjustment (±20%)
        oldUpload := prm.currentThresholds.MinUploadBandwidth
        oldLatency := prm.currentThresholds.TargetLatency
        oldPacketLoss := prm.currentThresholds.TargetPacketLoss

        // Adjust thresholds
        prm.currentThresholds.MinUploadBandwidth = oldUpload * (1.0 + adjustmentFactor)
        prm.currentThresholds.TargetLatency = oldLatency * (1.0 - adjustmentFactor) // Lower latency = harder
        prm.currentThresholds.TargetPacketLoss = oldPacketLoss * (1.0 - adjustmentFactor) // Lower loss = harder

        // Bounds checking: prevent extreme values
        if prm.currentThresholds.MinUploadBandwidth < 5.0 {
                prm.currentThresholds.MinUploadBandwidth = 5.0
        }
        if prm.currentThresholds.MinUploadBandwidth > 10.0 {
                prm.currentThresholds.MinUploadBandwidth = 10.0
        }
        if prm.currentThresholds.TargetLatency < 50.0 {
                prm.currentThresholds.TargetLatency = 50.0
        }
        if prm.currentThresholds.TargetLatency > 200.0 {
                prm.currentThresholds.TargetLatency = 200.0
        }
        if prm.currentThresholds.TargetPacketLoss < 0.05 {
                prm.currentThresholds.TargetPacketLoss = 0.05
        }
        if prm.currentThresholds.TargetPacketLoss > 0.5 {
                prm.currentThresholds.TargetPacketLoss = 0.5
        }

        prm.currentThresholds.LastAdjustHeight = blockHeight
        prm.currentThresholds.LastAdjustTime = time.Now()

        // Whitepaper Bab 9.1-9.2: Enhanced telemetry untuk monitoring retargeting behavior
        log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
        log.Printf("🎯 PoB DIFFICULTY RETARGET (Block #%d)", blockHeight)
        log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
        log.Printf("📊 Validator Statistics (50-block rolling window):")
        log.Printf("   Average Validators: %d (target: %d-%d)", 
                avgValidatorCount, targetMinValidators, targetMaxValidators)
        log.Printf("   Adjustment Factor: %.1f%% (%s)", 
                adjustmentFactor*100, 
                map[float64]string{-MaxAdjustmentFactor: "LOOSEN", MaxAdjustmentFactor: "TIGHTEN", 0: "STABLE"}[adjustmentFactor])
        log.Printf("📈 Threshold Changes:")
        log.Printf("   Upload:      %.2f → %.2f MB/s (%.1f%% change)", 
                oldUpload, prm.currentThresholds.MinUploadBandwidth, 
                ((prm.currentThresholds.MinUploadBandwidth-oldUpload)/oldUpload)*100)
        log.Printf("   Latency:     %.0f → %.0f ms (%.1f%% change)", 
                oldLatency, prm.currentThresholds.TargetLatency,
                ((prm.currentThresholds.TargetLatency-oldLatency)/oldLatency)*100)
        log.Printf("   Packet Loss: %.3f → %.3f%% (%.1f%% change)", 
                oldPacketLoss, prm.currentThresholds.TargetPacketLoss,
                ((prm.currentThresholds.TargetPacketLoss-oldPacketLoss)/oldPacketLoss)*100)
        log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

        return nil
}

// GetCurrentThresholds returns current difficulty thresholds
func (prm *PoBRetargetManager) GetCurrentThresholds() *PoBThresholds {
        return prm.currentThresholds
}

// EvaluateWithCurrentThresholds checks if result passes current thresholds
func (prm *PoBRetargetManager) EvaluateWithCurrentThresholds(result *PoBTestResult) bool {
        thresholds := prm.GetCurrentThresholds()
        
        if result.UploadBandwidth < thresholds.MinUploadBandwidth {
                return false
        }
        if result.Latency > thresholds.TargetLatency {
                return false
        }
        if result.PacketLoss > thresholds.TargetPacketLoss {
                return false
        }
        return true
}
