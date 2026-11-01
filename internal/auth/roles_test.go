package auth

import "testing"

func TestValidProfile(t *testing.T) {
	tests := []struct {
		name        string
		phone       string
		jobFunction string
		roles       []string
		want        bool
	}{
		{"empty", "", "", nil, true},
		{"full", "+5511987654321", "data_analytics", []string{"DECISION_MAKER", "CHAMPION"}, true},
		{"international", "+14155552671", "other", nil, true},
		{"formatted phone", "+55 11 98765-4321", "", nil, false},
		{"no plus", "5511987654321", "", nil, false},
		{"unknown function", "", "astronaut", nil, false},
		{"unknown role", "", "", []string{"KING"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validProfile(tt.phone, tt.jobFunction, tt.roles); got != tt.want {
				t.Fatalf("validProfile = %v, want %v", got, tt.want)
			}
		})
	}
}
