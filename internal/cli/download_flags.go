package cli

import (
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/spf13/cobra"
)

func addDownloadFlags(cmd *cobra.Command) *downloadnet.Config {
	overrides := &downloadnet.Config{}
	cmd.Flags().StringVar(&overrides.Method, "download-method", "", "Download method: direct, environment, or accelerated")
	cmd.Flags().StringVar(&overrides.Prefix, "acceleration-prefix", "", "HTTP(S) prefix prepended to every complete GitHub URL in accelerated mode")
	return overrides
}

func configuredDownload(cmd *cobra.Command, cfg, overrides downloadnet.Config) (downloadnet.Config, error) {
	if cmd.Flags().Changed("download-method") {
		cfg.Method = overrides.Method
	}
	if cmd.Flags().Changed("acceleration-prefix") {
		cfg.Prefix = overrides.Prefix
		if cfg.Method != downloadnet.MethodAccelerated {
			return downloadnet.Config{}, diagnostic.New("diagnostic.cli.accelerationMethod", "--acceleration-prefix requires --download-method accelerated or saved accelerated settings")
		}
	}
	cfg = cfg.Normalized()
	return cfg, cfg.Validate()
}
