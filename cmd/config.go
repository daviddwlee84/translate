package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/config"
)

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Show or locate the configuration",
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "path",
			Short: "Print the config file path",
			RunE: func(cmd *cobra.Command, _ []string) error {
				fmt.Println(config.Path())
				return nil
			},
		},
		&cobra.Command{
			Use:   "show",
			Short: "Print the effective configuration as TOML (or JSON with --json)",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, _, err := config.Load()
				if err != nil {
					return err
				}
				b, err := toml.Marshal(cfg)
				if err != nil {
					return err
				}
				if flagJSON {
					// Round-trip TOML → map → JSON so the JSON keys match the TOML
					// (snake_case, e.g. general.default_target) without adding json
					// tags to every config struct. Lets other front-ends (the Raycast
					// extension) read the effective config.
					var m map[string]any
					if err := toml.Unmarshal(b, &m); err != nil {
						return err
					}
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(m)
				}
				_, err = os.Stdout.Write(b)
				return err
			},
		},
	)
	return c
}
