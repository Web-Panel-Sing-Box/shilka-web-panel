package client_test

import (
	"context"
	"testing"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	svcclient "sing-box-web-panel/internal/services/client"
)

type clientRepo struct {
	items             map[int64]*domain.Client
	deleteManyCalls   int
	statusManyCalls   int
	resetManyCalls    int
	lastMutationIDs   []int64
	lastMutationState domain.ClientStatus
}

func newClientRepo() *clientRepo {
	nodeID := int64(9)
	return &clientRepo{items: map[int64]*domain.Client{
		1: {ID: 1, Name: "one", Status: domain.ClientStatusActive, Enabled: true, UsedUp: 10, UsedDown: 20},
		2: {ID: 2, Name: "two", Status: domain.ClientStatusActive, Enabled: true, UsedUp: 30, UsedDown: 40},
		3: {ID: 3, NodeID: &nodeID, RemoteID: "30", Name: "remote", Status: domain.ClientStatusActive, Enabled: true},
	}}
}

func (r *clientRepo) Create(context.Context, *domain.Client) error { return nil }
func (r *clientRepo) GetByID(_ context.Context, id int64) (*domain.Client, error) {
	c, ok := r.items[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *c
	return &cp, nil
}
func (r *clientRepo) GetBySubToken(context.Context, string) (*domain.Client, error) {
	return nil, repo.ErrNotFound
}
func (r *clientRepo) List(context.Context) ([]domain.Client, error) { return nil, nil }
func (r *clientRepo) ListByInbound(context.Context, int64) ([]domain.Client, error) {
	return nil, nil
}
func (r *clientRepo) Update(context.Context, *domain.Client) error { return nil }
func (r *clientRepo) Delete(_ context.Context, id int64) error {
	delete(r.items, id)
	return nil
}
func (r *clientRepo) SetStatus(_ context.Context, id int64, status domain.ClientStatus, enabled bool) error {
	r.items[id].Status = status
	r.items[id].Enabled = enabled
	return nil
}
func (r *clientRepo) ResetTraffic(_ context.Context, id int64) error {
	r.items[id].UsedUp = 0
	r.items[id].UsedDown = 0
	return nil
}
func (r *clientRepo) DeleteMany(_ context.Context, ids []int64) error {
	r.deleteManyCalls++
	r.lastMutationIDs = append([]int64(nil), ids...)
	for _, id := range ids {
		delete(r.items, id)
	}
	return nil
}
func (r *clientRepo) SetStatusMany(_ context.Context, ids []int64, status domain.ClientStatus, enabled bool) error {
	r.statusManyCalls++
	r.lastMutationIDs = append([]int64(nil), ids...)
	r.lastMutationState = status
	for _, id := range ids {
		r.items[id].Status = status
		r.items[id].Enabled = enabled
	}
	return nil
}
func (r *clientRepo) ResetTrafficMany(_ context.Context, ids []int64) error {
	r.resetManyCalls++
	r.lastMutationIDs = append([]int64(nil), ids...)
	for _, id := range ids {
		r.items[id].UsedUp = 0
		r.items[id].UsedDown = 0
	}
	return nil
}

type inboundLookup struct{}

func (inboundLookup) GetByID(context.Context, int64) (*domain.Inbound, error) {
	return &domain.Inbound{}, nil
}

type triggerCounter struct{ calls int }

func (t *triggerCounter) Trigger() { t.calls++ }

func TestBulkDeleteBatchesLocalsAndNotifiesOnce(t *testing.T) {
	repository := newClientRepo()
	trigger := &triggerCounter{}
	service := svcclient.NewService(repository, inboundLookup{}, trigger)

	results, err := service.BulkDelete(context.Background(), []int64{1, 2, 3, 404})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}
	if repository.deleteManyCalls != 1 || trigger.calls != 1 {
		t.Fatalf("delete calls = %d, trigger calls = %d", repository.deleteManyCalls, trigger.calls)
	}
	assertIDs(t, repository.lastMutationIDs, []int64{1, 2})
	if !results[0].OK || !results[1].OK || results[2].OK || results[2].Err == nil || results[3].OK || results[3].Err == nil {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestBulkSetStatusBatchesAndAlignsEnabled(t *testing.T) {
	repository := newClientRepo()
	trigger := &triggerCounter{}
	service := svcclient.NewService(repository, inboundLookup{}, trigger)

	results, err := service.BulkSetStatus(context.Background(), []int64{1, 2}, domain.ClientStatusDisabled)
	if err != nil {
		t.Fatalf("BulkSetStatus: %v", err)
	}
	if repository.statusManyCalls != 1 || trigger.calls != 1 || !results[0].OK || !results[1].OK {
		t.Fatalf("calls/results: status=%d trigger=%d results=%+v", repository.statusManyCalls, trigger.calls, results)
	}
	for _, id := range []int64{1, 2} {
		if repository.items[id].Status != domain.ClientStatusDisabled || repository.items[id].Enabled {
			t.Fatalf("client %d = %+v", id, repository.items[id])
		}
	}
}

func TestBulkResetTrafficDoesNotNotify(t *testing.T) {
	repository := newClientRepo()
	trigger := &triggerCounter{}
	service := svcclient.NewService(repository, inboundLookup{}, trigger)

	results, err := service.BulkResetTraffic(context.Background(), []int64{1, 2})
	if err != nil {
		t.Fatalf("BulkResetTraffic: %v", err)
	}
	if repository.resetManyCalls != 1 || trigger.calls != 0 || !results[0].OK || !results[1].OK {
		t.Fatalf("calls/results: reset=%d trigger=%d results=%+v", repository.resetManyCalls, trigger.calls, results)
	}
	for _, id := range []int64{1, 2} {
		if repository.items[id].UsedUp != 0 || repository.items[id].UsedDown != 0 {
			t.Fatalf("client %d traffic was not reset", id)
		}
	}
}

func assertIDs(t *testing.T, got, want []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}
