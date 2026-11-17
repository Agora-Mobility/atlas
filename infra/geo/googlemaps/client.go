package googlemaps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/tracing"
)

const (
	computeRoutesPath      = "/directions/v2:computeRoutes"
	computeRouteMatrixPath = "/distanceMatrix/v2:computeRouteMatrix"

	// Google bills by the fields asked for, and without status in the mask every matrix element looks OK.
	routesFieldMask = "routes.distanceMeters,routes.duration,routes.polyline.encodedPolyline"
	matrixFieldMask = "originIndex,status,condition,duration"

	travelModeDrive      = "DRIVE"
	preferTraffic        = "TRAFFIC_AWARE"
	preferNoTraffic      = "TRAFFIC_UNAWARE"
	polylineOverview     = "OVERVIEW"
	polylineEncoded      = "ENCODED_POLYLINE"
	conditionRouteExists = "ROUTE_EXISTS"

	maxMatrixOrigins = 24
	maxResponseBytes = 2 << 20
)

type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

var _ services.IRoutingProvider = (*Client)(nil)

func NewClient(apiKey, baseURL string, timeout time.Duration) *Client {
	return &Client{apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), http: tracing.HTTPClient(timeout)}
}

type latLng struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type location struct {
	LatLng latLng `json:"latLng"`
}

type waypoint struct {
	Location location `json:"location"`
}

type routesRequest struct {
	Origin            waypoint   `json:"origin"`
	Destination       waypoint   `json:"destination"`
	Intermediates     []waypoint `json:"intermediates,omitempty"`
	TravelMode        string     `json:"travelMode"`
	RoutingPreference string     `json:"routingPreference"`
	PolylineQuality   string     `json:"polylineQuality"`
	PolylineEncoding  string     `json:"polylineEncoding"`
}

type routesResponse struct {
	Routes []struct {
		DistanceMeters int    `json:"distanceMeters"`
		Duration       string `json:"duration"`
		Polyline       struct {
			EncodedPolyline string `json:"encodedPolyline"`
		} `json:"polyline"`
	} `json:"routes"`
}

type matrixWaypoint struct {
	Waypoint waypoint `json:"waypoint"`
}

type matrixRequest struct {
	Origins           []matrixWaypoint `json:"origins"`
	Destinations      []matrixWaypoint `json:"destinations"`
	TravelMode        string           `json:"travelMode"`
	RoutingPreference string           `json:"routingPreference"`
}

type matrixElement struct {
	OriginIndex int `json:"originIndex"`
	Status      struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
	Condition string `json:"condition"`
	Duration  string `json:"duration"`
}

func (c *Client) Route(ctx context.Context, from, to entities.GeoPoint, via ...entities.GeoPoint) (services.Route, error) {
	request := routesRequest{
		Origin:            toWaypoint(from),
		Destination:       toWaypoint(to),
		TravelMode:        travelModeDrive,
		RoutingPreference: preferTraffic,
		PolylineQuality:   polylineOverview,
		PolylineEncoding:  polylineEncoded,
	}
	for _, point := range via {
		request.Intermediates = append(request.Intermediates, toWaypoint(point))
	}
	var body routesResponse
	if err := c.post(ctx, computeRoutesPath, routesFieldMask, request, &body); err != nil {
		return services.Route{}, err
	}
	if len(body.Routes) == 0 {
		return services.Route{}, fmt.Errorf("%w: routes answered without a route", services.ErrRoutingUnavailable)
	}
	route := body.Routes[0]
	duration, err := parseDuration(route.Duration)
	if err != nil {
		return services.Route{}, err
	}
	return services.Route{
		DistanceMeters: route.DistanceMeters,
		Duration:       duration,
		Geometry:       route.Polyline.EncodedPolyline,
		Source:         services.RouteSourceGoogle,
	}, nil
}

func (c *Client) ETAs(ctx context.Context, origins []entities.GeoPoint, to entities.GeoPoint) ([]time.Duration, error) {
	if len(origins) == 0 {
		return nil, nil
	}
	if len(origins) > maxMatrixOrigins {
		origins = origins[:maxMatrixOrigins]
	}
	request := matrixRequest{
		Origins:           make([]matrixWaypoint, 0, len(origins)),
		Destinations:      []matrixWaypoint{{Waypoint: toWaypoint(to)}},
		TravelMode:        travelModeDrive,
		RoutingPreference: preferNoTraffic,
	}
	for _, origin := range origins {
		request.Origins = append(request.Origins, matrixWaypoint{Waypoint: toWaypoint(origin)})
	}
	var elements []matrixElement
	if err := c.post(ctx, computeRouteMatrixPath, matrixFieldMask, request, &elements); err != nil {
		return nil, err
	}
	return etasByOrigin(elements, len(origins))
}

// etasByOrigin puts the answer back in the order of the origins: Google does not promise any order.
func etasByOrigin(elements []matrixElement, origins int) ([]time.Duration, error) {
	etas := make([]time.Duration, origins)
	answered := make([]bool, origins)
	for _, element := range elements {
		if element.OriginIndex < 0 || element.OriginIndex >= origins {
			return nil, fmt.Errorf("%w: matrix answered unknown origin %d", services.ErrRoutingUnavailable, element.OriginIndex)
		}
		if element.Status.Code != 0 {
			return nil, fmt.Errorf("%w: matrix answered status %d %s", services.ErrRoutingUnavailable, element.Status.Code, element.Status.Message)
		}
		if element.Condition != conditionRouteExists {
			return nil, fmt.Errorf("%w: matrix without a route for an origin", services.ErrRoutingUnavailable)
		}
		eta, err := parseDuration(element.Duration)
		if err != nil {
			return nil, err
		}
		etas[element.OriginIndex] = eta
		answered[element.OriginIndex] = true
	}
	for _, ok := range answered {
		if !ok {
			return nil, fmt.Errorf("%w: matrix without an answer for an origin", services.ErrRoutingUnavailable)
		}
	}
	return etas, nil
}

func (c *Client) post(ctx context.Context, path, fieldMask string, payload, dest any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode google maps request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build google maps request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", c.apiKey)
	req.Header.Set("X-Goog-FieldMask", fieldMask)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", services.ErrRoutingUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%w: google maps answered %d", services.ErrRoutingUnavailable, res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(dest); err != nil {
		return fmt.Errorf("%w: decode google maps response: %w", services.ErrRoutingUnavailable, err)
	}
	return nil
}

// parseDuration reads the protobuf form Google uses, such as "165s".
func parseDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid duration %q", services.ErrRoutingUnavailable, value)
	}
	return duration, nil
}

func toWaypoint(point entities.GeoPoint) waypoint {
	return waypoint{Location: location{LatLng: latLng{Latitude: point.Lat, Longitude: point.Lng}}}
}
