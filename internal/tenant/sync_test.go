package tenant

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSync_validation(t *testing.T) {
	cases := []struct {
		name     string
		src, dst string
		opts     SyncOptions
		wantErr  string
	}{
		{
			name:    "src not IdentSafe",
			src:     "Bad-Name",
			dst:     "ok",
			opts:    SyncOptions{Confirmed: true},
			wantErr: "invalid source name",
		},
		{
			name:    "dst not IdentSafe",
			src:     "ok",
			dst:     "1bad",
			opts:    SyncOptions{Confirmed: true},
			wantErr: "invalid destination name",
		},
		{
			name:    "src equals dst",
			src:     "same",
			dst:     "same",
			opts:    SyncOptions{Confirmed: true},
			wantErr: "src and dst must differ",
		},
		{
			name:    "unconfirmed",
			src:     "src",
			dst:     "dst",
			opts:    SyncOptions{Confirmed: false},
			wantErr: "confirmation required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Sync(context.Background(), Deps{}, tc.src, tc.dst, tc.opts)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestSync_unconfirmedReturnsSentinel(t *testing.T) {
	err := Sync(context.Background(), Deps{}, "src", "dst", SyncOptions{Confirmed: false})
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("expected ErrConfirmationRequired, got %v", err)
	}
}
