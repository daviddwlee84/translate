package engine

import "testing"

func TestPickBestModelMatchesCopilotAutoRanking(t *testing.T) {
	tests := []struct {
		name   string
		models []string
		tier   string
		want   string
	}{
		{
			name:   "claude before openai",
			models: []string{"gpt-5.6-sol", "claude-sonnet-5", "gemini-3.1-pro-preview"},
			tier:   "default",
			want:   "claude-sonnet-5",
		},
		{
			name:   "default uses balanced terra",
			models: []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.4"},
			tier:   "default",
			want:   "gpt-5.6-terra",
		},
		{
			name:   "fast uses luna",
			models: []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.4-mini"},
			tier:   "fast",
			want:   "gpt-5.6-luna",
		},
		{
			name:   "max uses sol",
			models: []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"},
			tier:   "max",
			want:   "gpt-5.6-sol",
		},
		{
			name:   "non-flash gemini before flash",
			models: []string{"gemini-3.7-flash", "gemini-3.1-pro-preview"},
			tier:   "default",
			want:   "gemini-3.1-pro-preview",
		},
		{name: "empty", models: nil, tier: "default", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickBestModel(tc.models, tc.tier); got != tc.want {
				t.Fatalf("pickBestModel(%v, %q) = %q, want %q", tc.models, tc.tier, got, tc.want)
			}
		})
	}
}
