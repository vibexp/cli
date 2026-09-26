package resource_test

import (
	"bytes"
	"errors"
	"net/http"
	"testing"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/exitcode"
)

func TestReadPageMeta(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   resource.PageMeta
		wantOK bool
	}{
		{"top level", `{"artifacts":[],"page":2,"total_pages":6,"total_count":59}`, resource.PageMeta{Page: 2, TotalPages: 6, TotalCount: 59}, true},
		{"under data envelope", `{"data":{"prompts":[],"page":1,"total_pages":3,"total_count":25},"status":"ok"}`, resource.PageMeta{Page: 1, TotalPages: 3, TotalCount: 25}, true},
		{"no total_count", `{"items":[],"page":1,"total_pages":2}`, resource.PageMeta{Page: 1, TotalPages: 2, TotalCount: -1}, true},
		{"no page defaults to 1", `{"items":[],"total_pages":2}`, resource.PageMeta{Page: 1, TotalPages: 2, TotalCount: -1}, true},
		{"derived from page_size", `{"teams":[],"page":1,"page_size":20,"total_count":45}`, resource.PageMeta{Page: 1, TotalPages: 3, TotalCount: 45}, true},
		{"derived from per_page", `{"items":[],"page":2,"per_page":10,"total_count":10}`, resource.PageMeta{Page: 2, TotalPages: 1, TotalCount: 10}, true},
		{"no metadata", `{"items":[]}`, resource.PageMeta{}, false},
		{"bare array", `[{"id":"a"}]`, resource.PageMeta{}, false},
		{"not json", `nope`, resource.PageMeta{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resource.ReadPageMeta([]byte(tt.raw))
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("ReadPageMeta = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestWarnIfPartial(t *testing.T) {
	const how = "--page or --all"
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"partial", `{"things":[{"id":"a"},{"id":"b"}],"page":1,"per_page":2,"total_pages":3,"total_count":5}`,
			"showing page 1 of 3 (2 of 5); use --page or --all\n"},
		{"partial without total_count", `{"things":[{"id":"a"}],"page":2,"total_pages":3}`,
			"showing page 2 of 3 (1 on this page); use --page or --all\n"},
		{"enveloped partial", `{"data":{"prompts":[{"id":"a"}],"page":1,"total_pages":2,"total_count":2}}`,
			"showing page 1 of 2 (1 of 2); use --page or --all\n"},
		{"everything fit", `{"things":[{"id":"a"}],"page":1,"total_pages":1,"total_count":1}`, ""},
		{"last page", `{"things":[{"id":"a"}],"page":3,"total_pages":3,"total_count":5}`, ""},
		{"empty", `{"things":[],"page":1,"total_pages":0,"total_count":0}`, ""},
		{"no metadata", `{"things":[{"id":"a"}]}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			resource.WarnIfPartial(&buf, []byte(tt.raw), how)
			if buf.String() != tt.want {
				t.Errorf("hint = %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

func TestExtractItems(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   int
		wantOK bool
	}{
		{"bare array", `[{"id":"a"},{"id":"b"}]`, 2, true},
		{"items key", `{"items":[{"id":"a"}],"page":1}`, 1, true},
		{"named key", `{"artifacts":[{"id":"a"},{"id":"b"},{"id":"c"}],"page":1}`, 3, true},
		{"data envelope", `{"data":{"prompts":[{"id":"a"},{"id":"b"}],"page":1},"message":"ok"}`, 2, true},
		{"not a list", `{"id":"a","title":"x"}`, 0, false},
		{"not json", `nope`, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resource.ExtractItems([]byte(tt.raw))
			if ok != tt.wantOK || len(got) != tt.want {
				t.Errorf("ExtractItems = %d items, %v; want %d, %v", len(got), ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestPaginationValidate(t *testing.T) {
	tests := []struct {
		name    string
		p       resource.Pagination
		wantErr bool
	}{
		{"all alone", resource.Pagination{All: true, Limit: 100}, false},
		{"page alone", resource.Pagination{Page: 2}, false},
		{"all with page", resource.Pagination{All: true, Page: 2}, true},
		{"all with offset", resource.Pagination{All: true, Offset: 10}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && exitcode.FromError(err) != exitcode.UsageErr {
				t.Errorf("exit = %d, want 2", exitcode.FromError(err))
			}
		})
	}
}

func TestUsageOnBadRequest(t *testing.T) {
	bad := &api.Error{Status: http.StatusBadRequest, Detail: "limit must be at most 100", RequestID: "req-1"}
	err := resource.UsageOnBadRequest(bad)
	if exitcode.FromError(err) != exitcode.UsageErr {
		t.Errorf("400 exit = %d, want 2", exitcode.FromError(err))
	}
	if err.Error() != bad.Error() {
		t.Errorf("message = %q, want the server's %q", err.Error(), bad.Error())
	}
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Error("the API error should stay reachable via errors.As")
	}

	notFound := &api.Error{Status: http.StatusNotFound, Detail: "gone"}
	if got := resource.UsageOnBadRequest(notFound); got != error(notFound) {
		t.Errorf("non-400 should pass through unchanged, got %v", got)
	}
	if resource.UsageOnBadRequest(nil) != nil {
		t.Error("nil should stay nil")
	}
}
