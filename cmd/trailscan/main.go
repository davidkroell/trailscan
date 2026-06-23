package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"text/tabwriter"

	"github.com/davidkroell/trailscan"
	"github.com/tkrajina/gpxgo/gpx"
	"github.com/urfave/cli/v3"
)

type waypointJson struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Ele  float64 `json:"ele"`
	Name string  `json:"name"`
	ID   int64   `json:"id"`
	Type string  `json:"type"`
}

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
		Usage:     "a tool for analyzing GPX tracks using data from OpenStreetMap",
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
			&cli.StringFlag{
				Name:    "sort",
				Aliases: []string{"s"},
				Value:   "num",
				Usage:   "sort the output (only in case of text output)",
			},
			&cli.BoolFlag{
				Name:    "sort-inverted",
				Aliases: []string{"si"},
				Value:   false,
				Usage:   "invert the sorting, default: ascending",
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
				sortingField := cmd.String("sort")

				invertFactor := 1
				if cmd.Bool("sort-inverted") {
					invertFactor = -1
				}

				slices.SortFunc(visited, func(a, b trailscan.VisitedAmenity) int {
					var r int
					switch sortingField {
					case "name":
						r = cmp.Compare(a.Amenity.GetName(), b.Amenity.GetName())
					case "type":
						r = cmp.Compare(a.Amenity.GetType(), b.Amenity.GetType())
					case "lat":
						r = cmp.Compare(a.Amenity.Lat, b.Amenity.Lat)
					case "lon":
						r = cmp.Compare(a.Amenity.Lon, b.Amenity.Lon)
					case "elevation":
						r = cmp.Compare(a.Amenity.Ele, b.Amenity.Ele)
					case "num":
						fallthrough
					default:
						r = cmp.Compare(a.VisitedIndex, b.VisitedIndex)
					}

					return r * invertFactor
				})

				w := tabwriter.NewWriter(cmd.Writer, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintf(w, "NUM\tNAME\tTYPE\tLAT\tLON\tELEVATION\tTRACKED ELEVATION\tDISTANCE\n")

				for _, v := range visited {
					_, _ = fmt.Fprintf(w,
						"%3d\t%s\t%s\t%0.5f\t%0.5f\t%.0fm\t%.0fm\t%.1fm\n",
						v.VisitedIndex,
						v.Amenity.GetName(),
						v.Amenity.GetType(),
						v.Amenity.Lat,
						v.Amenity.Lon,
						v.Amenity.GetElevation(),
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
			{
				UsageText: "trailscan annotate",
				Name:      "annotate",
				Usage:     "Annotates a GPX file with waypoints using a JSON input, outputs a GPX file annotated with the provided waypoints",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:      "gpxfile",
						UsageText: "path to the gpx file",
					},
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "path to the output file to use, specify none will print to stdout",
					},
					&cli.StringFlag{
						Name:    "input",
						Aliases: []string{"i"},
						Usage:   "path to the input JSON file to use, specify none will read from stdin",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					reader := cmd.Reader
					writer := cmd.Writer

					outPath := cmd.String("output")
					if outPath != "" {
						f, err := os.OpenFile(outPath, os.O_RDWR|os.O_CREATE, 0644)
						if err != nil {
							return fmt.Errorf("opening output file: %w", err)
						}
						defer f.Close()
						writer = f
					}

					inPath := cmd.String("input")
					if inPath != "" {
						f, err := os.Open(inPath)
						if err != nil {
							return fmt.Errorf("opening input file: %w", err)
						}
						defer f.Close()
						reader = f
					}

					gpxData, err := gpx.ParseFile(cmd.StringArg("gpxfile"))
					if err != nil {
						return fmt.Errorf("parsing gpx file: %w", err)
					}

					waypoints := make([]waypointJson, 0)

					err = json.NewDecoder(reader).Decode(&waypoints)
					if err != nil {
						return fmt.Errorf("cannot read input waypoint file %w", err)
					}

					for _, w := range waypoints {
						ele := gpx.NullableFloat64{}
						if w.Ele > 0 {
							ele.SetValue(w.Ele)
						}

						gpxData.AppendWaypoint(&gpx.GPXPoint{
							Point: gpx.Point{
								Latitude:  w.Lat,
								Longitude: w.Lon,
								Elevation: ele,
							},
							Name:   w.Name,
							Type:   w.Type,
							Source: fmt.Sprintf("osmID: %d", w.ID),
						})
					}

					outBytes, err := gpxData.ToXml(gpx.ToXmlParams{
						Indent: true,
					})

					if err != nil {
						return fmt.Errorf("error marshalling GPX to XML: %w", err)
					}

					_, err = writer.Write(outBytes)
					if err != nil {
						return fmt.Errorf("error writing output: %w", err)
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
