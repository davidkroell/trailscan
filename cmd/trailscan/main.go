package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/davidkroell/trailscan"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		UsageText: "trailscan <track.gpx> [OPTIONS]",
		Name:      "trailscan",
		ArgsUsage: "<track.gpx>",
		Usage:     "a tool for analyzing GPX tracks and enriching them with data from OpenStreetMap.",
		Description: `trailscan is a tool for analyzing GPX tracks and enriching them with data from OpenStreetMap.
It matches recorded GPS positions against geographic features to determine where a track passes and what locations were visited.
The resulting track can be annotated with information such as mountain summits, landmarks, points of interest,
and other geographic features encountered along the route.`,
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:      "gpxfile",
				UsageText: "path to the gpx file",
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "overpass-endpoint",
				Aliases: []string{"e"},
				Value:   "https://overpass-api.de/api/interpreter",
				Usage:   "endpoint to use to send the overpass query",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "text",
				Usage:   "output format to use, supported output formats are [text, json]",
				Validator: func(s string) error {
					supported := []string{"text", "json"}
					if slices.Contains(supported, s) {
						return nil
					}

					return errors.New("supported output formats are [text, json]")
				},
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			file, err := os.Open(cmd.StringArg("gpxfile"))
			if err != nil {
				panic(err)
			}
			defer file.Close()

			points, bbox, err := trailscan.LoadGPX(file)
			if err != nil {
				panic(err)
			}

			fmt.Printf("Loaded %d track points\n", len(points))

			peaks, err := trailscan.FetchPeaks(bbox)
			if err != nil {
				panic(err)
			}

			fmt.Printf("Found %d peaks in bounding box\n", len(peaks))

			visited := trailscan.FindVisitedPeaks(points, peaks)

			fmt.Println("\nVisited Peaks")
			fmt.Println("=============")

			for _, v := range visited {
				fmt.Printf(
					"%s | peak=%.0fm | track=%.0fm | distance=%.1fm\n",
					v.Peak.Name,
					v.Peak.Ele,
					v.TrackElevation,
					v.Distance,
				)
			}
			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
