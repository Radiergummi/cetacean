package version

// Set via ldflags at build time:
//
//	go build -ldflags "-X github.com/radiergummi/cetacean/internal/version.Version=v1.0.0 ..."
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
