package trailscan

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"github.com/tkrajina/gpxgo/gpx"
)

const (
	// Matching settings
	PeakMatchDistanceMeters = 50.0
	MaxElevationDifference  = 30.0
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

type Peak struct {
	ID   int64
	Name string
	Ele  float64
	Lat  float64
	Lon  float64
}

type VisitedPeak struct {
	Peak           Peak
	Distance       float64
	TrackElevation float64
}

type OverpassResponse struct {
	Elements []struct {
		ID   int64   `json:"id"`
		Lat  float64 `json:"lat"`
		Lon  float64 `json:"lon"`
		Tags struct {
			Name string `json:"name"`
			Ele  string `json:"ele"`
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

func FetchPeaks(bbox BoundingBox) ([]Peak, error) {
	query := fmt.Sprintf(`
[out:json][timeout:10];
node["natural"="peak"](%f,%f,%f,%f);
out body;`,
		bbox.MinLat,
		bbox.MinLon,
		bbox.MaxLat,
		bbox.MaxLon,
	)
	query = strings.ReplaceAll(query, "\n", "")
	query = strings.ReplaceAll(query, "\t", "")

	endpoint := "https://overpass-api.de/api/interpreter"

	req, err := http.NewRequest(
		http.MethodPost,
		endpoint,
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
		return nil, fmt.Errorf("overpass error: %s\n%s", resp.Status, string(body))
	}

	var result OverpassResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var peaks []Peak

	for _, e := range result.Elements {
		var ele float64
		if e.Tags.Ele != "" {
			ele, _ = strconv.ParseFloat(e.Tags.Ele, 64)
		}

		peaks = append(peaks, Peak{
			ID:   e.ID,
			Name: e.Tags.Name,
			Ele:  ele,
			Lat:  e.Lat,
			Lon:  e.Lon,
		})
	}

	return peaks, nil
}

func FindVisitedPeaks(candidates []Point, peaks []Peak) []VisitedPeak {
	var results []VisitedPeak

	for _, peak := range peaks {
		bestDistance := math.MaxFloat64
		bestElevation := 0.0

		for _, p := range candidates {
			d := haversine(
				p.Lat,
				p.Lon,
				peak.Lat,
				peak.Lon,
			)

			if d < bestDistance {
				bestDistance = d
				bestElevation = p.Ele
			}
		}

		if bestDistance > PeakMatchDistanceMeters {
			continue
		}

		if peak.Ele > 0 &&
			math.Abs(bestElevation-peak.Ele) > MaxElevationDifference {
			continue
		}

		results = append(results, VisitedPeak{
			Peak:           peak,
			Distance:       bestDistance,
			TrackElevation: bestElevation,
		})
	}

	return results
}

func haversine(
	lat1, lon1,
	lat2, lon2 float64,
) float64 {
	const earthRadius = 6371000.0

	dLat := radians(lat2 - lat1)
	dLon := radians(lon2 - lon1)

	a :=
		math.Sin(dLat/2)*math.Sin(dLat/2) +
			math.Cos(radians(lat1))*
				math.Cos(radians(lat2))*
				math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(
		math.Sqrt(a),
		math.Sqrt(1-a),
	)

	return earthRadius * c
}

func radians(v float64) float64 {
	return v * math.Pi / 180
}
