package services

import (
	"testing"

	"github.com/google/go-github/v66/github"
)

func TestIsOrderIssue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		issue *github.Issue
		want  bool
	}{
		{
			name: "label based order issue",
			issue: &github.Issue{
				Labels: []*github.Label{{Name: github.String("gitshop:order")}},
			},
			want: true,
		},
		{
			name: "marker based order issue",
			issue: &github.Issue{
				Body: github.String("# gitshop:order-template"),
			},
			want: true,
		},
		{
			name: "generic text is not enough",
			issue: &github.Issue{
				Body: github.String("These are order details for a discussion thread."),
			},
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsOrderIssue(tc.issue)
			if got != tc.want {
				t.Fatalf("isOrderIssue() = %v, want %v", got, tc.want)
			}
		})
	}
}
