package tsutil

import (
	"os"
	"path/filepath"
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

// fakeSystemctl puts a fake systemctl first in PATH, so that the test
// can't restart the real daemon. It records its arguments in the
// returned file.
func fakeSystemctl(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	content := "#!/bin/sh\necho \"$@\" > " + args + "\n" + script + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "systemctl"), []byte(content), 0o755))
	t.Setenv("PATH", dir)
	return args
}

func TestRestartDaemon(t *testing.T) {
	args := fakeSystemctl(t, "exit 0")
	require.NoError(t, RestartDaemon(t.Context()))

	got, err := os.ReadFile(args)
	require.NoError(t, err)
	require.Equal(t, "restart tailscaled.service\n", string(got))
}

func TestRestartDaemonFailure(t *testing.T) {
	fakeSystemctl(t, `echo "Access denied" >&2; exit 1`)
	err := RestartDaemon(t.Context())
	require.ErrorContains(t, err, "Access denied")
}

func TestCanRestartDaemonNeedsSystemctl(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	require.False(t, CanRestartDaemon())
}
