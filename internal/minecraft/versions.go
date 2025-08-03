package minecraft

import (
	"time"
)

// MinecraftRelease represents a Minecraft version release
type MinecraftRelease struct {
	Version string
	Date    string
}

// MinecraftReleases contains all Minecraft releases
var MinecraftReleases = []MinecraftRelease{
	{Version: "1.21.8", Date: "2025-07-17"},
	{Version: "1.21.7", Date: "2025-06-30"},
	{Version: "1.21.6", Date: "2025-06-17"},
	{Version: "1.21.5", Date: "2025-03-25"},
	{Version: "1.21.4", Date: "2024-12-03"},
	{Version: "1.21.3", Date: "2024-10-23"},
	{Version: "1.21.2", Date: "2024-10-22"},
	{Version: "1.21.1", Date: "2024-08-08"},
	{Version: "1.21", Date: "2024-06-13"},
	{Version: "1.20.6", Date: "2024-04-29"},
	{Version: "1.20.5", Date: "2024-04-23"},
	{Version: "1.20.4", Date: "2023-12-07"},
	{Version: "1.20.3", Date: "2023-12-04"},
	{Version: "1.20.2", Date: "2023-09-20"},
	{Version: "1.20.1", Date: "2023-06-12"},
	{Version: "1.20", Date: "2023-06-02"},
	{Version: "1.19.4", Date: "2023-03-14"},
	{Version: "1.19.3", Date: "2022-12-07"},
	{Version: "1.19.2", Date: "2022-08-05"},
	{Version: "1.19.1", Date: "2022-07-27"},
	{Version: "1.19", Date: "2022-06-07"},
	{Version: "1.18.2", Date: "2022-02-28"},
	{Version: "1.18.1", Date: "2021-12-10"},
	{Version: "1.18", Date: "2021-11-30"},
	{Version: "1.17.1", Date: "2021-07-06"},
	{Version: "1.17", Date: "2021-06-08"},
	{Version: "1.16.5", Date: "2021-01-14"},
	{Version: "1.16.4", Date: "2020-10-29"},
	{Version: "1.16.3", Date: "2020-09-10"},
	{Version: "1.16.2", Date: "2020-08-11"},
	{Version: "1.16.1", Date: "2020-06-24"},
	{Version: "1.16", Date: "2020-06-23"},
	{Version: "1.15.2", Date: "2020-01-17"},
	{Version: "1.15.1", Date: "2019-12-16"},
	{Version: "1.15", Date: "2019-12-09"},
	{Version: "1.14.4", Date: "2019-07-19"},
	{Version: "1.14.3", Date: "2019-06-24"},
	{Version: "1.14.2", Date: "2019-05-27"},
	{Version: "1.14.1", Date: "2019-05-13"},
	{Version: "1.14", Date: "2019-04-23"},
	{Version: "1.13.2", Date: "2018-10-22"},
	{Version: "1.13.1", Date: "2018-08-22"},
	{Version: "1.13", Date: "2018-07-18"},
	{Version: "1.12.2", Date: "2017-09-18"},
	{Version: "1.12.1", Date: "2017-08-03"},
	{Version: "1.12", Date: "2017-06-02"},
	{Version: "1.11.2", Date: "2016-12-21"},
	{Version: "1.11.1", Date: "2016-12-20"},
	{Version: "1.11", Date: "2016-11-14"},
	{Version: "1.10.2", Date: "2016-06-23"},
	{Version: "1.10.1", Date: "2016-06-22"},
	{Version: "1.10", Date: "2016-06-08"},
	{Version: "1.9.4", Date: "2016-05-10"},
	{Version: "1.9.3", Date: "2016-05-10"},
	{Version: "1.9.2", Date: "2016-03-30"},
	{Version: "1.9.1", Date: "2016-03-30"},
	{Version: "1.9", Date: "2016-02-29"},
	{Version: "1.8.9", Date: "2015-12-03"},
	{Version: "1.8.8", Date: "2015-07-27"},
	{Version: "1.8.7", Date: "2015-06-05"},
	{Version: "1.8.6", Date: "2015-05-25"},
	{Version: "1.8.5", Date: "2015-05-22"},
	{Version: "1.8.4", Date: "2015-04-17"},
	{Version: "1.8.3", Date: "2015-02-20"},
	{Version: "1.8.2", Date: "2015-02-19"},
	{Version: "1.8.1", Date: "2014-11-24"},
	{Version: "1.8", Date: "2014-09-02"},
	{Version: "1.7.10", Date: "2014-05-14"},
	{Version: "1.7.9", Date: "2014-04-14"},
	{Version: "1.7.8", Date: "2014-04-09"},
	{Version: "1.7.7", Date: "2014-04-09"},
	{Version: "1.7.6", Date: "2014-04-09"},
	{Version: "1.7.5", Date: "2014-02-26"},
	{Version: "1.7.4", Date: "2013-12-09"},
	{Version: "1.7.3", Date: "2013-12-06"},
	{Version: "1.7.2", Date: "2013-10-25"},
	{Version: "1.6.4", Date: "2013-09-19"},
	{Version: "1.6.2", Date: "2013-07-05"},
	{Version: "1.6.1", Date: "2013-06-28"},
	{Version: "1.5.2", Date: "2013-04-25"},
	{Version: "1.5.1", Date: "2013-03-20"},
	{Version: "1.4.7", Date: "2012-12-27"},
	{Version: "1.4.5", Date: "2012-12-19"},
	{Version: "1.4.6", Date: "2012-12-19"},
	{Version: "1.4.4", Date: "2012-12-13"},
	{Version: "1.4.2", Date: "2012-11-24"},
	{Version: "1.3.2", Date: "2012-08-15"},
	{Version: "1.3.1", Date: "2012-07-31"},
	{Version: "1.2.5", Date: "2012-03-29"},
	{Version: "1.2.4", Date: "2012-03-21"},
	{Version: "1.2.3", Date: "2012-03-01"},
	{Version: "1.2.2", Date: "2012-02-29"},
	{Version: "1.2.1", Date: "2012-02-29"},
	{Version: "1.1", Date: "2012-01-11"},
	{Version: "1.0", Date: "2011-11-17"},
}

// GetLatestVersion returns the latest Minecraft version
func GetLatestVersion() string {
	if len(MinecraftReleases) > 0 {
		return MinecraftReleases[0].Version
	}
	return "1.21.8"
}

// GetVersions returns a list of all Minecraft versions
func GetVersions() []string {
	versions := make([]string, len(MinecraftReleases))
	for i, release := range MinecraftReleases {
		versions[i] = release.Version
	}
	return versions
}

// IsValidVersion checks if a given version string is a valid Minecraft version
func IsValidVersion(version string) bool {
	for _, release := range MinecraftReleases {
		if release.Version == version {
			return true
		}
	}
	return false
}

// GetVersionDate returns the release date of a Minecraft version
func GetVersionDate(version string) (time.Time, error) {
	for _, release := range MinecraftReleases {
		if release.Version == version {
			return time.Parse("2006-01-02", release.Date)
		}
	}
	return time.Time{}, nil
}