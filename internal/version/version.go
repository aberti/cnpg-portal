package version

// Version metadata, populated at build time by ldflags. See Makefile.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)
