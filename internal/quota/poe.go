package quota

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type poeProvider struct {
	caller ManagementAPICaller
	config APICallConfig
}

func NewPoeProvider(caller ManagementAPICaller, config APICallConfig) ProviderHandler {
	return poeProvider{caller: caller, config: config}
}

func (p poeProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// Poe 只依赖 auth_index 的 API key，单个 usage endpoint 即可读取 compute-points 余额。
	response, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.config.Method,
		URL:       p.config.URL,
		Header:    copyHeaders(p.config.Headers),
	})
	if err != nil {
		return ProviderOutput{}, err
	}
	usage, err := parsePoeUsagePayload(response)
	if err != nil {
		return ProviderOutput{}, err
	}
	return ProviderOutput{Provider: "poe", Result: PoeResult{Usage: usage}}, nil
}
