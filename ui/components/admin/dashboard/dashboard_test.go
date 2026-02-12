package dashboard

import "testing"

func TestIsStorefrontReady(t *testing.T) {
	t.Parallel()

	base := RepoStatus{
		StripeReady:     true,
		EmailConfigured: true,
		YAMLExists:      true,
		YAMLValid:       true,
		TemplateExists:  true,
		TemplateValid:   true,
	}

	tests := []struct {
		name   string
		status *RepoStatus
		want   bool
	}{
		{
			name:   "nil status",
			status: nil,
			want:   false,
		},
		{
			name: "missing stripe readiness",
			status: &RepoStatus{
				StripeReady:     false,
				EmailConfigured: true,
				YAMLExists:      true,
				YAMLValid:       true,
				TemplateExists:  true,
				TemplateValid:   true,
			},
			want: false,
		},
		{
			name: "missing email configuration",
			status: &RepoStatus{
				StripeReady:     true,
				EmailConfigured: false,
				YAMLExists:      true,
				YAMLValid:       true,
				TemplateExists:  true,
				TemplateValid:   true,
			},
			want: false,
		},
		{
			name: "template has mismatches",
			status: &RepoStatus{
				StripeReady:             true,
				EmailConfigured:         true,
				YAMLExists:              true,
				YAMLValid:               true,
				TemplateExists:          true,
				TemplateValid:           true,
				TemplatePriceMismatches: []string{"SKU_A"},
			},
			want: false,
		},
		{
			name:   "all checks green",
			status: &base,
			want:   true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isStorefrontReady(tt.status)
			if got != tt.want {
				t.Fatalf("isStorefrontReady() = %v, want %v", got, tt.want)
			}
		})
	}
}
