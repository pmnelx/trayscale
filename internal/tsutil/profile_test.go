package tsutil

import (
	"testing"

	"deedles.dev/trayscale/internal/metadata"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"
)

func testProfile(tailnet, account string) ipn.LoginProfile {
	return ipn.LoginProfile{
		ID:             "d7b3",
		Name:           account,
		NetworkProfile: ipn.NetworkProfile{DomainName: tailnet},
	}
}

func TestProfileLabel(t *testing.T) {
	tests := []struct {
		name    string
		profile ipn.LoginProfile
		want    string
	}{
		{"tailnet and account", testProfile("itselfhelp.com", "paul@itselfhelp.com"), "itselfhelp.com (paul@itselfhelp.com)"},
		{"account only", testProfile("", "paul@itselfhelp.com"), "paul@itselfhelp.com"},
		{"tailnet only", testProfile("itselfhelp.com", ""), "itselfhelp.com"},
		{"neither", testProfile("", ""), "d7b3"},
		{
			"display name beats domain",
			ipn.LoginProfile{Name: "a", NetworkProfile: ipn.NetworkProfile{DomainName: "d", DisplayName: "n"}},
			"n (a)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ProfileLabel(tt.profile))
		})
	}
}

func TestProfileLabelPrivate(t *testing.T) {
	metadata.Private = true
	defer func() { metadata.Private = false }()

	p := testProfile("itselfhelp.com", "paul@itselfhelp.com")
	require.Equal(t, "", ProfileTailnet(p))
	require.Equal(t, "profile@example.com", ProfileAccount(p))
	require.Equal(t, "profile@example.com", ProfileLabel(p))
}
