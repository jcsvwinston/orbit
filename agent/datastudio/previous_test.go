package datastudio

import (
	"context"
	"testing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// A mutation answers with the record as it was, so the server's audit entry
// can say what changed without a second round trip: one record for an
// update or a delete, one per deleted record for a bulk action, none for a
// create — and none when the record was never there.
func TestDispatch_MutationsReturnThePreviousRecord(t *testing.T) {
	h := New(Config{Source: newMemSource()})
	ctx := context.Background()

	upd := h.Dispatch(ctx, &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_UpdateRecord{
		UpdateRecord: &adminv1.UpdateRecordRequest{ModelName: "Article", Id: "1", Record: &adminv1.Record{ValuesJson: map[string]string{"Title": `"renamed"`}}}}})
	if upd.Error != "" {
		t.Fatalf("update: %s", upd.Error)
	}
	if got := upd.GetPrevious(); len(got) != 1 || got[0].GetValuesJson()["Title"] != `"tenant a, first"` {
		t.Fatalf("an update must return the record as it was, got %v", got)
	}
	if upd.GetRecord().GetValuesJson()["Title"] != `"renamed"` {
		t.Fatalf("and the record as it became, got %v", upd.GetRecord().GetValuesJson())
	}
	if _, leaked := upd.GetPrevious()[0].GetValuesJson()["Secret"]; leaked {
		t.Fatal("the previous record obeys the same exclusions as the wire")
	}

	del := h.Dispatch(ctx, &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_DeleteRecord{
		DeleteRecord: &adminv1.DeleteRecordRequest{ModelName: "Article", Id: "2"}}})
	if del.Error != "" || !del.GetDeleteRecord().GetDeleted() {
		t.Fatalf("delete: %+v", del)
	}
	if got := del.GetPrevious(); len(got) != 1 || got[0].GetValuesJson()["ID"] != "2" {
		t.Fatalf("a delete must return the record it removed, got %v", got)
	}

	missing := h.Dispatch(ctx, &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_DeleteRecord{
		DeleteRecord: &adminv1.DeleteRecordRequest{ModelName: "Article", Id: "2"}}})
	if missing.Error == "" || len(missing.GetPrevious()) != 0 {
		t.Fatalf("deleting a record that is not there is not found, with nothing previous: %+v", missing)
	}

	bulk := h.Dispatch(ctx, &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_BulkAction{
		BulkAction: &adminv1.BulkActionRequest{ModelName: "Article", Action: "delete", Ids: []string{"3", "99"}}}})
	if b := bulk.GetBulkAction(); b.GetAffected() != 1 || b.GetFailed() != 1 {
		t.Fatalf("bulk: %+v", b)
	}
	if got := bulk.GetPrevious(); len(got) != 1 || got[0].GetValuesJson()["ID"] != "3" {
		t.Fatalf("a bulk action returns one previous record per record it removed, got %v", got)
	}

	create := h.Dispatch(ctx, &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_CreateRecord{
		CreateRecord: &adminv1.CreateRecordRequest{ModelName: "Article", Record: &adminv1.Record{ValuesJson: map[string]string{"Title": `"new"`}}}}})
	if create.Error != "" || len(create.GetPrevious()) != 0 {
		t.Fatalf("a create has nothing previous: %+v", create)
	}
}
