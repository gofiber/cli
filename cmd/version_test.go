package cmd

import (
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Version_Printer(t *testing.T) {
	at := assert.New(t)
	t.Run("success", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		clearHTTPCache()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, fakeVersionResponse))
		httpmock.RegisterResponder(http.MethodGet, latestCliVersionURL, httpmock.NewBytesResponder(200, fakeCliVersionResponse("1.2.3")))

		out, err := runCobraCmd(versionCmd)
		require.NoError(t, err)
		at.Contains(out, "2.0.6")
		at.Contains(out, "fiber cli version:")
		at.Contains(out, "latest 1.2.3")
	})

	t.Run("latest err", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		clearHTTPCache()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, []byte("no version")))

		out, err := runCobraCmd(versionCmd)
		require.NoError(t, err)
		at.Contains(out, "no version")
	})

	t.Run("cli latest err", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		clearHTTPCache()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, fakeVersionResponse))
		httpmock.RegisterResponder(http.MethodGet, latestCliVersionURL, httpmock.NewErrorResponder(errors.New("cli network error")))

		out, err := runCobraCmd(versionCmd)
		require.NoError(t, err)
		at.Contains(out, "latest check failed")
		at.Contains(out, "cli network error")
	})
}

func Test_Version_Current(t *testing.T) {
	at := assert.New(t)

	t.Run("file not found", func(t *testing.T) {
		setupCurrentVersionFile()
		defer teardownCurrentVersionFile()

		_, err := currentVersion()
		require.Error(t, err)
	})

	t.Run("match version", func(t *testing.T) {
		content := `module fiber-demo
go 1.14
require (
	github.com/gofiber/fiber/v2 v2.0.6
	github.com/jarcoal/httpmock v1.0.6
)`

		setupCurrentVersionFile(content)
		defer teardownCurrentVersionFile()

		v, err := currentVersion()
		require.NoError(t, err)
		at.Equal("v2.0.6", v)
	})

	t.Run("match master", func(t *testing.T) {
		content := `module fiber-demo
go 1.14
require (
	github.com/gofiber/fiber v0.0.0-20200926082917-55763e7e6ee3
	github.com/jarcoal/httpmock v1.0.6
)`

		setupCurrentVersionFile(content)
		defer teardownCurrentVersionFile()

		v, err := currentVersion()
		require.NoError(t, err)
		at.Equal("v0.0.0-20200926082917-55763e7e6ee3", v)
	})

	t.Run("can read version from windows clrf file", func(t *testing.T) {
		content := "module fiber-demo\r\ngo 1.14\r\nrequire (\r\n" +
			"\tgithub.com/gofiber/fiber\r v0.0.0-20200926082917-55763e7e6ee3\r\n" +
			"\tgithub.com/jarcoal/httpmock v1.0.6\r\n" +
			")"

		setupCurrentVersionFile(content)
		defer teardownCurrentVersionFile()

		v, err := currentVersion()
		require.NoError(t, err)
		at.Equal("v0.0.0-20200926082917-55763e7e6ee3", v)
	})

	t.Run("package not found", func(t *testing.T) {
		content := `module fiber-demo
go 1.14
require (
	github.com/jarcoal/httpmock v1.0.6
)`

		setupCurrentVersionFile(content)
		defer teardownCurrentVersionFile()

		_, err := currentVersion()
		require.Error(t, err)
	})
}

func setupCurrentVersionFile(content ...string) {
	currentVersionFile = "current-version"
	if len(content) > 0 {
		if err := os.WriteFile(currentVersionFile, []byte(content[0]), 0o600); err != nil {
			panic(err)
		}
	}
}

func teardownCurrentVersionFile() {
	if err := os.Remove(currentVersionFile); err != nil && !os.IsNotExist(err) {
		panic(err)
	}
	currentVersionFile = "go.mod"
}

func Test_Version_Latest(t *testing.T) {
	at := assert.New(t)
	t.Run("http get error", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		clearHTTPCache()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewErrorResponder(errors.New("network error")))

		_, err := LatestFiberVersion()
		require.Error(t, err)
	})

	t.Run("version matched", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		clearHTTPCache()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, fakeVersionResponse))

		v, err := LatestFiberVersion()
		require.NoError(t, err)
		at.Equal("2.0.6", v)
	})

	t.Run("no version matched", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		clearHTTPCache()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, []byte("no version")))

		_, err := LatestFiberVersion()
		require.Error(t, err)
	})
}

var latestVersionURL = "https://api.github.com/repos/gofiber/fiber/releases/latest"

var fakeVersionResponse = []byte(`{ "url": "https://api.github.com/repos/gofiber/fiber/releases/32189569", "assets_url": "https://api.github.com/repos/gofiber/fiber/releases/32189569/assets", "upload_url": "https://uploads.github.com/repos/gofiber/fiber/releases/32189569/assets{?name,label}", "html_url": "https://github.com/gofiber/fiber/releases/tag/v2.0.6", "id": 32189569, "node_id": "MDc6UmVsZWFzZTMyMTg5NTY5", "tag_name": "v2.0.6", "target_commitish": "master", "name": "v2.0.6", "draft": false, "author": { "login": "Fenny", "id": 25108519, "node_id": "MDQ6VXNlcjI1MTA4NTE5", "avatar_url": "https://avatars1.githubusercontent.com/u/25108519?v=4", "gravatar_id": "", "url": "https://api.github.com/users/Fenny", "html_url": "https://github.com/Fenny", "followers_url": "https://api.github.com/users/Fenny/followers", "following_url": "https://api.github.com/users/Fenny/following{/other_user}", "gists_url": "https://api.github.com/users/Fenny/gists{/gist_id}", "starred_url": "https://api.github.com/users/Fenny/starred{/owner}{/repo}", "subscriptions_url": "https://api.github.com/users/Fenny/subscriptions", "organizations_url": "https://api.github.com/users/Fenny/orgs", "repos_url": "https://api.github.com/users/Fenny/repos", "events_url": "https://api.github.com/users/Fenny/events{/privacy}", "received_events_url": "https://api.github.com/users/Fenny/received_events", "type": "User", "site_admin": false }, "prerelease": false, "created_at": "2020-10-05T19:54:02Z", "published_at": "2020-10-05T22:10:27Z", "assets": [], "tarball_url": "https://api.github.com/repos/gofiber/fiber/tarball/v2.0.6", "zipball_url": "https://api.github.com/repos/gofiber/fiber/zipball/v2.0.6" }`)

func Test_LatestVersion_Cache(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	clearHTTPCache()

	httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, fakeVersionResponse))

	_, err := LatestFiberVersion()
	require.NoError(t, err)

	_, err = LatestFiberVersion()
	require.NoError(t, err)

	info := httpmock.GetCallCountInfo()
	require.Equal(t, 1, info["GET "+latestVersionURL])
}
