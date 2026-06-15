package trailscan_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/davidkroell/trailscan"
	"github.com/stretchr/testify/require"
)

func httpServeFile(t *testing.T, testDatafile string) *httptest.Server {
	t.Helper()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(testDatafile)
		require.NoError(t, err)
		defer f.Close()

		_, err = io.Copy(w, f)
		require.NoError(t, err)
	}))

	return s
}

func TestTrailscan(t *testing.T) {
	server := httpServeFile(t, "testdata/venedigerkrone.overpass.json")

	gpxFile, err := os.Open("testdata/venedigerkrone.gpx")
	require.NoError(t, err)
	defer gpxFile.Close()

	maxDistance := float64(30)
	points, bbox, err := trailscan.LoadGPX(gpxFile, maxDistance/4)
	require.NoError(t, err)

	// TODO assert points, bbox

	fetchOptions := trailscan.DefaultFetchOptions()
	fetchOptions.Endpoint = server.URL
	fetchOptions.QueryTemplate = trailscan.HikingQueryTemplate

	amenities, err := trailscan.FetchAmenities(context.Background(), bbox, fetchOptions)
	require.NoError(t, err)

	// TODO assert amenities

	findOpts := trailscan.DefaultFindOptions()
	findOpts.MaxDistanceMeters = maxDistance
	findOpts.MaxElevationDifference = 20

	visited := trailscan.FindVisitedAmenities(points, amenities, findOpts)

	require.NoError(t, err)
	require.NotNil(t, visited)

	// TODO assert visited
}
