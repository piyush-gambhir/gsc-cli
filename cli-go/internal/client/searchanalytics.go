package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// QueryRequest is the complete searchanalytics.query request body.
type QueryRequest struct {
	StartDate             string        `json:"startDate"`
	EndDate               string        `json:"endDate"`
	Dimensions            []string      `json:"dimensions,omitempty"`
	Type                  string        `json:"type,omitempty"`
	DataState             string        `json:"dataState,omitempty"`
	AggregationType       string        `json:"aggregationType,omitempty"`
	DimensionFilterGroups []FilterGroup `json:"dimensionFilterGroups,omitempty"`
	RowLimit              int           `json:"rowLimit,omitempty"`
	StartRow              int           `json:"startRow,omitempty"`
}

type FilterGroup struct {
	GroupType string   `json:"groupType,omitempty"`
	Filters   []Filter `json:"filters"`
}

type Filter struct {
	Dimension  string `json:"dimension"`
	Operator   string `json:"operator,omitempty"`
	Expression string `json:"expression"`
}

type Row struct {
	Keys        []string `json:"keys,omitempty"`
	Clicks      float64  `json:"clicks"`
	Impressions float64  `json:"impressions"`
	CTR         float64  `json:"ctr"`
	Position    float64  `json:"position"`
}

type QueryResponse struct {
	Rows                    []Row     `json:"rows"`
	ResponseAggregationType string    `json:"responseAggregationType,omitempty"`
	Metadata                *Metadata `json:"metadata,omitempty"`
}

// Metadata marks where preliminary data starts. Google's method reference
// spells the fields in snake_case while discovery and the Go client use
// camelCase, so both are accepted.
type Metadata struct {
	FirstIncompleteDate string `json:"firstIncompleteDate,omitempty"`
	FirstIncompleteHour string `json:"firstIncompleteHour,omitempty"`
}

func (m *Metadata) UnmarshalJSON(b []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v := raw[k]; v != "" {
				return v
			}
		}
		return ""
	}
	m.FirstIncompleteDate = pick("firstIncompleteDate", "first_incomplete_date")
	m.FirstIncompleteHour = pick("firstIncompleteHour", "first_incomplete_hour")
	return nil
}

// Query runs one searchanalytics.query request (one page).
func (c *Client) Query(ctx context.Context, site string, req QueryRequest) (*QueryResponse, error) {
	var out QueryResponse
	if err := c.getJSON(ctx, http.MethodPost, sitePath(site)+"/searchAnalytics/query", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// QueryRaw runs one request and returns the response body unchanged.
func (c *Client) QueryRaw(ctx context.Context, site string, req QueryRequest) ([]byte, error) {
	return c.Do(ctx, http.MethodPost, sitePath(site)+"/searchAnalytics/query", nil, req)
}
