// Package coverage maps every method in the pinned API snapshots to the
// commands that call it, or records why it is skipped. Tests check this table
// against the vendored discovery documents, the command tree, and
// docs/api-coverage.md, so none of them can drift.
package coverage

// Snapshot is a vendored discovery document in testdata/.
type Snapshot struct {
	File, API, Revision, SHA256, Source string
}

// Captured is when the snapshots were downloaded.
const Captured = "2026-10-03"

var Snapshots = []Snapshot{
	{
		File: "searchconsole-v1.discovery.json", API: "Search Console API v1", Revision: "20260923",
		SHA256: "4de75278f7d4599b5f437d7b24dbb1957fa5c333d445945de97b3c6bdc4dc322",
		Source: "https://searchconsole.googleapis.com/$discovery/rest?version=v1",
	},
	{
		File: "indexing-v3.discovery.json", API: "Indexing API v3", Revision: "20260923",
		SHA256: "aed76b26295ecd5c5ef71aadcd02d404c633b13641ef7eb5b06a52386e30514d",
		Source: "https://indexing.googleapis.com/$discovery/rest?version=v3",
	},
}

type Status string

const (
	Implemented Status = "implemented"
	Retired     Status = "retired"
	Skipped     Status = "skipped"
)

type Entry struct {
	ID       string
	HTTP     string
	Path     string
	Status   Status
	Commands []string
	Reason   string
}

const indexingReason = "Separate Indexing API, limited by Google to pages with JobPosting or BroadcastEvent (in VideoObject) structured data and to owner-level service accounts, with abuse enforcement. Excluded by decision; see PLAN.md."

var Entries = []Entry{
	{ID: "webmasters.sites.list", HTTP: "GET", Path: "webmasters/v3/sites", Status: Implemented,
		Commands: []string{"gsc sites list"}},
	{ID: "webmasters.sites.get", HTTP: "GET", Path: "webmasters/v3/sites/{siteUrl}", Status: Implemented,
		Commands: []string{"gsc sites get"}},
	{ID: "webmasters.sites.add", HTTP: "PUT", Path: "webmasters/v3/sites/{siteUrl}", Status: Implemented,
		Commands: []string{"gsc sites add"}},
	{ID: "webmasters.sites.delete", HTTP: "DELETE", Path: "webmasters/v3/sites/{siteUrl}", Status: Implemented,
		Commands: []string{"gsc sites remove"}},
	{ID: "webmasters.sitemaps.list", HTTP: "GET", Path: "webmasters/v3/sites/{siteUrl}/sitemaps", Status: Implemented,
		Commands: []string{"gsc sitemaps list"}},
	{ID: "webmasters.sitemaps.get", HTTP: "GET", Path: "webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}", Status: Implemented,
		Commands: []string{"gsc sitemaps get"}},
	{ID: "webmasters.sitemaps.submit", HTTP: "PUT", Path: "webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}", Status: Implemented,
		Commands: []string{"gsc sitemaps submit"}},
	{ID: "webmasters.sitemaps.delete", HTTP: "DELETE", Path: "webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}", Status: Implemented,
		Commands: []string{"gsc sitemaps delete"}},
	{ID: "webmasters.searchanalytics.query", HTTP: "POST", Path: "webmasters/v3/sites/{siteUrl}/searchAnalytics/query", Status: Implemented,
		Commands: []string{"gsc query", "gsc performance", "gsc top queries", "gsc top pages", "gsc top countries", "gsc top devices",
			"gsc top appearance", "gsc trend", "gsc freshness", "gsc export", "gsc insights striking-distance", "gsc insights low-ctr",
			"gsc insights cannibalization", "gsc insights decliners", "gsc insights new-queries", "gsc insights lost-queries"}},
	{ID: "searchconsole.urlInspection.index.inspect", HTTP: "POST", Path: "v1/urlInspection/index:inspect", Status: Implemented,
		Commands: []string{"gsc inspect"}},
	{ID: "searchconsole.urlTestingTools.mobileFriendlyTest.run", HTTP: "POST", Path: "v1/urlTestingTools/mobileFriendlyTest:run", Status: Retired,
		Reason: "Google retired the Mobile-Friendly Test API on 2023-12-01. The method is still in the discovery document but no longer operates."},
	{ID: "indexing.urlNotifications.publish", HTTP: "POST", Path: "v3/urlNotifications:publish", Status: Skipped, Reason: indexingReason},
	{ID: "indexing.urlNotifications.getMetadata", HTTP: "GET", Path: "v3/urlNotifications/metadata", Status: Skipped, Reason: indexingReason},
}
