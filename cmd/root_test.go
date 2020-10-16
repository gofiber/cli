package cmd

import (
	"bytes"
	"errors"
	"net/http"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
)

func Test_Root_Execute(t *testing.T) {
	setupOsExit()
	defer teardownOsExit()

	b := &bytes.Buffer{}
	rootCmd.SetErr(b)
	rootCmd.SetOut(b)

	Execute()
}

func Test_Root_RunE(t *testing.T) {
	b := &bytes.Buffer{}
	rootCmd.SetErr(b)
	rootCmd.SetOut(b)

	assert.Nil(t, rootRunE(rootCmd, nil))
}

func Test_Root_RootPersistentPostRun(t *testing.T) {
	at := assert.New(t)

	b := &bytes.Buffer{}
	rootCmd.SetErr(b)
	rootCmd.SetOut(b)

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder(http.MethodGet, latestCliVersionUrl, httpmock.NewErrorResponder(errors.New("network error")))

	rootPersistentPostRun(rootCmd, nil)

	at.Equal(0, b.Len())

	httpmock.RegisterResponder(http.MethodGet, latestCliVersionUrl, httpmock.NewBytesResponder(200, fakeCliVersionResponse))

	rootPersistentPostRun(rootCmd, nil)

	at.Contains(b.String(), "WARNING")
}

var latestCliVersionUrl = "https://api.github.com/repos/gofiber/fiber-cli/releases/latest"

var fakeCliVersionResponse = []byte(`{ "assets": [], "assets_url": "https://api.github.com/repos/gofiber/fiber-cli/releases/32630724/assets", "author": { "avatar_url": "https://avatars1.githubusercontent.com/u/1214670?v=4", "events_url": "https://api.github.com/users/kiyonlin/events{/privacy}", "followers_url": "https://api.github.com/users/kiyonlin/followers", "following_url": "https://api.github.com/users/kiyonlin/following{/other_user}", "gists_url": "https://api.github.com/users/kiyonlin/gists{/gist_id}", "gravatar_id": "", "html_url": "https://github.com/kiyonlin", "id": 1214670, "login": "kiyonlin", "node_id": "MDQ6VXNlcjEyMTQ2NzA=", "organizations_url": "https://api.github.com/users/kiyonlin/orgs", "received_events_url": "https://api.github.com/users/kiyonlin/received_events", "repos_url": "https://api.github.com/users/kiyonlin/repos", "site_admin": false, "starred_url": "https://api.github.com/users/kiyonlin/starred{/owner}{/repo}", "subscriptions_url": "https://api.github.com/users/kiyonlin/subscriptions", "type": "User", "url": "https://api.github.com/users/kiyonlin" }, "created_at": "2020-10-15T15:58:55Z", "draft": false, "html_url": "https://github.com/gofiber/fiber-cli/releases/tag/v99.99.99", "id": 32630724, "name": "v99.99.99", "node_id": "MDc6UmVsZWFzZTMyNjMwNzI0", "prerelease": false, "published_at": "2020-10-15T16:09:05Z", "tag_name": "v99.99.99", "tarball_url": "https://api.github.com/repos/gofiber/fiber-cli/tarball/v99.99.99", "target_commitish": "master", "upload_url": "https://uploads.github.com/repos/gofiber/fiber-cli/releases/32630724/assets{?name,label}", "url": "https://api.github.com/repos/gofiber/fiber-cli/releases/32630724", "zipball_url": "https://api.github.com/repos/gofiber/fiber-cli/zipball/v99.99.99"}`)
