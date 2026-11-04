package mapbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	profileDriving        = "driving"
	profileDrivingTraffic = "driving-traffic"
	maxMatrixCoordinates  = 25
	maxResponseBytes      = 2 << 20
)

type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

var _ services.IRoutingProvider = (*Client)(nil)

func NewClient(token, baseURL string, timeout time.Duration) *Client {
	return &Client{token: token, baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}}
}

type directionsResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Routes  []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry string  `json:"geometry"`
	} `json:"routes"`
}

type matrixResponse struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Durations [][]*float64 `json:"durations"`
}

func (c *Client) Route(ctx context.Context, from, to entities.GeoPoint, via ...entities.GeoPoint) (services.Route, error) {
	points := append(append([]entities.GeoPoint{from}, via...), to)
	query := url.Values{"overview": {"simplified"}, "geometries": {"polyline6"}, "alternatives": {"false"}}
	var body directionsResponse
	if err := c.get(ctx, "/directions/v5/mapbox/"+profileDrivingTraffic+"/"+coordinates(points), query, &body); err != nil {
		return services.Route{}, err
	}
	if body.Code != "Ok" || len(body.Routes) == 0 {
		return services.Route{}, fmt.Errorf("%w: directions answered %s %s", services.ErrRoutingUnavailable, body.Code, body.Message)
	}
	route := body.Routes[0]
	return services.Route{
		DistanceMeters: int(route.Distance),
		Duration:       time.Duration(route.Duration * float64(time.Second)),
		Geometry:       route.Geometry,
		Source:         services.RouteSourceMapbox,
	}, nil
}

func (c *Client) ETAs(ctx context.Context, origins []entities.GeoPoint, to entities.GeoPoint) ([]time.Duration, error) {
	if len(origins) == 0 {
		return nil, nil
	}
	if len(origins) >= maxMatrixCoordinates {
		origins = origins[:maxMatrixCoordinates-1]
	}
	points := append(append([]entities.GeoPoint{}, origins...), to)
	sources := make([]string, 0, len(origins))
	for i := range origins {
		sources = append(sources, strconv.Itoa(i))
	}
	query := url.Values{
		"sources":      {strings.Join(sources, ";")},
		"destinations": {strconv.Itoa(len(origins))},
		"annotations":  {"duration"},
	}
	var body matrixResponse
	if err := c.get(ctx, "/directions-matrix/v1/mapbox/"+profileDriving+"/"+coordinates(points), query, &body); err != nil {
		return nil, err
	}
	if body.Code != "Ok" || len(body.Durations) != len(origins) {
		return nil, fmt.Errorf("%w: matrix answered %s %s", services.ErrRoutingUnavailable, body.Code, body.Message)
	}
	etas := make([]time.Duration, 0, len(origins))
	for _, row := range body.Durations {
		if len(row) == 0 || row[0] == nil {
			return nil, fmt.Errorf("%w: matrix without a route for an origin", services.ErrRoutingUnavailable)
		}
		etas = append(etas, time.Duration(*row[0]*float64(time.Second)))
	}
	return etas, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, dest any) error {
	query.Set("access_token", c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+query.Encode(), http.NoBody)
	if err != nil {
		return fmt.Errorf("build mapbox request: %w", err)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", services.ErrRoutingUnavailable, redactToken(err))
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%w: mapbox answered %d", services.ErrRoutingUnavailable, res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(dest); err != nil {
		return fmt.Errorf("%w: decode mapbox response: %w", services.ErrRoutingUnavailable, err)
	}
	return nil
}

func redactToken(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func coordinates(points []entities.GeoPoint) string {
	parts := make([]string, 0, len(points))
	for _, p := range points {
		parts = append(parts, strconv.FormatFloat(p.Lng, 'f', 6, 64)+","+strconv.FormatFloat(p.Lat, 'f', 6, 64))
	}
	return strings.Join(parts, ";")
}
