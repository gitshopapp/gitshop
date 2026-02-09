package handlers

import "testing"

func TestParseInstallationID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    int64
		wantErr bool
	}{
		{name: "valid", value: "12345", want: 12345},
		{name: "empty", value: "", wantErr: true},
		{name: "zero", value: "0", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "not numeric", value: "abc", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseInstallationID(tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value=%q)", tc.value)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected installation id: got=%d want=%d", got, tc.want)
			}
		})
	}
}
