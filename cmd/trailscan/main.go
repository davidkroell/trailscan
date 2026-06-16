package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"text/tabwriter"

	"github.com/davidkroell/trailscan"
	"github.com/urfave/cli/v3"
)

func main() {
	supportedQueryTemplates := make([]string, 0, len(trailscan.AllTemplates))
	for name := range trailscan.AllTemplates {
		supportedQueryTemplates = append(supportedQueryTemplates, name)
	}
	sort.Strings(supportedQueryTemplates)

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
				Name:    "endpoint",
				Aliases: []string{"e"},
				Value:   "https://overpass-api.de/api/interpreter",
				Usage:   "endpoint to use to send the overpass query",
			},
			&cli.StringFlag{
				Name:    "query-template",
				Aliases: []string{"q"},
				Value:   "peaks",
				Usage:   fmt.Sprintf("use a predefined query, supported queries are %v, or a path to a valid query template file", supportedQueryTemplates),
				Validator: func(s string) error {
					if slices.Contains(supportedQueryTemplates, s) {
						return nil
					}

					f, err := os.Open(s)
					if err == nil {
						f.Close()
						return nil
					}

					return fmt.Errorf("supported queries are %v, or a path to a valid query template file", supportedQueryTemplates)
				},
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "text",
				Usage:   "output format to use, supported output formats are [text json]",
				Validator: func(s string) error {
					supported := []string{"text", "json"}
					if slices.Contains(supported, s) {
						return nil
					}

					return errors.New("supported output formats are [text json]")
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
			&cli.BoolFlag{
				Name:  "no-simplify-gpx",
				Value: false,
				Usage: "disables simplifying GPX tracks (disables speedup)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			file, err := os.Open(cmd.StringArg("gpxfile"))
			if err != nil {
				return fmt.Errorf("cannot open gpx file: %w", err)
			}
			defer file.Close()

			maxDistanceFlag := cmd.Float64("max-distance")
			reduceFlag := maxDistanceFlag
			if cmd.Bool("no-simplify-gpx") {
				reduceFlag = 0
			}
			points, bbox, err := trailscan.LoadGPX(file, reduceFlag)
			if err != nil {
				return fmt.Errorf("cannot load gpx file: %w", err)
			}

			fetchOptions := trailscan.DefaultFetchOptions()
			fetchOptions.Endpoint = cmd.String("endpoint")
			queryTemplate := cmd.String("query-template")

			qt, ok := trailscan.AllTemplates[queryTemplate]
			if ok {
				fetchOptions.QueryTemplate = qt
			} else {
				b, err := os.ReadFile(queryTemplate)
				if err != nil {
					return fmt.Errorf("cannot read query template file: %w", err)
				}
				fetchOptions.QueryTemplate = string(b)
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
				_, _ = fmt.Fprintf(w, "NUM\tNAME\tTYPE\tLAT\tLON\tELEVATION\tTRACKED ELEVATION\tDISTANCE\n")

				for _, v := range visited {
					var amType, name string
					var ele float64
					if v.Amenity.ParentWay != nil {
						name = v.Amenity.ParentWay.Name
						ele = v.Amenity.ParentWay.Ele
						amType = v.Amenity.ParentWay.Type
					} else {
						name = v.Amenity.Name
						ele = v.Amenity.Ele
						amType = v.Amenity.Type
					}

					_, _ = fmt.Fprintf(w,
						"%3d\t%s\t%s\t%0.5f\t%0.5f\t%.0fm\t%.0fm\t%.1fm\n",
						v.VisitedIndex,
						name,
						amType,
						v.Amenity.Lat,
						v.Amenity.Lon,
						ele,
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
		Commands: []*cli.Command{
			{
				UsageText: "trailscan querytemplates",
				Name:      "querytemplates",
				Usage:     "Print all predefined querytemplates",
				Action: func(ctx context.Context, cmd *cli.Command) error {

					for _, name := range supportedQueryTemplates {
						_, _ = fmt.Fprintf(cmd.Writer, "template: %s\n%s\n---\n", name, trailscan.AllTemplates[name])
					}

					return nil
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
