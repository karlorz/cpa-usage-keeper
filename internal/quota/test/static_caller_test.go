package test

import (
	"context"
	"encoding/json"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type staticResponseManagementCaller struct {
	requests []apicall.Request
	response *apicall.Response
}

func (c *staticResponseManagementCaller) CallManagementAPI(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
	c.requests = append(c.requests, request)
	if c.response == nil {
		return &apicall.Response{StatusCode: 200, BodyText: `{"data":[]}`, Body: json.RawMessage(`{"data":[]}`)}, nil
	}
	return c.response, nil
}
