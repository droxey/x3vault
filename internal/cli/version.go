package cli

// Version and Commit are set at link time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
)
