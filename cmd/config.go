package cmd

import (
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
			Short: "Print the effective configuration as TOML",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, _, err := config.Load()
				if err != nil {
					return err
				}
				b, err := toml.Marshal(cfg)
				if err != nil {
					return err
				}
				_, err = os.Stdout.Write(b)
				return err
			},
		},
	)
	return c
}
