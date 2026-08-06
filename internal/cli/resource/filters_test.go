package resource

import "testing"

func TestMetadataFilterQueryMergesAndSorts(t *testing.T) {
	f := &MetadataFilter{Tags: true}
	f.pairs = []string{"env=prod", "env=staging", "team=core"}
	f.tags = []string{"go"}
	got, err := f.Query()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"env":["prod","staging"],"tags":["go"],"team":["core"]}`
	if got != want {
		t.Errorf("Query() = %q, want %q", got, want)
	}
}

func TestMetadataFilterQueryEmptyWhenUnset(t *testing.T) {
	got, err := (&MetadataFilter{}).Query()
	if err != nil || got != "" {
		t.Errorf("Query() = %q, %v; want empty", got, err)
	}
}

func TestMetadataFilterQueryEmptyValueMeansKeyExists(t *testing.T) {
	f := &MetadataFilter{}
	f.pairs = []string{"deprecated="}
	got, err := f.Query()
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"deprecated":[""]}` {
		t.Errorf("Query() = %q", got)
	}
}

func TestMetadataFilterQueryRejectsMissingEquals(t *testing.T) {
	f := &MetadataFilter{}
	f.pairs = []string{"noequals"}
	if _, err := f.Query(); err == nil {
		t.Error("expected usage error for pair without =")
	}
}

func TestMetadataFilterApplyToPathMergesQuery(t *testing.T) {
	f := &MetadataFilter{}
	f.pairs = []string{"env=prod"}
	got, err := f.ApplyToPath("/api/v1/t/memories?project_id=p-1")
	if err != nil {
		t.Fatal(err)
	}
	want := "/api/v1/t/memories?metadata=%7B%22env%22%3A%5B%22prod%22%5D%7D&project_id=p-1"
	if got != want {
		t.Errorf("ApplyToPath() = %q, want %q", got, want)
	}
}
