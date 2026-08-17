package downloader

import "testing"

func TestConnectionPlanHoldsTheCeiling(t *testing.T) {
	for _, tc := range []struct{ ask, files, chunks int }{
		{0, 1, maxChunks},
		{1, 1, maxChunks},
		{2, 2, maxChunks},
		{3, 3, 2},
		{4, 4, 2},
		{8, 8, 1},
		{20, 8, 1}, // clamped: extra files would only wait at zero
	} {
		files, chunks := connectionPlan(tc.ask)
		if files != tc.files || chunks != tc.chunks {
			t.Errorf("connectionPlan(%d) = %d files, %d chunks; want %d, %d",
				tc.ask, files, chunks, tc.files, tc.chunks)
		}
		if files*chunks > maxConnections {
			t.Errorf("%d files x %d chunks = %d, over the %d ceiling",
				files, chunks, files*chunks, maxConnections)
		}
	}
}

func TestPlanChunksRespectsBudget(t *testing.T) {
	plan, ok := planChunks(200<<20, 2)
	if !ok {
		t.Fatal("a 200 MB file should chunk")
	}
	if len(plan.ranges) != 2 {
		t.Fatalf("got %d ranges, want 2", len(plan.ranges))
	}
	// Ranges must tile the file exactly, with no gap or overlap.
	var next int64
	for i, r := range plan.ranges {
		if r[0] != next {
			t.Errorf("range %d starts at %d, want %d", i, r[0], next)
		}
		next = r[1] + 1
	}
	if next != plan.total {
		t.Errorf("ranges cover %d bytes, want %d", next, plan.total)
	}

	if _, ok := planChunks(1<<20, 4); ok {
		t.Error("a 1 MB file is not worth splitting")
	}
	if _, ok := planChunks(200<<20, 1); ok {
		t.Error("one chunk means a single stream")
	}
}
