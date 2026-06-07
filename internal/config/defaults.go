package config

const (
	DefaultPanelGitHubRepository = "https://github.com/kittors/codeProxy"
	DefaultPprofAddr             = "127.0.0.1:8316"
	DefaultAutoUpdateChannel     = "main"
	DefaultAutoUpdateRepository  = "https://github.com/kittors/CliRelay"
	DefaultAutoUpdateDockerImage = "ghcr.io/kittors/clirelay"
	DefaultAutoUpdateUpdaterURL  = "http://clirelay-updater:8320"

	// EnvAuthPath overrides auth-dir with the path visible inside the running container/process.
	EnvAuthPath = "AUTH_PATH"
)
