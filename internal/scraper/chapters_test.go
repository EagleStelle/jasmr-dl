package scraper

import (
	"testing"
	"time"
)

// The real table from a post, header row included.
const chapterPage = `
<table id="basic-chapter-playlist"><tbody><tr>
<td class="chapter_list start_time">&nbsp;</td>
<td class="chapter_list chapter_title">トラックリスト</td>
</tr>
<tr><td class="chapter_list start_time"><a href="#" data-index="0" data-value="0">00:00:00 </a></td>
<td class="chapter_list chapter_title"><a href="#" data-track-title="トラック1：リアス部長の花嫁修行！？_wav" data-index="0" data-value="0">トラック1</a></td></tr>
<tr><td class="chapter_list start_time"><a href="#" data-index="1" data-value="323">00:05:23 </a></td>
<td class="chapter_list chapter_title"><a href="#" data-track-title="トラック2：リアス部長の甘い囁き！？_wav" data-index="1" data-value="323">トラック2</a></td></tr>
<tr><td class="chapter_list start_time"><a href="#" data-index="2" data-value="1260">00:21:00 </a></td>
<td class="chapter_list chapter_title"><a href="#" data-track-title="トラック3：リアス部長とおうちデート！？_wav" data-index="2" data-value="1260">トラック3</a></td></tr>
</tbody></table>`

func TestExtractChapters(t *testing.T) {
	doc, _ := parse(t, chapterPage)
	got := extractChapters(doc)

	if len(got) != 3 {
		t.Fatalf("got %d chapters, want 3 (the header row is not one)", len(got))
	}

	want := []Chapter{
		{0, "トラック1：リアス部長の花嫁修行！？_wav"},
		{323 * time.Second, "トラック2：リアス部長の甘い囁き！？_wav"},
		{1260 * time.Second, "トラック3：リアス部長とおうちデート！？_wav"},
	}
	for i, w := range want {
		if got[i].Start != w.Start {
			t.Errorf("chapter %d start = %v, want %v", i, got[i].Start, w.Start)
		}
		if got[i].Title != w.Title {
			t.Errorf("chapter %d title = %q, want %q", i, got[i].Title, w.Title)
		}
	}
}

func TestExtractChaptersDedupesBothTables(t *testing.T) {
	// Posts carry the same list twice, once per player skin.
	doc, _ := parse(t, chapterPage+`
		<table id="plyr-chapter-playlist"><tbody>
		<tr><td><a data-track-title="トラック1：リアス部長の花嫁修行！？_wav" data-value="0">a</a></td></tr>
		<tr><td><a data-track-title="トラック2：リアス部長の甘い囁き！？_wav" data-value="323">b</a></td></tr>
		<tr><td><a data-track-title="トラック3：リアス部長とおうちデート！？_wav" data-value="1260">c</a></td></tr>
		</tbody></table>`)

	if got := extractChapters(doc); len(got) != 3 {
		t.Fatalf("got %d chapters, want 3 after dedupe", len(got))
	}
}

func TestExtractChaptersNoneOrSingle(t *testing.T) {
	doc, _ := parse(t, `<p>no table here</p>`)
	if got := extractChapters(doc); got != nil {
		t.Errorf("got %v, want nil", got)
	}

	// One chapter describes nothing, so it is not worth embedding.
	doc, _ = parse(t, `<table id="basic-chapter-playlist"><tbody>
		<tr><td><a data-track-title="only" data-value="0">x</a></td></tr></tbody></table>`)
	if got := extractChapters(doc); got != nil {
		t.Errorf("got %v, want nil for a lone chapter", got)
	}
}
