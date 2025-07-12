package cmd

import (
	"bytes"
	"errors"
	"net/http"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Upgrade_upgradeRunE(t *testing.T) {
	t.Parallel()
	at := assert.New(t)

	b := &bytes.Buffer{}
	upgradeCmd.SetErr(b)
	upgradeCmd.SetOut(b)

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder(http.MethodGet, latestCliVersionURL, httpmock.NewErrorResponder(errors.New("network error")))

	require.Error(t, upgradeRunE(upgradeCmd, nil))

	httpmock.RegisterResponder(http.MethodGet, latestCliVersionURL, httpmock.NewBytesResponder(200, fakeCliVersionResponse()))

	setupSpinner()
	defer teardownSpinner()

	require.NoError(t, upgradeRunE(upgradeCmd, nil))

	at.Contains(b.String(), "99.99.99")

	httpmock.RegisterResponder(http.MethodGet, latestCliVersionURL, httpmock.NewBytesResponder(200, fakeCliVersionResponse(version)))

	b.Reset()

	require.NoError(t, upgradeRunE(upgradeCmd, nil))

	at.Contains(b.String(), "Currently")
}

func Test_Upgrade_upgrade(t *testing.T) {
	t.Parallel()
	at := assert.New(t)

	b := &bytes.Buffer{}
	upgradeCmd.SetErr(b)
	upgradeCmd.SetOut(b)

	upgrade(upgradeCmd, "99.99.99")

	at.Contains(b.String(), "failed to upgrade")
}
