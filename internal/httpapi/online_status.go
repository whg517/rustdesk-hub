package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/whg517/rustdesk-hub/internal/hbbs"
	"github.com/whg517/rustdesk-hub/internal/hub"
	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

type onlineStatusResponse struct {
	Configured bool              `json:"configured"`
	CheckedAt  *time.Time        `json:"checkedAt"`
	Statuses   map[string]string `json:"statuses"`
}

func queryOnlineServer(ctx context.Context, address string, ids []string) ([]bool, error) {
	client, err := hbbs.New(address)
	if err != nil {
		return nil, err
	}
	return client.Query(ctx, ids)
}

func (h *api) onlineStatus(c *echo.Context, scope hub.BookScope) error {
	ids := c.QueryParams()["id"]
	if len(ids) == 0 || len(ids) > hbbs.MaxPeers {
		return hub.InputError(fmt.Sprintf("每次须查询 1 至 %d 个设备 ID", hbbs.MaxPeers))
	}
	ctx := c.Request().Context()
	saved, err := h.store.SavedPeerIDs(ctx, scope, ids)
	if err != nil {
		return err
	}
	settings, err := h.store.RustDeskSettings(ctx)
	if err != nil {
		return err
	}
	address := settings.OnlineAddress()

	result := onlineStatusResponse{
		Configured: address != "",
		Statuses:   make(map[string]string, len(saved)),
	}
	query := make([]string, 0, len(saved))
	for _, id := range saved {
		result.Statuses[id] = "unknown"
		if rustdesk.IsRendezvousID(id) {
			query = append(query, id)
		}
	}

	if result.Configured && len(query) > 0 {
		states, err := h.queryOnline(ctx, address, query)
		checkedAt := time.Now().UTC()
		result.CheckedAt = &checkedAt
		if err == nil && len(states) == len(query) {
			for i, id := range query {
				if states[i] {
					result.Statuses[id] = "online"
				} else {
					result.Statuses[id] = "offline"
				}
			}
		}
	}

	return c.JSON(http.StatusOK, result)
}
