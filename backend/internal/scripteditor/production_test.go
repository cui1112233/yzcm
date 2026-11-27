package scripteditor

import "testing"

func TestProductionStatusKeepsUnknownCallSeparateFromFailed(t *testing.T) {
	view := AggregateProduction([]LinkedProductionJob{{Stage: "video", Status: "may_have_sent"}})
	if view.ResultPendingConfirmation != 1 || view.Failed != 0 {
		t.Fatalf("view=%#v", view)
	}
}
