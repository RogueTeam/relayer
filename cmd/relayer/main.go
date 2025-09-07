package main

import (
	"context"
	"log"
	"os"

	"github.com/RogueTeam/relayer/cmd/relayer/dht"
	"github.com/RogueTeam/relayer/cmd/relayer/run"
	"github.com/urfave/cli/v3"
)

var app = cli.Command{
	Name:        "relayer",
	Description: "Dead simple tool for communicating machine services without complex certificate infrastructure",

	Commands: []*cli.Command{
		dht.DHT,
		run.Run,
	},
}

func main() {
	err := app.Run(context.TODO(), os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
