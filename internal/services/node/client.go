package node

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sing-box-web-panel/internal/domain"
)

var ErrUnsafeAddress = errors.New("node address is private or loopback")
var ErrRemote = errors.New("remote node error")

type RemoteHTTPError struct {
	StatusCode int
	Body       string
}

func (e *RemoteHTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("%s: status %d", ErrRemote, e.StatusCode)
	}
	return fmt.Sprintf("%s: status %d: %s", ErrRemote, e.StatusCode, e.Body)
}

func (e *RemoteHTTPError) Is(target error) bool {
	return target == ErrRemote
}

func IsRemoteStatus(err error, status int) bool {
	var remoteErr *RemoteHTTPError
	return errors.As(err, &remoteErr) && remoteErr.StatusCode == status
}

type RemoteClienter interface {
	Status(ctx context.Context, n *domain.Node) (*RemoteStatus, time.Duration, error)
	Snapshot(ctx context.Context, n *domain.Node) (*RemoteSnapshot, error)
	CreateInbound(ctx context.Context, n *domain.Node, in RemoteInboundRequest) (*RemoteInbound, error)
	UpdateInbound(ctx context.Context, n *domain.Node, remoteID string, in RemoteInboundRequest) (*RemoteInbound, error)
	DeleteInbound(ctx context.Context, n *domain.Node, remoteID string) error
	ToggleInbound(ctx context.Context, n *domain.Node, remoteID string) (*RemoteInbound, error)
	CreateClient(ctx context.Context, n *domain.Node, in RemoteClientCreateRequest) (*RemoteClient, error)
	UpdateClient(ctx context.Context, n *domain.Node, remoteID string, in RemoteClientUpdateRequest) (*RemoteClient, error)
	DeleteClient(ctx context.Context, n *domain.Node, remoteID string) error
	ResetClientTraffic(ctx context.Context, n *domain.Node, remoteID string) (*RemoteClient, error)
	SetClientStatus(ctx context.Context, n *domain.Node, remoteID string, status domain.ClientStatus) (*RemoteClient, error)
	BulkDeleteClients(ctx context.Context, n *domain.Node, remoteIDs []string) (*RemoteClientBulkResponse, error)
	BulkResetClientTraffic(ctx context.Context, n *domain.Node, remoteIDs []string) (*RemoteClientBulkResponse, error)
	BulkSetClientStatus(ctx context.Context, n *domain.Node, remoteIDs []string, status domain.ClientStatus) (*RemoteClientBulkResponse, error)
}

type HTTPClient struct {
	statusTimeout time.Duration
	writeTimeout  time.Duration
}

func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		statusTimeout: 4 * time.Second,
		writeTimeout:  10 * time.Second,
	}
}

func (c *HTTPClient) Status(ctx context.Context, n *domain.Node) (*RemoteStatus, time.Duration, error) {
	start := time.Now()
	var out RemoteStatus
	if err := c.do(ctx, n, http.MethodGet, "/api/node/v1/status", nil, c.statusTimeout, &out); err != nil {
		return nil, 0, err
	}
	return &out, time.Since(start), nil
}

func (c *HTTPClient) Snapshot(ctx context.Context, n *domain.Node) (*RemoteSnapshot, error) {
	var out RemoteSnapshot
	if err := c.do(ctx, n, http.MethodGet, "/api/node/v1/snapshot", nil, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) CreateInbound(ctx context.Context, n *domain.Node, in RemoteInboundRequest) (*RemoteInbound, error) {
	var out RemoteInbound
	if err := c.do(ctx, n, http.MethodPost, "/api/node/v1/inbounds", in, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) UpdateInbound(ctx context.Context, n *domain.Node, remoteID string, in RemoteInboundRequest) (*RemoteInbound, error) {
	var out RemoteInbound
	if err := c.do(ctx, n, http.MethodPut, "/api/node/v1/inbounds/"+remoteID, in, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) DeleteInbound(ctx context.Context, n *domain.Node, remoteID string) error {
	return c.do(ctx, n, http.MethodDelete, "/api/node/v1/inbounds/"+remoteID, nil, c.writeTimeout, nil)
}

func (c *HTTPClient) ToggleInbound(ctx context.Context, n *domain.Node, remoteID string) (*RemoteInbound, error) {
	var out RemoteInbound
	if err := c.do(ctx, n, http.MethodPost, "/api/node/v1/inbounds/"+remoteID+"/toggle", nil, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) CreateClient(ctx context.Context, n *domain.Node, in RemoteClientCreateRequest) (*RemoteClient, error) {
	var out RemoteClient
	if err := c.do(ctx, n, http.MethodPost, "/api/node/v1/clients", in, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) UpdateClient(ctx context.Context, n *domain.Node, remoteID string, in RemoteClientUpdateRequest) (*RemoteClient, error) {
	var out RemoteClient
	if err := c.do(ctx, n, http.MethodPut, "/api/node/v1/clients/"+remoteID, in, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) DeleteClient(ctx context.Context, n *domain.Node, remoteID string) error {
	return c.do(ctx, n, http.MethodDelete, "/api/node/v1/clients/"+remoteID, nil, c.writeTimeout, nil)
}

func (c *HTTPClient) ResetClientTraffic(ctx context.Context, n *domain.Node, remoteID string) (*RemoteClient, error) {
	var out RemoteClient
	if err := c.do(ctx, n, http.MethodPost, "/api/node/v1/clients/"+remoteID+"/reset-traffic", nil, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) SetClientStatus(ctx context.Context, n *domain.Node, remoteID string, status domain.ClientStatus) (*RemoteClient, error) {
	var out RemoteClient
	in := RemoteClientStatusRequest{Status: string(status)}
	if err := c.do(ctx, n, http.MethodPost, "/api/node/v1/clients/"+remoteID+"/status", in, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) BulkDeleteClients(ctx context.Context, n *domain.Node, remoteIDs []string) (*RemoteClientBulkResponse, error) {
	return c.bulkClients(ctx, n, "/api/node/v1/clients/bulk/delete", RemoteClientBulkRequest{IDs: remoteIDs})
}

func (c *HTTPClient) BulkResetClientTraffic(ctx context.Context, n *domain.Node, remoteIDs []string) (*RemoteClientBulkResponse, error) {
	return c.bulkClients(ctx, n, "/api/node/v1/clients/bulk/reset-traffic", RemoteClientBulkRequest{IDs: remoteIDs})
}

func (c *HTTPClient) BulkSetClientStatus(ctx context.Context, n *domain.Node, remoteIDs []string, status domain.ClientStatus) (*RemoteClientBulkResponse, error) {
	return c.bulkClients(ctx, n, "/api/node/v1/clients/bulk/set-status", RemoteClientBulkRequest{IDs: remoteIDs, Status: string(status)})
}

func (c *HTTPClient) bulkClients(ctx context.Context, n *domain.Node, suffix string, in RemoteClientBulkRequest) (*RemoteClientBulkResponse, error) {
	var out RemoteClientBulkResponse
	if err := c.do(ctx, n, http.MethodPost, suffix, in, c.writeTimeout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) do(ctx context.Context, n *domain.Node, method, suffix string, body any, timeout time.Duration, out any) error {
	base, err := baseURL(n)
	if err != nil {
		return err
	}
	base.Path = path.Join(base.Path, suffix)
	var reader io.Reader
	if body != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return fmt.Errorf("encode node request: %w", err)
		}
		reader = buf
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.APITokenSecret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			DialContext:     safeDialer(n.AllowPrivateAddress).DialContext,
			TLSClientConfig: tlsClientConfig(n.SkipTLSVerify),
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return classifyTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &RemoteHTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode node response: %w", err)
	}
	return nil
}

func tlsClientConfig(skipVerify bool) *tls.Config {
	if !skipVerify {
		return nil
	}
	return &tls.Config{InsecureSkipVerify: true}
}

func baseURL(n *domain.Node) (*url.URL, error) {
	scheme := strings.ToLower(strings.TrimSpace(n.Scheme))
	if scheme == "" {
		scheme = "https"
	}
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported node scheme %q", n.Scheme)
	}
	if n.Address == "" {
		return nil, fmt.Errorf("node address is required")
	}
	if n.Port < 1 || n.Port > 65535 {
		return nil, fmt.Errorf("node port must be between 1 and 65535")
	}
	u := &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(strings.TrimSpace(n.Address), strconv.Itoa(n.Port)),
		Path:   strings.TrimSpace(n.BasePath),
	}
	return u, nil
}

func safeDialer(allowPrivate bool) *net.Dialer {
	return &net.Dialer{
		Timeout: 4 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if allowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return err
			}
			for _, ip := range ips {
				if isUnsafeIP(ip) {
					return ErrUnsafeAddress
				}
			}
			return nil
		},
	}
}

func isUnsafeIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	return false
}
