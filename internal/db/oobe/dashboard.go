package oobe

import (
	"fmt"
	"math/rand"
	"net/url"
)

// FolderInfo is a dashboard folder.
type FolderInfo struct {
	FolderID string
	Name     string
}

// DashboardInfo is a summary of one dashboard.
type DashboardInfo struct {
	DashboardID string
	Title       string
	FolderID    string
	FolderName  string
	Panels      []Panel // extracted from the non-null version object
}

// PanelQuery holds one query attached to a panel.
type PanelQuery struct {
	SQL    string
	XAlias string
	YAlias string
}

// Panel is a single visualization panel inside a dashboard.
type Panel struct {
	ID      string
	Type    string
	Title   string
	Queries []PanelQuery
}

// ListFolders returns all dashboard folders for the configured org.
func (b *Backend) ListFolders() ([]FolderInfo, error) {
	path := fmt.Sprintf("/api/v2/%s/folders/dashboards", b.cfg.Org)
	var resp struct {
		List []struct {
			FolderID string `json:"folderId"`
			Name     string `json:"name"`
		} `json:"list"`
	}
	if err := b.client.getJSON(path, &resp); err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	out := make([]FolderInfo, len(resp.List))
	for i, f := range resp.List {
		out[i] = FolderInfo{FolderID: f.FolderID, Name: f.Name}
	}
	return out, nil
}

// ListDashboardsInFolder returns all dashboards in a folder, with panels pre-populated.
func (b *Backend) ListDashboardsInFolder(folderID, folderName string) ([]DashboardInfo, error) {
	path := fmt.Sprintf("/api/%s/dashboards?page_num=0&page_size=1000&sort_by=name&desc=false&name=&folder=%s",
		b.cfg.Org, url.QueryEscape(folderID))
	// Each element has top-level fields AND version-specific objects (v1..v8), only one non-null.
	type versionData struct {
		DashboardID string `json:"dashboardId"`
		Title       string `json:"title"`
		Tabs        []struct {
			TabID  string `json:"tabId"`
			Name   string `json:"name"`
			Panels []struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Title   string `json:"title"`
				Queries []struct {
					Query  string `json:"query"`
					Fields struct {
						X []struct{ Alias string `json:"alias"` } `json:"x"`
						Y []struct{ Alias string `json:"alias"` } `json:"y"`
					} `json:"fields"`
				} `json:"queries"`
			} `json:"panels"`
		} `json:"tabs"`
	}
	var resp struct {
		Dashboards []struct {
			DashboardID string       `json:"dashboard_id"`
			Title       string       `json:"title"`
			FolderID    string       `json:"folder_id"`
			FolderName  string       `json:"folder_name"`
			V1          *versionData `json:"v1"`
			V2          *versionData `json:"v2"`
			V3          *versionData `json:"v3"`
			V4          *versionData `json:"v4"`
			V5          *versionData `json:"v5"`
			V6          *versionData `json:"v6"`
			V7          *versionData `json:"v7"`
			V8          *versionData `json:"v8"`
		} `json:"dashboards"`
	}
	if err := b.client.getJSON(path, &resp); err != nil {
		return nil, fmt.Errorf("list dashboards: %w", err)
	}
	out := make([]DashboardInfo, 0, len(resp.Dashboards))
	for _, d := range resp.Dashboards {
		// Find first non-nil version (prefer newest).
		var vd *versionData
		for _, v := range []*versionData{d.V8, d.V7, d.V6, d.V5, d.V4, d.V3, d.V2, d.V1} {
			if v != nil {
				vd = v
				break
			}
		}
		info := DashboardInfo{
			DashboardID: d.DashboardID,
			Title:       d.Title,
			FolderID:    d.FolderID,
			FolderName:  d.FolderName,
		}
		if info.FolderName == "" {
			info.FolderName = folderName
		}
		if vd != nil && len(vd.Tabs) > 0 {
			for _, p := range vd.Tabs[0].Panels {
				panel := Panel{ID: p.ID, Type: p.Type, Title: p.Title}
				for _, q := range p.Queries {
					pq := PanelQuery{SQL: q.Query}
					if len(q.Fields.X) > 0 {
						pq.XAlias = q.Fields.X[0].Alias
					}
					if len(q.Fields.Y) > 0 {
						pq.YAlias = q.Fields.Y[0].Alias
					}
					panel.Queries = append(panel.Queries, pq)
				}
				if len(panel.Queries) > 0 && panel.Queries[0].SQL != "" {
					info.Panels = append(info.Panels, panel)
				}
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// PanelQueryContext carries everything needed to build the panel search URL.
type PanelQueryContext struct {
	DashboardID   string
	DashboardName string
	FolderID      string
	FolderName    string
	PanelID       string
	PanelName     string
	FallbackCol   string // first x-axis alias or "_timestamp"
	SQL           string
}

// QueryPanel executes a panel SQL query over the current time window.
func (b *Backend) QueryPanel(ctx PanelQueryContext) ([]map[string]any, error) {
	if b.startUS == 0 {
		b.ResetTimeWindow()
	}
	fallback := ctx.FallbackCol
	if fallback == "" {
		fallback = "_timestamp"
	}
	runID := fmt.Sprintf("%016x%016x", rand.Int63(), rand.Int63())
	path := fmt.Sprintf(
		"/api/%s/_search_stream?type=logs&search_type=dashboards&use_cache=true"+
			"&dashboard_id=%s&dashboard_name=%s"+
			"&folder_id=%s&folder_name=%s"+
			"&panel_id=%s&panel_name=%s"+
			"&run_id=%s&tab_id=default&tab_name=Default"+
			"&fallback_order_by_col=%s&is_multi_stream_search=false",
		b.cfg.Org,
		url.QueryEscape(ctx.DashboardID), url.QueryEscape(ctx.DashboardName),
		url.QueryEscape(ctx.FolderID), url.QueryEscape(ctx.FolderName),
		url.QueryEscape(ctx.PanelID), url.QueryEscape(ctx.PanelName),
		runID,
		url.QueryEscape(fallback),
	)
	body := map[string]any{
		"query": map[string]any{
			"sql":        ctx.SQL,
			"start_time": b.startUS,
			"end_time":   b.endUS,
			"size":       -1,
		},
	}
	rc, err := b.client.postSSE(path, body)
	if err != nil {
		return nil, fmt.Errorf("query panel: %w", err)
	}
	return readSSEHits(rc)
}
