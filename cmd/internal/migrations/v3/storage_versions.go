package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// storageMinimumVersions maps storage package names to their minimum required major version
// for Fiber v3 compatibility. These represent the target versions that storage packages
// should be migrated to based on their latest stable releases in the gofiber/storage repository.
var storageMinimumVersions = map[string]string{
	// v3 adapters (minimum required major version)
	"postgres": "v3", // Latest: v3.3.1
	"redis":    "v3", // Latest: v3.4.2

	// v2 adapters (minimum required major version)
	"arangodb":  "v2", // Latest: v2.2.2
	"azureblob": "v2", // Latest: v2.2.2
	"badger":    "v2", // Latest: v2.1.2
	"bbolt":     "v2", // Latest: v2.1.2
	"couchbase": "v2", // Latest: v2.2.2
	"dynamodb":  "v2", // Latest: v2.2.2
	"etcd":      "v2", // Latest: v2.2.0
	"memcache":  "v2", // Latest: v2.1.1
	"memory":    "v2", // Latest: v2.1.1
	"mongodb":   "v2", // Latest: v2.2.1
	"mssql":     "v2", // Latest: v2.1.2
	"mysql":     "v2", // Latest: v2.3.0
	"pebble":    "v2", // Latest: v2.1.1
	"ristretto": "v2", // Latest: v2.1.1
	"s3":        "v2", // Latest: v2.4.2
	"sqlite3":   "v2", // Latest: v2.2.2

	// Note: v1/unversioned adapters are intentionally excluded as they don't
	// require migration. These packages remain on v0.x or v1.x versions:
	// aerospike, cassandra, clickhouse, cloudflarekv, coherence, leveldb,
	// minio, mockstorage, nats, neo4j, rueidis, scylladb, surrealdb, valkey
}

// MigrateStorageVersions updates storage package imports to use the correct latest version.
// This migration handles storage packages from github.com/gofiber/storage/*.
func MigrateStorageVersions(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	// Regex to match storage imports with or without version suffix
	// Examples:
	//   "github.com/gofiber/storage/sqlite3"
	//   "github.com/gofiber/storage/redis/v2"
	//   "github.com/gofiber/storage/postgres/v3"
	reStorageImport := regexp.MustCompile(`"github\.com/gofiber/storage/([a-zA-Z0-9_-]+)(?:/v(\d+))?"`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		// Replace storage imports to add or update the version suffix
		return reStorageImport.ReplaceAllStringFunc(content, func(match string) string {
			// Extract the storage package name and current version
			submatches := reStorageImport.FindStringSubmatch(match)
			if len(submatches) < 2 {
				return match
			}
			storagePkg := submatches[1]
			currentVersion := ""
			if len(submatches) > 2 && submatches[2] != "" {
				currentVersion = "v" + submatches[2]
			}

			// Get the minimum required version for this storage package
			minVersion, ok := storageMinimumVersions[storagePkg]
			if !ok {
				// Unknown package - leave unchanged
				return match
			}

			// If already at the correct version, skip
			if currentVersion == minVersion {
				return match
			}

			// Return the updated import with the correct version
			return fmt.Sprintf(`"github.com/gofiber/storage/%s/%s"`, storagePkg, minVersion)
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate storage versions: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating storage package versions")
	return nil
}
