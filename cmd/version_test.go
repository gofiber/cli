package cmd

import (
	"errors"
	"io/ioutil"
	"net/http"
	"os"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
)

func Test_Version_Printer(t *testing.T) {
	at := assert.New(t)
	t.Run("success", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, fakeVersionResponse))

		out, err := runCobraCmd(versionCmd)
		at.Nil(err)
		at.Contains(out, "2.0.6")
	})

	t.Run("latest err", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, []byte("no version")))

		out, err := runCobraCmd(versionCmd)
		at.Nil(err)
		at.Contains(out, "no version")
	})
}

func Test_Version_Current(t *testing.T) {
	at := assert.New(t)

	t.Run("file not found", func(t *testing.T) {
		setupCurrentVersionFile()
		defer teardownCurrentVersionFile()

		_, err := currentVersion()
		at.NotNil(err)
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
		at.Nil(err)
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
		at.Nil(err)
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
		at.NotNil(err)
	})
}

func setupCurrentVersionFile(content ...string) {
	currentVersionFile = "current-version"
	if len(content) > 0 {
		_ = ioutil.WriteFile(currentVersionFile, []byte(content[0]), 0600)
	}
}

func teardownCurrentVersionFile() {
	_ = os.Remove(currentVersionFile)
}

func Test_Version_Latest(t *testing.T) {
	at := assert.New(t)
	t.Run("http get error", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewErrorResponder(errors.New("network error")))

		_, err := latestVersion(false)
		at.NotNil(err)
	})

	t.Run("version matched", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, fakeVersionResponse))

		v, err := latestVersion(false)
		at.Nil(err)
		at.Equal("2.0.6", v)
	})

	t.Run("no version matched", func(t *testing.T) {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()

		httpmock.RegisterResponder(http.MethodGet, latestVersionURL, httpmock.NewBytesResponder(200, []byte("no version")))

		_, err := latestVersion(false)
		at.NotNil(err)
	})
}

var latestVersionURL = "https://api.github.com/repos/gofiber/fiber/releases/latest"

var fakeVersionResponse = []byte(`{ "url": "https://api.github.com/repos/gofiber/fiber/releases/32189569", "assets_url": "https://api.github.com/repos/gofiber/fiber/releases/32189569/assets", "upload_url": "https://uploads.github.com/repos/gofiber/fiber/releases/32189569/assets{?name,label}", "html_url": "https://github.com/gofiber/fiber/releases/tag/v2.0.6", "id": 32189569, "node_id": "MDc6UmVsZWFzZTMyMTg5NTY5", "tag_name": "v2.0.6", "target_commitish": "master", "name": "v2.0.6", "draft": false, "author": { "login": "Fenny", "id": 25108519, "node_id": "MDQ6VXNlcjI1MTA4NTE5", "avatar_url": "https://avatars1.githubusercontent.com/u/25108519?v=4", "gravatar_id": "", "url": "https://api.github.com/users/Fenny", "html_url": "https://github.com/Fenny", "followers_url": "https://api.github.com/users/Fenny/followers", "following_url": "https://api.github.com/users/Fenny/following{/other_user}", "gists_url": "https://api.github.com/users/Fenny/gists{/gist_id}", "starred_url": "https://api.github.com/users/Fenny/starred{/owner}{/repo}", "subscriptions_url": "https://api.github.com/users/Fenny/subscriptions", "organizations_url": "https://api.github.com/users/Fenny/orgs", "repos_url": "https://api.github.com/users/Fenny/repos", "events_url": "https://api.github.com/users/Fenny/events{/privacy}", "received_events_url": "https://api.github.com/users/Fenny/received_events", "type": "User", "site_admin": false }, "prerelease": false, "created_at": "2020-10-05T19:54:02Z", "published_at": "2020-10-05T22:10:27Z", "assets": [], "tarball_url": "https://api.github.com/repos/gofiber/fiber/tarball/v2.0.6", "zipball_url": "https://api.github.com/repos/gofiber/fiber/zipball/v2.0.6" }`)
