package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
)

const (
	maxWebResponseBytes    = 4 << 20
	maxWebRedirects        = 5
	maxWebResults          = 20
	maxWebResultFieldRunes = 4096
)

type webFetchTool struct {
	client   *http.Client
	resolver *net.Resolver
	spec     tool.Spec
}

func NewWebFetch(client *http.Client) tool.Tool {
	return &webFetchTool{client: client, resolver: net.DefaultResolver, spec: tool.Spec{
		Name: "web_fetch", Description: "Fetch a public HTTP or HTTPS URL",
		ReadOnly: true, ConcurrencySafe: true,
		Schema: json.RawMessage(`{"type":"object","required":["url"],"properties":{"url":{"type":"string"}},"additionalProperties":false}`),
	}}
}

func (web *webFetchTool) Spec() tool.Spec { return web.spec }

func (web *webFetchTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	u, err := parseWebURL(arguments)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: web.spec.Name, Action: permissions.ActionNetwork, Network: []string{u.Hostname()}}, nil
}

func (web *webFetchTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	u, err := parseWebURL(arguments)
	if err != nil {
		return core.ToolResult{}, err
	}
	client := web.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	clientCopy := *client
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = secureDialContext(web.resolver)
	clientCopy.Transport = transport
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxWebRedirects {
			return errors.New("web fetch exceeded redirect limit")
		}
		if err := validatePublicURL(req.Context(), web.resolver, req.URL); err != nil {
			return err
		}
		return nil
	}
	if err := validatePublicURL(ctx, web.resolver, u); err != nil {
		return core.ToolResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return core.ToolResult{}, err
	}
	req.Header.Set("Accept", "text/html, text/plain, application/json, */*;q=0.1")
	response, err := clientCopy.Do(req)
	if err != nil {
		return core.ToolResult{}, err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, maxWebResponseBytes+1))
	if err != nil {
		return core.ToolResult{}, err
	}
	if len(content) > maxWebResponseBytes {
		return core.ToolResult{}, fmt.Errorf("web response exceeds %d bytes", maxWebResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return core.ToolResult{}, fmt.Errorf("web fetch returned HTTP %s: %s", response.Status, strings.TrimSpace(string(content)))
	}
	return textResult(string(content)), nil
}

func parseWebURL(arguments json.RawMessage) (*url.URL, error) {
	var input struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return nil, err
	}
	input.URL = strings.TrimSpace(input.URL)
	u, err := url.Parse(input.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, fmt.Errorf("url must be a public http or https URL")
	}
	return u, nil
}

func validatePublicURL(ctx context.Context, resolver *net.Resolver, u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("url must be a public http or https URL")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("private or reserved network target %q is not allowed", host)
		}
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("local network target %q is not allowed", host)
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return fmt.Errorf("resolve public network target %q: %w", host, err)
	}
	for _, address := range addresses {
		if isPrivateIP(address.IP) {
			return fmt.Errorf("network target %q resolves to a private or reserved address", host)
		}
	}
	return nil
}

func secureDialContext(resolver *net.Resolver) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if resolver == nil {
			resolver = net.DefaultResolver
		}
		var addresses []net.IPAddr
		if ip := net.ParseIP(host); ip != nil {
			addresses = []net.IPAddr{{IP: ip}}
		} else {
			addresses, err = resolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("network target %q did not resolve", host)
		}
		for _, candidate := range addresses {
			if isPrivateIP(candidate.IP) {
				return nil, fmt.Errorf("network target %q resolves to a private or reserved address", host)
			}
		}
		dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
		var lastErr error
		for _, candidate := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast()
}

type WebResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

type WebSearch func(context.Context, string, int) ([]WebResult, error)

type webSearchTool struct {
	search WebSearch
	spec   tool.Spec
}

func NewWebSearch(search WebSearch) tool.Tool {
	return &webSearchTool{search: search, spec: tool.Spec{
		Name: "web_search", Description: "Search the web using the configured search provider",
		ReadOnly: true, ConcurrencySafe: true,
		Schema: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"additionalProperties":false}`),
	}}
}

func (web *webSearchTool) Spec() tool.Spec { return web.spec }
func (web *webSearchTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return permissions.Request{}, err
	}
	if strings.TrimSpace(input.Query) == "" {
		return permissions.Request{}, errors.New("query is required")
	}
	return permissions.Request{Tool: web.spec.Name, Action: permissions.ActionNetwork}, nil
}
func (web *webSearchTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return core.ToolResult{}, err
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return core.ToolResult{}, errors.New("query is required")
	}
	if input.Limit <= 0 {
		input.Limit = 5
	}
	if input.Limit > maxWebResults {
		return core.ToolResult{}, fmt.Errorf("limit exceeds %d", maxWebResults)
	}
	if web.search == nil {
		return core.ToolResult{}, errors.New("web search provider is unavailable")
	}
	results, err := web.search(ctx, input.Query, input.Limit)
	if err != nil {
		return core.ToolResult{}, err
	}
	if len(results) > input.Limit {
		results = results[:input.Limit]
	}
	for index := range results {
		results[index].Title = truncateRunes(results[index].Title, maxWebResultFieldRunes)
		results[index].URL = truncateRunes(results[index].URL, maxWebResultFieldRunes)
		results[index].Snippet = truncateRunes(results[index].Snippet, maxWebResultFieldRunes)
	}
	encoded, err := json.Marshal(struct {
		Query   string      `json:"query"`
		Results []WebResult `json:"results"`
	}{input.Query, results})
	if err != nil {
		return core.ToolResult{}, err
	}
	return textResult(string(encoded)), nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// NewDuckDuckGoSearch provides a keyless default search backend. Its response
// is treated as untrusted content and bounded again by webSearchTool.
func NewDuckDuckGoSearch(client *http.Client) WebSearch {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	clientCopy := *client
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = secureDialContext(net.DefaultResolver)
	clientCopy.Transport = transport
	return newDuckDuckGoSearch(&clientCopy, "https://api.duckduckgo.com/", func(ctx context.Context, target *url.URL) error {
		return validatePublicURL(ctx, net.DefaultResolver, target)
	})
}

func newDuckDuckGoSearch(client *http.Client, endpointURL string, validate func(context.Context, *url.URL) error) WebSearch {
	return func(ctx context.Context, query string, limit int) ([]WebResult, error) {
		endpoint, err := url.Parse(endpointURL)
		if err != nil {
			return nil, err
		}
		parameters := endpoint.Query()
		parameters.Set("q", query)
		parameters.Set("format", "json")
		parameters.Set("no_html", "1")
		parameters.Set("no_redirect", "1")
		endpoint.RawQuery = parameters.Encode()
		if err := validate(ctx, endpoint); err != nil {
			return nil, err
		}
		clientCopy := *client
		clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxWebRedirects {
				return errors.New("web search exceeded redirect limit")
			}
			return validate(req.Context(), req.URL)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		response, err := clientCopy.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, maxWebResponseBytes+1))
		if err != nil {
			return nil, err
		}
		if len(body) > maxWebResponseBytes {
			return nil, fmt.Errorf("web search response exceeds %d bytes", maxWebResponseBytes)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("web search returned HTTP %s", response.Status)
		}
		var payload struct {
			Heading       string `json:"Heading"`
			AbstractText  string `json:"AbstractText"`
			AbstractURL   string `json:"AbstractURL"`
			RelatedTopics []struct {
				Text     string `json:"Text"`
				FirstURL string `json:"FirstURL"`
			} `json:"RelatedTopics"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		results := make([]WebResult, 0, min(limit, maxWebResults))
		if payload.AbstractText != "" && payload.AbstractURL != "" {
			results = append(results, WebResult{Title: payload.Heading, URL: payload.AbstractURL, Snippet: payload.AbstractText})
		}
		for _, topic := range payload.RelatedTopics {
			if len(results) >= limit {
				break
			}
			if topic.Text != "" && topic.FirstURL != "" {
				results = append(results, WebResult{Title: topic.Text, URL: topic.FirstURL, Snippet: topic.Text})
			}
		}
		return results, nil
	}
}
