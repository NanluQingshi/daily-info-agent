package filter

import (
	"reflect"
	"testing"

	"github.com/user/daily-info-agent/pkg/models"
)

func item(title, desc string) models.RawItem {
	return models.RawItem{Title: title, Description: desc}
}

func TestKeywordFilter_Disabled_PassesAll(t *testing.T) {
	f := New(nil, nil)
	if f.Enabled() {
		t.Fatal("empty filter should be disabled")
	}
	items := []models.RawItem{item("a", ""), item("b", "")}
	kept, removed := f.Apply(items)
	if len(kept) != 2 || removed != 0 {
		t.Fatalf("kept=%d removed=%d, want 2/0", len(kept), removed)
	}
	if &kept[0] != &items[0] {
		t.Error("disabled filter should return the input slice")
	}
}

func TestKeywordFilter_Whitelist_KeepsOnlyMatches(t *testing.T) {
	f := New([]string{"芯片", "GPU"}, nil)
	items := []models.RawItem{
		item("国产芯片量产突破", ""),
		item("股市今日大涨", ""),
		item("全新GPU架构发布", ""),
		item("完全无关的标题", ""),
	}
	kept, removed := f.Apply(items)
	if len(kept) != 2 || removed != 2 {
		t.Fatalf("kept=%d removed=%d, want 2/2", len(kept), removed)
	}
	if kept[0].Title != "国产芯片量产突破" || kept[1].Title != "全新GPU架构发布" {
		t.Errorf("wrong survivors: %q, %q", kept[0].Title, kept[1].Title)
	}
}

func TestKeywordFilter_Whitelist_MatchesDescription(t *testing.T) {
	f := New([]string{"chipset"}, nil)
	items := []models.RawItem{
		item(" innocuous title ", "a new chipset launched today"),
		item("no match here", "nothing relevant"),
	}
	kept, _ := f.Apply(items)
	if len(kept) != 1 {
		t.Fatalf("kept=%d, want 1", len(kept))
	}
}

func TestKeywordFilter_Blacklist_DropsMatches(t *testing.T) {
	f := New(nil, []string{"广告", "sponsored"})
	items := []models.RawItem{
		item("正经科技新闻", ""),
		item("【广告】限时优惠", ""),
		item("This is a Sponsored post", ""),
	}
	kept, removed := f.Apply(items)
	if len(kept) != 1 || removed != 2 {
		t.Fatalf("kept=%d removed=%d, want 1/2", len(kept), removed)
	}
	if kept[0].Title != "正经科技新闻" {
		t.Errorf("wrong survivor: %q", kept[0].Title)
	}
}

func TestKeywordFilter_BothLists_WhitelistThenBlacklist(t *testing.T) {
	// whitelist "AI" keeps 3 items; blacklist "广告" prunes the sponsored one.
	f := New([]string{"ai"}, []string{"广告"})
	items := []models.RawItem{
		item("AI 大模型新突破", ""),
		item("AI 课程【广告】", ""),
		item("股市行情", ""),
	}
	kept, removed := f.Apply(items)
	if len(kept) != 1 || removed != 2 {
		t.Fatalf("kept=%d removed=%d, want 1/2", len(kept), removed)
	}
	if kept[0].Title != "AI 大模型新突破" {
		t.Errorf("wrong survivor: %q", kept[0].Title)
	}
}

func TestKeywordFilter_CaseInsensitive(t *testing.T) {
	f := New([]string{"openai"}, nil)
	if !f.keep(item("OpenAI Releases GPT-5", "")) {
		t.Error("mixed-case title should match lowercase keyword")
	}
	if !f.keep(item("About OPENAI", "see also: OpenAi")) {
		t.Error("uppercase keyword occurrences should match")
	}
}

func TestKeywordFilter_EmptyInput(t *testing.T) {
	f := New([]string{"x"}, []string{"y"})
	kept, removed := f.Apply(nil)
	if len(kept) != 0 || removed != 0 {
		t.Fatalf("kept=%d removed=%d, want 0/0", len(kept), removed)
	}
}

func TestKeywordFilter_DoesNotMutateInput(t *testing.T) {
	f := New([]string{"keep"}, nil)
	items := []models.RawItem{item("keep me", ""), item("drop me", "")}
	_, _ = f.Apply(items)
	if len(items) != 2 {
		t.Fatalf("input mutated: len=%d", len(items))
	}
}

func TestSplitKeywords(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"芯片", []string{"芯片"}},
		{"芯片, 大模型", []string{"芯片", "大模型"}},
		{"芯片，大模型", []string{"芯片", "大模型"}}, // full-width comma
		{"芯片、大模型", []string{"芯片", "大模型"}}, // enumeration comma
		{" a , , b ,, ", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := SplitKeywords(c.raw)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitKeywords(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
