package domain

import "time"

// ClientStatus mirrors the frontend client lifecycle states.
type ClientStatus string

const (
	ClientStatusActive   ClientStatus = "active"
	ClientStatusDisabled ClientStatus = "disabled"
	ClientStatusExpired  ClientStatus = "expired"
)

// Client is a proxy user. Name is the stable identity used to key per-user
// traffic statistics, so it is globally unique.
//
// A local client (NodeID == nil) may be bound to several inbounds at once
// (SIN-11): InboundIDs holds the full set, sourced from the client_inbounds
// join table. InboundID is kept as the primary/first binding for backward
// compatibility and as the single binding used by the remote-node path.
type Client struct {
	ID                 int64
	NodeID             *int64
	RemoteID           string
	InboundID          int64
	InboundIDs         []int64
	Name               string
	UUID               string // VLESS credential
	Password           string // Naive / Hysteria2 credential
	UsedUp             int64  // bytes uploaded by the client (counted cumulatively)
	UsedDown           int64  // bytes downloaded by the client
	TotalQuota         int64  // byte quota; 0 means unlimited
	Expiry             *time.Time
	Status             ClientStatus
	SubToken           string
	StartAfterFirstUse bool
	Enabled            bool
	FirstUsedAt        *time.Time
	LastUsedAt         *time.Time
	LastSyncedAt       *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// UsedTotal returns the combined up + down byte count.
func (c *Client) UsedTotal() int64 { return c.UsedUp + c.UsedDown }

// QuotaExceeded reports whether a finite quota has been reached.
func (c *Client) QuotaExceeded() bool {
	return c.TotalQuota > 0 && c.UsedTotal() >= c.TotalQuota
}

// IsExpired reports whether the client's subscription window has closed at now.
func (c *Client) IsExpired(now time.Time) bool {
	return c.Expiry != nil && now.After(*c.Expiry)
}
