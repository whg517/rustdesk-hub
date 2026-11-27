package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/whg517/rustdesk-hub/internal/hub"
	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

type bookHandler func(*echo.Context, hub.BookScope) error

func (h *api) bookEntry(source hub.Source, guidFromRequest func(*echo.Context) string, endpoint bookHandler) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var scope hub.BookScope
		if source == hub.Web {
			scope.Credential = browserCredential(c)
			if c.Request().Method != http.MethodGet {
				if err := h.checkWebWrite(c, scope.Credential); err != nil {
					return err
				}
			}
		} else {
			scope.Credential = bearer(c)
		}
		if scope.Credential.Token == "" {
			return hub.ErrSession
		}
		if guidFromRequest != nil {
			scope.GUID = guidFromRequest(c)
			if scope.GUID == "" {
				return hub.InputError("缺少通讯录 GUID")
			}
		}
		return endpoint(c, scope)
	}
}

type peerResponse struct {
	ID    string   `json:"id"`
	Alias string   `json:"alias"`
	Note  string   `json:"note"`
	Tags  []string `json:"tags"`
}

type peerPageResponse struct {
	Total int64          `json:"total"`
	Peers []peerResponse `json:"data"`
}

type webPeerResponse struct {
	peerResponse
	Connection *rustdesk.Connection `json:"connection,omitempty"`
}

type tagResponse struct {
	Name  string `json:"name"`
	Color uint32 `json:"color"`
}

type webTagResponse struct {
	ID string `json:"id"`
	tagResponse
}

func encodeWebTags(tags []hub.Tag) []webTagResponse {
	result := make([]webTagResponse, 0, len(tags))
	for _, tag := range tags {
		result = append(result, webTagResponse{
			ID:          strconv.FormatUint(uint64(tag.ID), 10),
			tagResponse: tagResponse{Name: tag.Name, Color: tag.Color},
		})
	}
	return result
}

func encodeTags(tags []hub.Tag) []tagResponse {
	result := make([]tagResponse, 0, len(tags))
	for _, tag := range tags {
		result = append(result, tagResponse{Name: tag.Name, Color: tag.Color})
	}
	return result
}

func (h *api) listPeers(c *echo.Context, scope hub.BookScope) error {
	page, pageErr := strconv.Atoi(c.QueryParamOr("current", "1"))
	size, sizeErr := strconv.Atoi(c.QueryParamOr("pageSize", "100"))
	if pageErr != nil || sizeErr != nil || page < 1 || page > 1_000_000 || size < 1 || size > 1000 {
		return hub.InputError("分页参数无效")
	}
	filter := hub.PeerQuery{Page: page, PageSize: size}
	if scope.Credential.Source == hub.Web {
		filter.Search = strings.TrimSpace(c.QueryParam("q"))
		filter.Tag = c.QueryParam("tag")
		if tagID := c.QueryParam("tagId"); tagID != "" {
			id, err := parseWebTagID(tagID)
			if err != nil {
				return err
			}
			filter.TagID = id
		}
		filter.IncludeTags = true
	}
	data, err := h.store.ListPeers(c.Request().Context(), scope, filter)
	if err != nil {
		return err
	}
	result := peerPageResponse{Total: data.Total, Peers: make([]peerResponse, 0, len(data.Peers))}
	for _, peer := range data.Peers {
		result.Peers = append(result.Peers, peerResponse{ID: peer.ID, Alias: peer.Alias, Note: peer.Note, Tags: peer.Tags})
	}
	if filter.IncludeTags {
		setup, err := h.clientSetup(c.Request().Context())
		if err != nil {
			return err
		}
		peers := make([]webPeerResponse, 0, len(result.Peers))
		for _, peer := range result.Peers {
			peers = append(peers, webPeerResponse{
				peerResponse: peer,
				Connection:   setup.ConnectionFor(peer.ID),
			})
		}
		return c.JSON(http.StatusOK, struct {
			Total int64             `json:"total"`
			Peers []webPeerResponse `json:"data"`
			Tags  []webTagResponse  `json:"tags"`
		}{Total: result.Total, Peers: peers, Tags: encodeWebTags(data.Tags)})
	}
	return c.JSON(http.StatusOK, result)
}

func (h *api) listTags(c *echo.Context, scope hub.BookScope) error {
	tags, err := h.store.ListTags(c.Request().Context(), scope)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, encodeTags(tags))
}

func mutationComplete(c *echo.Context, scope hub.BookScope, err error) error {
	if err != nil {
		return err
	}
	if scope.Credential.Source == hub.Native {
		return c.NoContent(http.StatusOK)
	}
	return c.NoContent(http.StatusNoContent)
}

func additionComplete(c *echo.Context, scope hub.BookScope, created bool, err error) error {
	if err != nil {
		return err
	}
	if scope.Credential.Source == hub.Native {
		return c.NoContent(http.StatusOK)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return c.JSON(status, map[string]bool{"created": created})
}

func (h *api) addPeer(c *echo.Context, scope hub.BookScope) error {
	var request peerResponse
	if err := readJSON(c, &request); err != nil {
		return err
	}
	created, err := h.store.AddPeer(c.Request().Context(), scope,
		hub.Peer{ID: request.ID, Alias: request.Alias, Note: request.Note, Tags: request.Tags})
	return additionComplete(c, scope, created, err)
}

func nonemptyValue(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return false
	}
	var text string
	return json.Unmarshal(value, &text) != nil || text != ""
}

func (h *api) updatePeer(c *echo.Context, scope hub.BookScope) error {
	var request struct {
		ID       string          `json:"id"`
		Alias    *string         `json:"alias"`
		Note     *string         `json:"note"`
		Tags     *[]string       `json:"tags"`
		Hash     json.RawMessage `json:"hash"`
		Password json.RawMessage `json:"password"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	changes := hub.PeerChanges{
		Alias: request.Alias, Note: request.Note, Tags: request.Tags,
		ContainsDeviceCredential: nonemptyValue(request.Hash) || nonemptyValue(request.Password),
	}
	return mutationComplete(c, scope, h.store.UpdatePeer(c.Request().Context(), scope, request.ID, changes))
}

func (h *api) deletePeers(c *echo.Context, scope hub.BookScope) error {
	var ids []string
	if err := readJSON(c, &ids); err != nil {
		return err
	}
	return mutationComplete(c, scope, h.store.DeletePeers(c.Request().Context(), scope, ids))
}

func (h *api) addTag(c *echo.Context, scope hub.BookScope) error {
	var request struct {
		Name  string  `json:"name"`
		Color *uint32 `json:"color"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	if request.Color == nil {
		return hub.InputError("请选择标签颜色")
	}
	created, err := h.store.AddTag(c.Request().Context(), scope, hub.Tag{Name: request.Name, Color: *request.Color})
	return additionComplete(c, scope, created, err)
}

func (h *api) renameNativeTag(c *echo.Context, scope hub.BookScope) error {
	var request struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	return mutationComplete(c, scope, h.store.UpdateTag(c.Request().Context(), scope, hub.TagRef{Name: request.Old}, &request.New, nil))
}

func (h *api) updateNativeTagColor(c *echo.Context, scope hub.BookScope) error {
	var request struct {
		Name  string  `json:"name"`
		Color *uint32 `json:"color"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	if request.Color == nil {
		return hub.InputError("请选择标签颜色")
	}
	return mutationComplete(c, scope, h.store.UpdateTag(c.Request().Context(), scope, hub.TagRef{Name: request.Name}, nil, request.Color))
}

func parseWebTagID(id string) (uint, error) {
	if id == "" {
		return 0, apiError{http.StatusConflict, "AddressBook.ReloadRequired", "标签管理已更新，请刷新页面后重试"}
	}
	parsed, err := strconv.Atoi(id)
	if err != nil || parsed <= 0 {
		return 0, hub.InputError("标签 ID 无效")
	}
	return uint(parsed), nil
}

func (h *api) updateWebTag(c *echo.Context, scope hub.BookScope) error {
	var request struct {
		ID      string  `json:"tagId"`
		NewName *string `json:"newName"`
		Color   *uint32 `json:"color"`
	}
	if err := readJSON(c, &request); err != nil {
		return err
	}
	id, err := parseWebTagID(request.ID)
	if err != nil {
		return err
	}
	return mutationComplete(c, scope, h.store.UpdateTag(c.Request().Context(), scope, hub.TagRef{ID: id}, request.NewName, request.Color))
}

func (h *api) deleteWebTag(c *echo.Context, scope hub.BookScope) error {
	var body json.RawMessage
	if err := readJSON(c, &body); err != nil {
		return err
	}
	var request struct {
		ID string `json:"tagId"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return apiError{http.StatusConflict, "AddressBook.ReloadRequired", "标签管理已更新，请刷新页面后重试"}
	}
	id, err := parseWebTagID(request.ID)
	if err != nil {
		return err
	}
	return mutationComplete(c, scope, h.store.DeleteTagByID(c.Request().Context(), scope, id))
}

func (h *api) deleteTags(c *echo.Context, scope hub.BookScope) error {
	var names []string
	if err := readJSON(c, &names); err != nil {
		return err
	}
	return mutationComplete(c, scope, h.store.DeleteTags(c.Request().Context(), scope, names))
}
