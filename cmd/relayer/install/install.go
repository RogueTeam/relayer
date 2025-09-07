package install

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

var Install = &cli.Command{
	Name:        "install",
	Description: "Installs relayer as a service in the system. It also creates the necessary user",
	Action: func(ctx context.Context, c *cli.Command) (err error) {
		if isSystemd() {
			err = installSystemd()
			if err != nil {
				return fmt.Errorf("failed to install in systemd based: %w", err)
			}
			return nil
		}
		return nil
	},
}
