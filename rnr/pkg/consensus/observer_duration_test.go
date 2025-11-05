package consensus

import (
        "testing"
        "time"
)

func TestCalculateObserverDuration(t *testing.T) {
        tests := []struct {
                name              string
                activeValidators  int
                expectedDuration  time.Duration
                toleranceSeconds  int64
        }{
                {
                        name:              "Below minimum validators (<100)",
                        activeValidators:  50,
                        expectedDuration:  6 * time.Hour,
                        toleranceSeconds:  0,
                },
                {
                        name:              "Exactly at minimum (100)",
                        activeValidators:  100,
                        expectedDuration:  6 * time.Hour,
                        toleranceSeconds:  0,
                },
                {
                        name:              "Middle range (500) - linear: 6 + (400/900)*18 = 14h",
                        activeValidators:  500,
                        expectedDuration:  14 * time.Hour,
                        toleranceSeconds:  60,
                },
                {
                        name:              "Near maximum (999) - linear: 6 + (899/900)*18 ≈ 23.98h",
                        activeValidators:  999,
                        expectedDuration:  24*time.Hour - 1*time.Minute,
                        toleranceSeconds:  120,
                },
                {
                        name:              "At maximum (1000)",
                        activeValidators:  1000,
                        expectedDuration:  24 * time.Hour,
                        toleranceSeconds:  0,
                },
                {
                        name:              "Above maximum (2000)",
                        activeValidators:  2000,
                        expectedDuration:  24 * time.Hour,
                        toleranceSeconds:  0,
                },
                {
                        name:              "Linear interpolation at 250 - 6 + (150/900)*18 = 9h",
                        activeValidators:  250,
                        expectedDuration:  9 * time.Hour,
                        toleranceSeconds:  60,
                },
                {
                        name:              "Linear interpolation at 750 - 6 + (650/900)*18 = 19h",
                        activeValidators:  750,
                        expectedDuration:  19 * time.Hour,
                        toleranceSeconds:  60,
                },
        }

        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        actual := calculateObserverDuration(tt.activeValidators)
                        
                        diff := int64(actual - tt.expectedDuration)
                        if diff < 0 {
                                diff = -diff
                        }
                        
                        if diff > tt.toleranceSeconds*int64(time.Second) {
                                t.Errorf("calculateObserverDuration(%d) = %v, expected %v (tolerance: %ds)",
                                        tt.activeValidators, actual, tt.expectedDuration, tt.toleranceSeconds)
                        }
                        
                        t.Logf("✓ Validators: %d → Duration: %v (expected: %v)", 
                                tt.activeValidators, actual, tt.expectedDuration)
                })
        }
}

func TestObserverDurationLinearIncrease(t *testing.T) {
        prev := calculateObserverDuration(100)
        
        for i := 101; i < 1000; i++ {
                current := calculateObserverDuration(i)
                
                if current < prev {
                        t.Errorf("Duration decreased at validator count %d: %v < %v", i, current, prev)
                }
                
                if current == prev && i > 100 {
                        t.Errorf("Duration did not increase at validator count %d (still %v)", i, current)
                }
                
                prev = current
        }
        
        t.Logf("✓ Duration increases monotonically from 100 to 1000 validators")
}

func TestObserverDurationBoundaries(t *testing.T) {
        minDuration := calculateObserverDuration(0)
        if minDuration != 6*time.Hour {
                t.Errorf("Minimum duration (0 validators) = %v, expected 6h", minDuration)
        }
        
        maxDuration := calculateObserverDuration(10000)
        if maxDuration != 24*time.Hour {
                t.Errorf("Maximum duration (10000 validators) = %v, expected 24h", maxDuration)
        }
        
        t.Logf("✓ Minimum: %v, Maximum: %v", minDuration, maxDuration)
}
