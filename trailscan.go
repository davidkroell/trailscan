package trailscan

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/tkrajina/gpxgo/gpx"
)

type Point struct {
	Lat float64
	Lon float64
	Ele float64
}

type BoundingBox struct {
	MinLat float64
	MinLon float64
	MaxLat float64
	MaxLon float64
}

type Amenity struct {
	ID   int64
	Name string
	Type string
	Ele  float64
	Lat  float64
	Lon  float64
}

type VisitedAmenity struct {
	Amenity        Amenity
	Distance       float64
	TrackElevation float64
	VisitedIndex   int

	sortingNum int
}

type OverpassResponse struct {
	Elements []struct {
		ID   int64   `json:"id"`
		Lat  float64 `json:"lat"`
		Lon  float64 `json:"lon"`
		Tags struct {
			Name    string `json:"name"`
			Natural string `json:"natural"`
			Place   string `json:"place"`
			Tourism string `json:"tourism"`
			Ele     string `json:"ele"`
		} `json:"tags"`
	} `json:"elements"`
}

func LoadGPX(gpxReader io.Reader) ([]Point, BoundingBox, error) {
	gpxData, err := gpx.Parse(gpxReader)
	if err != nil {
		return nil, BoundingBox{}, err
	}

	points := make([]Point, 0, 2000) // a typical gpx file has at least a few hundred or a thousand points

	bbox := BoundingBox{
		MinLat: math.MaxFloat64,
		MinLon: math.MaxFloat64,
		MaxLat: -math.MaxFloat64,
		MaxLon: -math.MaxFloat64,
	}

	for _, track := range gpxData.Tracks {
		for _, seg := range track.Segments {
			for _, p := range seg.Points {

				points = append(points, Point{
					Lat: p.Latitude,
					Lon: p.Longitude,
					Ele: p.Elevation.Value(),
				})

				bbox.MinLat = math.Min(bbox.MinLat, p.Latitude)
				bbox.MinLon = math.Min(bbox.MinLon, p.Longitude)
				bbox.MaxLat = math.Max(bbox.MaxLat, p.Latitude)
				bbox.MaxLon = math.Max(bbox.MaxLon, p.Longitude)
			}
		}
	}

	// Bounding box expansion for Overpass query
	const bboxExpansionDegrees = 0.01

	bbox.MinLat -= bboxExpansionDegrees
	bbox.MinLon -= bboxExpansionDegrees
	bbox.MaxLat += bboxExpansionDegrees
	bbox.MaxLon += bboxExpansionDegrees

	return points, bbox, nil
}

type FetchOptions struct {
	QueryTemplate string
	Endpoint      string
}

const PeaksQueryTemplate = `
[out:json][timeout:15];
node["natural"="peak"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});
out body;`

const HikingQueryTemplate = `
[out:json][timeout:15];
(
  node["natural"~"peak|saddle|water"]["name"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});
  node["natural"="lake"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});

  node["tourism"="alpine_hut"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});
  node["tourism"="wilderness_hut"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});
  node["amenity"="shelter"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});

  node["tourism"="viewpoint"]["name"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});
);
out body;`

const VillagesQueryTemplate = `
[out:json][timeout:15];

node[place~"city|town|village"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});

out body;`

const CyclingQueryTemplate = `
[out:json][timeout:15];
(
  node[place~"city|town|village"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});
  node["natural"="saddle"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});

  node["tourism"="viewpoint"]["name"]({{.MinLat}},{{.MinLon}},{{.MaxLat}},{{.MaxLon}});
);
out body;`

func DefaultFetchOptions() FetchOptions {
	return FetchOptions{
		QueryTemplate: PeaksQueryTemplate,
		Endpoint:      "https://overpass-api.de/api/interpreter",
	}
}

func FetchAmenities(ctx context.Context, bbox BoundingBox, op FetchOptions) ([]Amenity, error) {
	queryBuf := new(bytes.Buffer)
	err := template.Must(template.New("query").Parse(op.QueryTemplate)).ExecuteTemplate(queryBuf, "query", bbox)
	if err != nil {
		return nil, fmt.Errorf("error templating query: %w", err)
	}

	query := strings.ReplaceAll(queryBuf.String(), "\n", "")
	query = strings.ReplaceAll(query, "\t", "")

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		op.Endpoint,
		strings.NewReader(query),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "go-http/"+runtime.Version()+" trailscan")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("overpass api error: %s\n%s", resp.Status, string(body))
	}

	var result OverpassResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	amenities := make([]Amenity, 0, len(result.Elements))

	for _, e := range result.Elements {
		var ele float64
		if e.Tags.Ele != "" {
			ele, _ = strconv.ParseFloat(e.Tags.Ele, 64)
		}

		amenities = append(amenities, Amenity{
			ID:   e.ID,
			Type: cmp.Or(e.Tags.Natural, e.Tags.Place, e.Tags.Tourism),
			Name: e.Tags.Name,
			Ele:  ele,
			Lat:  e.Lat,
			Lon:  e.Lon,
		})
	}

	return amenities, nil
}

type FindOptions struct {
	MaxDistanceMeters      float64
	MaxElevationDifference float64
}

func DefaultFindOptions() FindOptions {
	return FindOptions{
		MaxDistanceMeters:      50,
		MaxElevationDifference: 30,
	}
}

func FindVisitedAmenities(candidates []Point, amenities []Amenity, op FindOptions) []VisitedAmenity {
	type temp struct {
		firstNum      int
		bestDistance  float64
		bestElevation float64
		found         bool
	}

	state := make(map[int64]*temp) // Amenity ID -> tracking state

	for i, p := range candidates {
		for _, amenity := range amenities {

			d := gpx.HaversineDistance(p.Lat, p.Lon, amenity.Lat, amenity.Lon)

			s, ok := state[amenity.ID]
			if !ok {
				s = &temp{
					bestDistance: math.MaxFloat64,
				}
				state[amenity.ID] = s
			}

			// update BEST distance anywhere on track
			if d < s.bestDistance {
				s.bestDistance = d
				s.bestElevation = p.Ele
			}

			// first time we see it in track order
			if !s.found &&
				d <= op.MaxDistanceMeters &&
				(amenity.Ele <= 0 || math.Abs(p.Ele-amenity.Ele) <= op.MaxElevationDifference) {

				s.firstNum = i
				s.found = true
			}
		}
	}

	// build results
	results := make([]VisitedAmenity, 0, len(state))

	for _, amenity := range amenities {
		s := state[amenity.ID]

		// only include if ever matched
		if !s.found {
			continue
		}

		results = append(results, VisitedAmenity{
			sortingNum:     s.firstNum,
			Amenity:        amenity,
			Distance:       s.bestDistance,
			TrackElevation: s.bestElevation,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].sortingNum < results[j].sortingNum
	})

	for i := 0; i < len(results); i++ {
		results[i].VisitedIndex = i + 1
	}

	return results
}
