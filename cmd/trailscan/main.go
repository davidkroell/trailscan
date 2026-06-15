package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"text/tabwriter"

	"github.com/davidkroell/trailscan"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		UsageText: "trailscan <track.gpx> [OPTIONS]",
		Name:      "trailscan",
		ArgsUsage: "<track.gpx>",
		Usage:     "a tool for analyzing GPX tracks using data from OpenStreetMap.",
		Description: `trailscan is a tool for analyzing GPX tracks using data from OpenStreetMap.
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
				Name:  "overpass-query-type",
				Value: "peaks",
				Usage: "uses a predefined query, supported queries are [peaks, villages, hiking, cycling]",
				Validator: func(s string) error {
					supported := []string{"peaks", "villages", "hiking", "cycling"}
					if slices.Contains(supported, s) {
						return nil
					}

					return errors.New("supported queries are [peaks, villages, hiking, cycling]")
				},
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
			&cli.Float64Flag{
				Name:    "max-distance",
				Aliases: []string{"md"},
				Value:   50,
				Usage:   "configure the maximum distance (in meters) between tracked and actual",
				Validator: func(v float64) error {
					if v < 1 || v > 1000 {
						return errors.New("maximum distance must be between 1 and 1000")
					}
					return nil
				},
			},
			&cli.Float64Flag{
				Name:    "max-elevation-diff",
				Aliases: []string{"me"},
				Value:   30,
				Usage:   "configure the maximum elevation difference (in meters) between tracked and actual",
				Validator: func(v float64) error {
					if v < 1 || v > 1000 {
						return errors.New("maximum elevation difference must be between 1 and 1000")
					}
					return nil
				},
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			file, err := os.Open(cmd.StringArg("gpxfile"))
			if err != nil {
				return fmt.Errorf("cannot open gpx file: %w", err)
			}
			defer file.Close()

			maxDistanceFlag := cmd.Float64("max-distance")
			points, bbox, err := trailscan.LoadGPX(file, maxDistanceFlag/4)
			if err != nil {
				return fmt.Errorf("cannot load gpx file: %w", err)
			}

			fetchOptions := trailscan.DefaultFetchOptions()
			fetchOptions.Endpoint = cmd.String("overpass-endpoint")
			switch cmd.String("overpass-query-type") {
			case "peaks":
				fetchOptions.QueryTemplate = trailscan.PeaksQueryTemplate
			case "villages":
				fetchOptions.QueryTemplate = trailscan.VillagesQueryTemplate
			case "hiking":
				fetchOptions.QueryTemplate = trailscan.HikingQueryTemplate
			case "cycling":
				fetchOptions.QueryTemplate = trailscan.CyclingQueryTemplate
			}

			amenities, err := trailscan.FetchAmenities(ctx, bbox, fetchOptions)
			if err != nil {
				return fmt.Errorf("cannot fetch amenities: %w", err)
			}

			findOpts := trailscan.DefaultFindOptions()
			findOpts.MaxDistanceMeters = maxDistanceFlag
			findOpts.MaxElevationDifference = cmd.Float64("max-elevation-diff")

			visited := trailscan.FindVisitedAmenities(points, amenities, findOpts)

			switch cmd.String("output") {
			case "text":
				w := tabwriter.NewWriter(cmd.Writer, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintf(w, "NUM\tNAME\tTYPE\tLAT\tLON\tREAL ELEVATION\tTRACKED ELEVATION\tDISTANCE\n")

				for _, v := range visited {
					_, _ = fmt.Fprintf(w,
						"%3d\t%s\t%s\t%0.5f\t%0.5f\t%.0fm\t%.0fm\t%.1fm\n",
						v.VisitedIndex,
						v.Amenity.Name,
						v.Amenity.Type,
						v.Amenity.Lat,
						v.Amenity.Lon,
						v.Amenity.Ele,
						v.TrackElevation,
						v.Distance,
					)
				}

				_ = w.Flush()
			case "json":
				_ = json.NewEncoder(cmd.Writer).Encode(visited)
			}
			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
