package buildinfo

import "strings"

var (
	Version   = "v1.2.1-dev"
	Revision  = "unknown"
	BuildTime = "unknown"
)

type Info struct {
	Version   string
	Revision  string
	BuildTime string
}

func Current() Info {
	return Info{
		Version:   normalize(Version, "dev"),
		Revision:  normalize(Revision, "unknown"),
		BuildTime: normalize(BuildTime, "unknown"),
	}
}

func normalize(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
