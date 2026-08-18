package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimitCircuitBreakerThresholdDefaultsAndFallback(t *testing.T) {
	setting := GetModelRouteSetting()
	original := setting.RateLimitCircuitBreakerThreshold
	setting.RateLimitCircuitBreakerThreshold = DefaultRateLimitCircuitBreakerThreshold
	t.Cleanup(func() { setting.RateLimitCircuitBreakerThreshold = original })

	assert.Equal(t, DefaultRateLimitCircuitBreakerThreshold, GetRateLimitCircuitBreakerThreshold())

	setting.RateLimitCircuitBreakerThreshold = MinRateLimitCircuitBreakerThreshold - 1
	assert.Equal(t, DefaultRateLimitCircuitBreakerThreshold, GetRateLimitCircuitBreakerThreshold())

	setting.RateLimitCircuitBreakerThreshold = MaxRateLimitCircuitBreakerThreshold + 1
	assert.Equal(t, DefaultRateLimitCircuitBreakerThreshold, GetRateLimitCircuitBreakerThreshold())
}

func TestValidateRateLimitCircuitBreakerThreshold(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "minimum", value: "3", want: 3},
		{name: "maximum", value: "999", want: 999},
		{name: "trimmed", value: " 42 ", want: 42},
		{name: "below minimum", value: "2", wantErr: true},
		{name: "above maximum", value: "1000", wantErr: true},
		{name: "fraction", value: "3.5", wantErr: true},
		{name: "empty", value: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateRateLimitCircuitBreakerThreshold(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
